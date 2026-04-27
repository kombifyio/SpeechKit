//go:build linux

package middleware

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimitOptions configures the in-memory token-bucket limiter.
type RateLimitOptions struct {
	RequestsPerSecond float64 // sustained rate; zero disables limiting
	Burst             int     // max tokens in bucket; zero disables limiting
	// AllowPublicPaths is the list of exact request paths that bypass the
	// limiter entirely. Production deployments must always include
	// `/healthz` and `/readyz` so external probes (Render, Kubernetes) are
	// never rate-limited away from a real outage.
	AllowPublicPaths []string
}

// RateLimit returns a middleware that enforces a per-identity token bucket.
// Identities come from the Auth middleware; requests without an identity key
// on the remote address, which is rough but adequate for v1. For production
// behind a trusted LB, swap to a distributed limiter (Redis, envoy).
//
// Keeping it in-memory is a deliberate v1 choice — it adds zero external
// dependencies to the OSS Server-Target.
func RateLimit(opts RateLimitOptions) Middleware {
	if opts.RequestsPerSecond <= 0 || opts.Burst <= 0 {
		// Disabled: returns a pass-through decorator.
		return func(next http.Handler) http.Handler { return next }
	}
	publicSet := make(map[string]struct{}, len(opts.AllowPublicPaths))
	for _, p := range opts.AllowPublicPaths {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			publicSet[trimmed] = struct{}{}
		}
	}
	buckets := &bucketStore{
		perSecond: opts.RequestsPerSecond,
		burst:     float64(opts.Burst),
		m:         map[string]*bucket{},
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, public := publicSet[r.URL.Path]; public {
				next.ServeHTTP(w, r)
				return
			}
			key := rateLimitKey(r)
			if !buckets.allow(key) {
				writeRateLimitError(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func rateLimitKey(r *http.Request) string {
	id := IdentityFromContext(r.Context())
	if id.UserID != "" {
		return "u:" + id.UserID
	}
	return "r:" + r.RemoteAddr
}

type bucket struct {
	tokens float64
	last   time.Time
}

type bucketStore struct {
	mu        sync.Mutex
	perSecond float64
	burst     float64
	m         map[string]*bucket
}

func (s *bucketStore) allow(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	b, ok := s.m[key]
	if !ok {
		s.m[key] = &bucket{tokens: s.burst - 1, last: now}
		return true
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * s.perSecond
	if b.tokens > s.burst {
		b.tokens = s.burst
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

func writeRateLimitError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", "1")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    "rate_limited",
			"message": "request rate limit exceeded",
		},
	})
}
