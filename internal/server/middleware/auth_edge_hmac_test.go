//go:build linux

package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuth_EdgeHMACValidSignature(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := IdentityFromContext(r.Context())
			if id.UserID != "user-42" || id.OrgID != "org-kombify" || id.Role != "admin" || id.Source != "edge_hmac" {
				t.Fatalf("unexpected identity: %+v", id)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	req.Header.Set("X-Edge-User-Id", "user-42")
	req.Header.Set("X-Edge-Org-Id", "org-kombify")
	req.Header.Set("X-Edge-Plan", "pro")
	req.Header.Set("X-Edge-Role", "admin")
	mac := hmac.New(sha256.New, []byte("edge-secret-xyz"))
	mac.Write([]byte("user-42\norg-kombify\npro\nadmin"))
	req.Header.Set("X-Edge-Auth-Hmac", hex.EncodeToString(mac.Sum(nil)))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuth_EdgeHMACRejectsTamperedRole(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	req.Header.Set("X-Edge-User-Id", "user-42")
	req.Header.Set("X-Edge-Org-Id", "org-kombify")
	req.Header.Set("X-Edge-Plan", "pro")
	req.Header.Set("X-Edge-Role", "admin")
	mac := hmac.New(sha256.New, []byte("edge-secret-xyz"))
	mac.Write([]byte("user-42\norg-kombify\npro\n"))
	req.Header.Set("X-Edge-Auth-Hmac", hex.EncodeToString(mac.Sum(nil)))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for tampered role, got %d", rec.Code)
	}
}

func TestAuth_EdgeHMACRejectsTamperedSignature(t *testing.T) {
	t.Setenv("TEST_EDGE_SECRET", "edge-secret-xyz")
	handler := Auth(AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	req.Header.Set("X-Edge-User-Id", "user-42")
	req.Header.Set("X-Edge-Org-Id", "org-kombify")
	req.Header.Set("X-Edge-Plan", "pro")
	req.Header.Set("X-Edge-Auth-Hmac", "deadbeef")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
