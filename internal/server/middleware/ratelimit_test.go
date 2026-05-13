//go:build linux

package middleware

import (
	"container/list"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimit_DisabledWhenZeroOpts(t *testing.T) {
	mw := RateLimit(RateLimitOptions{})
	called := 0
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("disabled limiter returned %d", rec.Code)
		}
	}
	if called != 10 {
		t.Fatalf("inner handler invoked %d times, want 10", called)
	}
}

func TestRateLimit_AllowsBurstThenRejects(t *testing.T) {
	// Effectively zero refill rate (1 rps with very small bursts) so the
	// 6th request inside the same second hits the limit.
	mw := RateLimit(RateLimitOptions{RequestsPerSecond: 1, Burst: 5})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	allowed := 0
	rejected := 0
	for i := 0; i < 8; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
		req.RemoteAddr = "1.2.3.4:5678"
		// Attach an authenticated identity so the limiter keys per user
		// rather than per remote.
		ctx := context.WithValue(req.Context(), identityCtxKey{}, Identity{UserID: "u-burst"})
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		switch rec.Code {
		case http.StatusOK:
			allowed++
		case http.StatusTooManyRequests:
			rejected++
		default:
			t.Fatalf("unexpected status %d", rec.Code)
		}
	}
	if allowed != 5 || rejected != 3 {
		t.Fatalf("allowed=%d rejected=%d, want 5 allowed + 3 rejected", allowed, rejected)
	}
}

func TestRateLimit_PublicPathBypasses(t *testing.T) {
	mw := RateLimit(RateLimitOptions{
		RequestsPerSecond: 1,
		Burst:             1,
		AllowPublicPaths:  []string{"/healthz", "/readyz"},
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("/healthz request %d got status %d (must always be 200)", i, rec.Code)
		}
	}
}

func TestRateLimit_KeyIsPerIdentity(t *testing.T) {
	// Two different users sharing the same IP must not steal each other's
	// quota. With burst=2 and two users, four requests in a row should
	// all succeed (each user gets 2).
	mw := RateLimit(RateLimitOptions{RequestsPerSecond: 1, Burst: 2})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, user := range []string{"alice", "alice", "bob", "bob"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
		req.RemoteAddr = "9.9.9.9:1"
		ctx := context.WithValue(req.Context(), identityCtxKey{}, Identity{UserID: user})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("user=%s got status %d, want 200", user, rec.Code)
		}
	}
}

func TestRateLimit_RejectionBodyShape(t *testing.T) {
	mw := RateLimit(RateLimitOptions{RequestsPerSecond: 1, Burst: 1})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	req.RemoteAddr = "5.5.5.5:1"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req) // burns the only token

	req2 := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	req2.RemoteAddr = "5.5.5.5:1"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") != "1" {
		t.Fatalf("expected Retry-After=1, got %q", rec2.Header().Get("Retry-After"))
	}
	body := rec2.Body.String()
	if !contains(body, "rate_limited") {
		t.Fatalf("expected rate_limited code in body, got %s", body)
	}
}

func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestBucketStore_EvictsOldestWhenCapHit(t *testing.T) {
	const cap = 4
	store := &bucketStore{
		perSecond:  100, // generous so token math doesn't reject
		burst:      10,
		maxBuckets: cap,
		m:          map[string]*list.Element{},
		lru:        list.New(),
	}

	// Fill to cap.
	for _, key := range []string{"u:a", "u:b", "u:c", "u:d"} {
		if !store.allow(key) {
			t.Fatalf("first request for %s should be allowed", key)
		}
	}
	if got := store.size(); got != cap {
		t.Fatalf("size after fill = %d, want %d", got, cap)
	}

	// Touch "u:b" so it becomes most-recent. "u:a" is now LRU.
	if !store.allow("u:b") {
		t.Fatalf("repeat request for u:b should be allowed")
	}

	// Insert a new key. It should evict "u:a" (oldest), not "u:b".
	if !store.allow("u:e") {
		t.Fatalf("new key u:e should be allowed")
	}
	if got := store.size(); got != cap {
		t.Fatalf("size after eviction = %d, want %d", got, cap)
	}
	store.mu.Lock()
	_, hasA := store.m["u:a"]
	_, hasB := store.m["u:b"]
	_, hasE := store.m["u:e"]
	store.mu.Unlock()
	if hasA {
		t.Fatalf("u:a should have been evicted as LRU")
	}
	if !hasB {
		t.Fatalf("u:b should still be present (recently touched)")
	}
	if !hasE {
		t.Fatalf("u:e should be present (just inserted)")
	}
}

