//go:build linux

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
