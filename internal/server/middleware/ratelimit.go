//go:build linux

package middleware

import (
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
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
	// EndpointCosts assigns a per-endpoint token cost so expensive
	// handlers (LLM, transcription, session create) drain the bucket
	// faster than cheap ones. Lookup is "METHOD PATH" first (e.g.
	// "POST /v1/dictation/transcribe"), then bare PATH, then default
	// 1.0. Zero or negative entries fall back to 1.0. (Audit S-4)
	EndpointCosts map[string]float64
	// DemoDailyQuota caps how many requests an identity with
	// Plan="demo" (the smoke-token surface) may make per UTC day,
	// keyed by UserID + client IP. Zero disables the quota. Public
	// paths bypass this check the same way they bypass token-bucket
	// rate limiting. (Audit S-5)
	DemoDailyQuota int
	// Now lets tests inject a fake clock for the daily-quota window
	// reset; production leaves it nil and gets time.Now.
	Now func() time.Time
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
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	buckets := &bucketStore{
		perSecond:  opts.RequestsPerSecond,
		burst:      float64(opts.Burst),
		maxBuckets: maxBuckets,
		m:          map[string]*list.Element{},
		lru:        list.New(),
		now:        now,
	}
	demoQuota := newDemoQuotaStore(opts.DemoDailyQuota, now)
	costs := normalizeEndpointCosts(opts.EndpointCosts)
	startSweeper(opts, buckets)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, public := publicSet[r.URL.Path]; public {
				next.ServeHTTP(w, r)
				return
			}
			key := rateLimitKey(r)
			cost := endpointCost(costs, r.Method, r.URL.Path)
			if !buckets.allowN(key, cost) {
				writeRateLimitError(w)
				return
			}
			if demoQuota != nil {
				id := IdentityFromContext(r.Context())
				if strings.EqualFold(id.Plan, "demo") {
					if retry, ok := demoQuota.allow(id.UserID, clientIPFromRemoteAddr(r)); !ok {
						writeDemoQuotaExceededError(w, retry)
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// normalizeEndpointCosts copies the user map with non-positive values
// dropped. Keys are kept verbatim so callers may use "METHOD PATH" or
// bare PATH form interchangeably.
func normalizeEndpointCosts(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for k, v := range in {
		if v <= 0 {
			continue
		}
		out[strings.TrimSpace(k)] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// endpointCost resolves the per-request token cost. Lookup order:
// "METHOD PATH" (more specific) > bare PATH > default 1.0. Method
// comparison is case-insensitive.
func endpointCost(costs map[string]float64, method, path string) float64 {
	if len(costs) == 0 {
		return 1
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if c, ok := costs[method+" "+path]; ok {
		return c
	}
	if c, ok := costs[path]; ok {
		return c
	}
	return 1
}

// clientIPFromRemoteAddr returns the host portion of r.RemoteAddr
// without the trailing :port. Falls back to the raw value when
// SplitHostPort fails (IPv6 missing brackets, malformed input).
func clientIPFromRemoteAddr(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return strings.TrimSpace(host)
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
	// Audit S-4: when the entire bearer-auth surface shares one
	// UserID="service" (the upstream service-to-service private-network
	// pattern), keying on that single string puts every consumer in
	// one bucket and a single noisy neighbour DoS-es everyone else.
	// Key on the remote IP instead so the budget is per-source.
	if id.UserID == "service" {
		if ip := clientIPFromRemoteAddr(r); ip != "" {
			return "svc:" + ip
		}
	}
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
	now        func() time.Time
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

// allow keeps the historical "draw 1 token" semantics for callers that
// don't supply a per-endpoint cost.
func (s *bucketStore) allow(key string) bool { return s.allowN(key, 1) }

// allowN attempts to draw `cost` tokens from the named bucket. Returns
// true when the bucket had enough tokens (and were debited), false
// when the request must be rejected. Cost <= 0 is treated as 1.
func (s *bucketStore) allowN(key string, cost float64) bool {
	if cost <= 0 {
		cost = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	clock := s.now
	if clock == nil {
		clock = time.Now
	}
	now := clock()
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
			if b.tokens >= cost {
				b.tokens -= cost
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

	// Fresh bucket starts at burst, debit cost.
	startTokens := s.burst - cost
	if startTokens < 0 {
		// Even a fresh bucket can't service this single request — fail
		// open on the bucket but still reject. Keep an empty bucket so
		// retries within the same instant continue to be rejected
		// rather than getting a brand-new burst.
		startTokens = 0
	}
	entry := &bucketEntry{key: key, bucket: &bucket{tokens: startTokens, last: now}}
	s.m[key] = s.lru.PushFront(entry)
	return s.burst >= cost
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

func writeDemoQuotaExceededError(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int(retryAfter.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    "demo_quota_exceeded",
			"message": fmt.Sprintf("daily quota exceeded for Plan=demo; resets in ~%ds", seconds),
		},
	})
}

// dailyQuotaStore tracks per-identity demo-tier request counts within
// the current UTC day. Resets atomically when a request crosses the
// next UTC-midnight boundary.
type dailyQuotaStore struct {
	mu      sync.Mutex
	quota   int
	resetAt time.Time
	counts  map[string]int
	now     func() time.Time
}

func newDemoQuotaStore(quota int, now func() time.Time) *dailyQuotaStore {
	if quota <= 0 {
		return nil
	}
	if now == nil {
		now = time.Now
	}
	return &dailyQuotaStore{
		quota:   quota,
		resetAt: nextUTCMidnight(now()),
		counts:  map[string]int{},
		now:     now,
	}
}

// allow increments the per-(user, ip) counter and reports whether the
// request fits inside the daily quota. The returned duration is the
// time until the next UTC-midnight reset and is the value the caller
// echoes back via Retry-After when ok=false.
func (d *dailyQuotaStore) allow(userID, clientIP string) (time.Duration, bool) {
	if d == nil {
		return 0, true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	if !now.Before(d.resetAt) {
		// Crossed UTC midnight — reset the window.
		for k := range d.counts {
			delete(d.counts, k)
		}
		d.resetAt = nextUTCMidnight(now)
	}
	key := "demo:" + userID + ":" + clientIP
	if d.counts[key] >= d.quota {
		return d.resetAt.Sub(now), false
	}
	d.counts[key]++
	return 0, true
}

// nextUTCMidnight returns the UTC midnight strictly after t.
func nextUTCMidnight(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC).Add(24 * time.Hour)
}
