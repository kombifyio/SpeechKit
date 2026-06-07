//go:build linux

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serveWithSecurity(opts SecurityHeadersOptions, inner http.HandlerFunc) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	SecurityHeaders(opts)(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	return rec
}

func TestSecurityHeaders_Defaults(t *testing.T) {
	rec := serveWithSecurity(SecurityHeadersOptions{}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := rec.Header()
	if got := h.Get("Content-Security-Policy"); got != DefaultContentSecurityPolicy {
		t.Errorf("CSP = %q, want default %q", got, DefaultContentSecurityPolicy)
	}
	if strings.Contains(h.Get("Content-Security-Policy"), "unsafe-inline") {
		t.Error("default CSP must not contain 'unsafe-inline'")
	}
	if h.Get("X-Frame-Options") != "DENY" {
		t.Errorf("X-Frame-Options = %q", h.Get("X-Frame-Options"))
	}
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", h.Get("X-Content-Type-Options"))
	}
	if h.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", h.Get("Referrer-Policy"))
	}
	if h.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS must be off by default")
	}
}

func TestSecurityHeaders_Overrides(t *testing.T) {
	rec := serveWithSecurity(SecurityHeadersOptions{
		ContentSecurityPolicy: "default-src 'self'",
		FrameOptions:          "SAMEORIGIN",
		ReferrerPolicy:        "strict-origin",
		HSTS:                  true,
		HSTSMaxAgeSeconds:     100,
	}, func(http.ResponseWriter, *http.Request) {})
	h := rec.Header()
	if h.Get("Content-Security-Policy") != "default-src 'self'" {
		t.Errorf("CSP override lost: %q", h.Get("Content-Security-Policy"))
	}
	if h.Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Errorf("frame override lost: %q", h.Get("X-Frame-Options"))
	}
	if h.Get("Referrer-Policy") != "strict-origin" {
		t.Errorf("referrer override lost: %q", h.Get("Referrer-Policy"))
	}
	if got := h.Get("Strict-Transport-Security"); got != "max-age=100; includeSubDomains" {
		t.Errorf("HSTS = %q", got)
	}
}

func TestSecurityHeaders_HSTSDefaultMaxAge(t *testing.T) {
	rec := serveWithSecurity(SecurityHeadersOptions{HSTS: true}, func(http.ResponseWriter, *http.Request) {})
	if got := rec.Header().Get("Strict-Transport-Security"); !strings.HasPrefix(got, "max-age=63072000") {
		t.Errorf("default HSTS max-age = %q", got)
	}
}

// A handler may override the strict default CSP for its own response — the
// path the admin sign-in HTML page relies on.
func TestSecurityHeaders_HandlerCanOverrideCSP(t *testing.T) {
	const pageCSP = "default-src 'none'; script-src 'sha256-abc'"
	rec := serveWithSecurity(SecurityHeadersOptions{}, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Security-Policy", pageCSP)
		w.WriteHeader(http.StatusOK)
	})
	if got := rec.Header().Get("Content-Security-Policy"); got != pageCSP {
		t.Errorf("handler CSP override lost: %q", got)
	}
}

// The admin sign-in page must ship a CSP that allow-lists exactly its own
// inline style+script by hash (no 'unsafe-inline'), and the served bodies must
// match the hashed constants so the two can never drift.
func TestWriteBrowserAuthError_CSPHashesInlineAssets(t *testing.T) {
	rec := httptest.NewRecorder()
	writeBrowserAuthError(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))

	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" || strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("admin page CSP must be set and free of 'unsafe-inline': %q", csp)
	}
	if !strings.Contains(csp, cspSourceHash(adminAuthErrorStyleCSS)) {
		t.Error("CSP is missing the inline <style> hash")
	}
	if !strings.Contains(csp, cspSourceHash(adminAuthErrorScriptJS)) {
		t.Error("CSP is missing the inline <script> hash")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "<style>"+adminAuthErrorStyleCSS+"</style>") {
		t.Error("served <style> body does not match the hashed constant")
	}
	if !strings.Contains(body, "<script>"+adminAuthErrorScriptJS+"</script>") {
		t.Error("served <script> body does not match the hashed constant")
	}
}
