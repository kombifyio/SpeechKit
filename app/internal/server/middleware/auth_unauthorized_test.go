//go:build linux

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuth_HTMLUnauthorizedPathReturnsBrowserResponse(t *testing.T) {
	t.Setenv("TEST_BEARER", "correct-horse-battery-staple")
	handler := Auth(AuthOptions{
		Mode:                      "bearer",
		BearerTokenEnv:            "TEST_BEARER",
		HTMLUnauthorizedPaths:     []string{"/setup", "/setup/"},
		AdminUsernameProvider:     func() string { return "admin" },
		AdminPasswordHashProvider: func() string { return "$2a$04$3ZQhRz6fJb3kQGN9cE1uD.RZ8c3E9oB3z4ED5CzSMYRhhAv7n4EHa" },
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("expected HTML content type, got %q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "SpeechKit Admin Sign-In Required") {
		t.Fatalf("expected browser auth page, got %s", body)
	}
	if strings.Contains(body, "sessionStorage") || strings.Contains(body, "localStorage") {
		t.Fatalf("browser auth page must not persist Basic credentials in web storage: %s", body)
	}
	if strings.Contains(body, "Kombify Cloud SSO") {
		t.Fatalf("browser auth page should not mention Kombify Cloud SSO: %s", body)
	}
	for _, want := range []string{"Admin username", "Admin password"} {
		if !strings.Contains(body, want) {
			t.Fatalf("browser auth page should contain %q, got %s", want, body)
		}
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("browser admin page must not trigger HTTP auth, got WWW-Authenticate=%q", got)
	}

	apiReq := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	apiReq.Header.Set("Accept", "application/json")
	apiRec := httptest.NewRecorder()
	handler.ServeHTTP(apiRec, apiReq)
	if apiRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected API 401, got %d", apiRec.Code)
	}
	if got := apiRec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected JSON content type for API auth failure, got %q", got)
	}
}

func TestAuth_HTMLUnauthorizedPathFallsBackToJSONWithoutAdminLogin(t *testing.T) {
	t.Setenv("TEST_BEARER", "correct-horse-battery-staple")
	handler := Auth(AuthOptions{
		Mode:                  "bearer",
		BearerTokenEnv:        "TEST_BEARER",
		HTMLUnauthorizedPaths: []string{"/setup", "/setup/"},
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected JSON content type when admin login is disabled, got %q", got)
	}
	if strings.Contains(rec.Body.String(), "SpeechKit Admin Sign-In Required") {
		t.Fatalf("disabled admin login should not render browser sign-in page: %s", rec.Body.String())
	}
}
