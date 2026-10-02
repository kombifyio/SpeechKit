//go:build linux

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func serveWithRequestID(t *testing.T, inbound string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	var seen string
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	if inbound != "" {
		req.Header.Set(RequestIDHeader, inbound)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, seen
}

func TestRequestID_GeneratesWhenAbsent(t *testing.T) {
	rec, seen := serveWithRequestID(t, "")
	if seen == "" {
		t.Fatal("no request id in context")
	}
	if rec.Header().Get(RequestIDHeader) != seen {
		t.Errorf("response header %q != context id %q", rec.Header().Get(RequestIDHeader), seen)
	}
	if len(seen) != 32 { // 16 random bytes hex-encoded
		t.Errorf("generated id has unexpected length %d", len(seen))
	}
}

func TestRequestID_ReusesValidInbound(t *testing.T) {
	const inbound = "edge-abc.123_XYZ"
	rec, seen := serveWithRequestID(t, inbound)
	if seen != inbound {
		t.Errorf("valid inbound id not reused: got %q", seen)
	}
	if rec.Header().Get(RequestIDHeader) != inbound {
		t.Errorf("response header = %q, want %q", rec.Header().Get(RequestIDHeader), inbound)
	}
}

func TestRequestID_ReplacesInvalidInbound(t *testing.T) {
	for _, bad := range []string{"has space", "inject\r\nX", "weird;char", string(make([]byte, 200))} {
		_, seen := serveWithRequestID(t, bad)
		if seen == bad || seen == "" {
			t.Errorf("invalid inbound %q should be replaced with a fresh id, got %q", bad, seen)
		}
	}
}

func TestValidRequestID(t *testing.T) {
	good := []string{"abc", "A1-b2_c3.d4", "0123456789"}
	bad := []string{"", "with space", "tab\t", "semi;colon", "slash/x"}
	for _, g := range good {
		if !validRequestID(g) {
			t.Errorf("validRequestID(%q) = false, want true", g)
		}
	}
	for _, b := range bad {
		if validRequestID(b) {
			t.Errorf("validRequestID(%q) = true, want false", b)
		}
	}
}
