//go:build linux

package middleware

import (
	"container/list"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// defaultRateLimitMaxBuckets caps the in-memory bucket map so that a flood of
// distinct identities (e.g. a wide CIDR scan or an attacker rotating bearer
// tokens) cannot grow memory without bound. 100k buckets is roughly 10 MiB of
// resident memory, comfortably below any realistic container limit.
const defaultRateLimitMaxBuckets = 100_000

// RateLimitOptions configures the in-memory token-bucket limiter.
type RateLimitOptions struct {
	RequestsPerSecond float64 // sustained rate; zero disables limiting
	Burst             int     // max tokens in bucket; zero disables limiting
	// AllowPublicPaths is the list of exact request paths that bypass the
	// limiter entirely. Production deployments must always include
	// `/healthz` and `/readyz` so external probes (Render, Kubernetes) are
	// never rate-limited away from a real outage.
	AllowPublicPaths []string
	// MaxBuckets caps the in-memory bucket map. Zero falls back to
	// defaultRateLimitMaxBuckets. Once the cap is hit, the least-recently-
	// used bucket is evicted before a new one is inserted.
	MaxBuckets int
	// SweepInterval controls how often a background goroutine scans the
	// bucket map and evicts entries whose last access is older than
	// SweepMaxAge. Zero falls back to 5 minutes; set negative to disable.
	SweepInterval time.Duration
	// SweepMaxAge is the staleness threshold beyond which buckets are
	// evicted by the background sweeper. Zero falls back to a value
	// derived from Burst/RequestsPerSecond.
	SweepMaxAge time.Duration
	// Context, when non-nil, controls the lifetime of the background
	// sweeper goroutine. Cancellation stops the sweep loop. Production
	// callers should pass the server's shutdown context here.
	Context context.Context //nolint:containedctx // intentional middleware-lifetime ctx
}

// RateLimit returns a middleware that enforces a per-identity token bucket.
// Identities come from the Auth middleware; requests without an identity key
// on the remote address, which is rough but adequate for v1. For production
// behind a trusted LB, swap to a distributed limiter (Redis, envoy).
//
// Keeping it in-memory is a deliberate v1 choice — it adds zero external
// dependencies to the OSS Server-Target. The map is bounded by MaxBuckets
// with LRU eviction so a flood of distinct identities cannot exhaust memory.
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
	maxBuckets := opts.MaxBuckets
	if maxBuckets <= 0 {
		maxBuckets = defaultRateLimitMaxBuckets
	}
	buckets := &bucketStore{
		perSecond:  opts.RequestsPerSecond,
		burst:      float64(opts.Burst),
		maxBuckets: maxBuckets,
		m:          map[string]*list.Element{},
		lru:        list.New(),
	}
	startSweeper(opts, buckets)
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

func startSweeper(opts RateLimitOptions, buckets *bucketStore) {
	interval := opts.SweepInterval
	if interval == 0 {
		interval = 5 * time.Minute
	}
	if interval < 0 {
		return
	}
	maxAge := opts.SweepMaxAge
	if maxAge <= 0 {
		// A bucket is interesting only as long as a fresh request might be
		// affected by its accumulated tokens. After ~2x the time it takes a
		// fully drained bucket to refill to burst capacity, the bucket is
		// indistinguishable from a fresh one — safe to drop.
		seconds := 2 * float64(buckets.burst) / buckets.perSecond
		maxAge = max(time.Duration(seconds*float64(time.Second)), 5*time.Minute)
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				buckets.sweepStale(time.Now(), maxAge)
			}
		}
	}()
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

type bucketEntry struct {
	key    string
	bucket *bucket
}

type bucketStore struct {
	mu         sync.Mutex
	perSecond  float64
	burst      float64
	maxBuckets int
	m          map[string]*list.Element // key → element holding *bucketEntry
	lru        *list.List               // front = most recent, back = oldest
}

// entryOf is the single cast site for elements in the LRU list. The list
// is private and only [bucketStore] inserts into it, so a non-*bucketEntry
// value is an impossible-state corruption. Returning nil + logging — rather
// than panicking inside an HTTP request — lets the server keep serving
// other requests while operators get a structured signal in logs.
func entryOf(elem *list.Element) *bucketEntry {
	if elem == nil {
		return nil
	}
	entry, ok := elem.Value.(*bucketEntry)
	if !ok {
		slog.Error("ratelimit: bucketStore corruption: list element value has unexpected type")
		return nil
	}
	return entry
}

func (s *bucketStore) allow(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if elem, ok := s.m[key]; ok {
		s.lru.MoveToFront(elem)
		entry := entryOf(elem)
		if entry == nil {
			// Drop the corrupted element and treat as a fresh bucket below.
			delete(s.m, key)
			s.lru.Remove(elem)
		} else {
			b := entry.bucket
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
	}

	// Fresh bucket — evict oldest if over cap.
	for s.lru.Len() >= s.maxBuckets {
		oldest := s.lru.Back()
		if oldest == nil {
			break
		}
		if entry := entryOf(oldest); entry != nil {
			delete(s.m, entry.key)
		}
		s.lru.Remove(oldest)
	}

	entry := &bucketEntry{key: key, bucket: &bucket{tokens: s.burst - 1, last: now}}
	s.m[key] = s.lru.PushFront(entry)
	return true
}

// sweepStale removes buckets whose last access is older than maxAge.
// Returns the number of buckets evicted. Safe to call concurrently with
// allow() — uses the same mutex.
func (s *bucketStore) sweepStale(now time.Time, maxAge time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for {
		oldest := s.lru.Back()
		if oldest == nil {
			break
		}
		entry := entryOf(oldest)
		if entry == nil {
			// Corrupted: remove from list but skip the map delete.
			s.lru.Remove(oldest)
			continue
		}
		if now.Sub(entry.bucket.last) <= maxAge {
			break
		}
		delete(s.m, entry.key)
		s.lru.Remove(oldest)
		removed++
	}
	return removed
}

func (s *bucketStore) size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lru.Len()
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
