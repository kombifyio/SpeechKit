package controlplane

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGuardRejectsCrossSiteMutatingRequests(t *testing.T) {
	handler := Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), "")
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", http.NoBody)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestGuardAllowsWailsLocalhostOriginWithSessionHeader(t *testing.T) {
	handler := Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), "test-token")
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", http.NoBody)
	req.Header.Set("Origin", "https://settings.wails.localhost")
	req.Header.Set(TokenHeaderName, "test-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get(TokenHeaderName); got != "test-token" {
		t.Fatalf("%s = %q, want %q", TokenHeaderName, got, "test-token")
	}
}

func TestHasValidTokenHeaderFailsClosed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", http.NoBody)
	req.Header.Set(TokenHeaderName, "test-token")

	if HasValidTokenHeader(req, "") {
		t.Fatal("empty expected token was accepted")
	}
}