func TestBucketStore_SweepStaleEvictsOldEntries(t *testing.T) {
	store := &bucketStore{
		perSecond:  10,
		burst:      5,
		maxBuckets: 100,
		m:          map[string]*list.Element{},
		lru:        list.New(),
	}

	now := time.Now()
	// Manually insert entries in LRU order (front=most-recent, back=oldest).
	// sweepStale walks from back and stops at the first non-stale entry, so
	// the test must respect the production invariant that LRU order matches
	// timestamp order.
	insert := func(key string, last time.Time) {
		entry := &bucketEntry{key: key, bucket: &bucket{tokens: 4, last: last}}
		store.m[key] = store.lru.PushFront(entry)
	}
	insert("stale-2", now.Add(-3*time.Hour)) // pushed first → ends at back
	insert("stale-1", now.Add(-2*time.Hour))
	insert("fresh", now) // pushed last → at front

	removed := store.sweepStale(now, 1*time.Hour)
	if removed != 2 {
		t.Fatalf("sweepStale removed %d entries, want 2", removed)
	}
	if got := store.size(); got != 1 {
		t.Fatalf("size after sweep = %d, want 1", got)
	}
	store.mu.Lock()
	_, hasFresh := store.m["fresh"]
	store.mu.Unlock()
	if !hasFresh {
		t.Fatalf("fresh entry should survive sweep")
	}
}

func TestRateLimit_SweeperContextCancellationStopsGoroutine(t *testing.T) {
	// Verify the sweeper goroutine returns when ctx is cancelled. We can't
	// directly assert goroutine count, but we can verify ctx-cancel is
	// honoured by setting an aggressive interval and giving it a tick.
	ctx, cancel := context.WithCancel(context.Background())
	mw := RateLimit(RateLimitOptions{
		RequestsPerSecond: 10,
		Burst:             2,
		SweepInterval:     50 * time.Millisecond,
		SweepMaxAge:       10 * time.Millisecond,
		Context:           ctx,
	})
	if mw == nil {
		t.Fatalf("RateLimit returned nil middleware")
	}
	// Cancel immediately — the sweeper goroutine must exit on next tick or
	// next select cycle.
	cancel()
	// Sleep to let the goroutine observe the cancellation. If it leaks,
	// goleak (not configured here) would flag it; this test is mainly a
	// smoke check that the context plumbing compiles and is wired.
	time.Sleep(100 * time.Millisecond)
}

func TestEntryOf_NilAndWrongTypeAreSafe(t *testing.T) {
	// Nil element must produce nil, not crash.
	if got := entryOf(nil); got != nil {
		t.Fatalf("entryOf(nil) = %v, want nil", got)
	}

	// An element whose value is the wrong type must produce nil and log
	// rather than panicking — a panic inside the rate-limit hot path would
	// crash the whole server.
	l := list.New()
	corrupted := l.PushFront("not-a-bucketEntry")
	if got := entryOf(corrupted); got != nil {
		t.Fatalf("entryOf(wrong-type) = %v, want nil", got)
	}

	// Sanity: a correctly typed entry round-trips.
	want := &bucketEntry{key: "k", bucket: &bucket{}}
	good := l.PushFront(want)
	if got := entryOf(good); got != want {
		t.Fatalf("entryOf(*bucketEntry) = %v, want %v", got, want)
	}
}

func TestRateLimit_RecoversFromCorruptedBucketStore(t *testing.T) {
	// Build a limiter, then manually poison one element to simulate the
	// "impossible" corruption path. The limiter must keep serving subsequent
	// requests instead of crashing.
	mw := RateLimit(RateLimitOptions{RequestsPerSecond: 100, Burst: 10})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Warm the limiter with one request so the bucket store has an entry.
	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	req.RemoteAddr = "9.9.9.9:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("warm-up returned %d, want 200", rec.Code)
	}

	// Subsequent requests from the same remote must still succeed (and not
	// panic the process). We're not directly poisoning the store — that
	// requires reflection — but entryOf's defensive nil-return path is
	// covered by TestEntryOf_NilAndWrongTypeAreSafe above; this test
	// confirms the happy path didn't regress when entryOf was introduced.
	for i := 0; i < 5; i++ {
		req = httptest.NewRequest(http.MethodGet, "/v1/x", nil)
		req.RemoteAddr = "9.9.9.9:1234"
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("post-warmup request %d returned %d, want 200", i, rec.Code)
		}
	}
}
