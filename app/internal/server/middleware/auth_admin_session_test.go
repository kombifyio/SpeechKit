//go:build linux

package middleware

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestAuth_AdminSessionAcceptsConfiguredPasswordHash(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	expires := time.Now().Add(adminSessionTTL)
	cookie := &http.Cookie{
		Name:  adminSessionCookieName,
		Value: signAdminSessionToken(adminSessionClaims{User: "admin", ExpiresAt: expires.Unix(), Nonce: "test"}, string(hash)),
		Path:  "/",
	}
	handler := Auth(AuthOptions{
		Mode:                      "bearer",
		BearerTokenProvider:       func() string { return "api-token" },
		AdminUsernameProvider:     func() string { return "admin" },
		AdminPasswordHashProvider: func() string { return string(hash) },
		TrustedProxyCIDRs:         []string{"203.0.113.0/24"},
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := IdentityFromContext(r.Context())
			if id.UserID != "admin" || id.Role != "admin" || id.Source != "admin_session" {
				t.Fatalf("identity = %+v, want admin session identity", id)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin session should authenticate, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuth_AdminSessionEndpointIssuesHttpOnlyCookie(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	handler := Auth(AuthOptions{
		Mode:                      "bearer",
		BearerTokenProvider:       func() string { return "api-token" },
		AdminUsernameProvider:     func() string { return "admin" },
		AdminPasswordHashProvider: func() string { return string(hash) },
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := IdentityFromContext(r.Context())
			if id.Source != "admin_session" {
				t.Fatalf("identity source = %q, want admin_session", id.Source)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	loginReq := httptest.NewRequest(http.MethodPost, "/v1/admin/session", nil)
	loginReq.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:correct-password")))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("admin session login got %d body=%s", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	// S-13: login now issues the session cookie AND a paired CSRF
	// cookie. Identify them by name so test order isn't load-bearing.
	if len(cookies) != 2 {
		t.Fatalf("login cookies = %d, want 2 (session + csrf)", len(cookies))
	}
	var cookie, csrf *http.Cookie
	for _, c := range cookies {
		switch c.Name {
		case adminSessionCookieName:
			cookie = c
		case adminCSRFCookieName:
			csrf = c
		}
	}
	if cookie == nil {
		t.Fatalf("missing %s cookie among %+v", adminSessionCookieName, cookies)
	}
	if csrf == nil {
		t.Fatalf("missing %s cookie among %+v", adminCSRFCookieName, cookies)
	}
	if !cookie.HttpOnly {
		t.Fatal("admin session cookie must be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("admin session SameSite = %v, want Lax", cookie.SameSite)
	}
	if csrf.HttpOnly {
		t.Fatal("CSRF cookie must NOT be HttpOnly — JS reads it for the X-CSRF-Token header")
	}
	if csrf.SameSite != http.SameSiteStrictMode {
		t.Fatalf("CSRF cookie SameSite = %v, want Strict", csrf.SameSite)
	}
	if csrf.Value == "" {
		t.Fatal("CSRF cookie value must be non-empty")
	}
	if csrf.Value != csrfTokenFor(cookie.Value) {
		t.Fatalf("CSRF cookie value does not match csrfTokenFor(session)")
	}
	payload, _, ok := strings.Cut(cookie.Value, ".")
	if !ok {
		t.Fatalf("admin session cookie is not signed: %q", cookie.Value)
	}
	decodedPayload, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode admin session payload: %v", err)
	}
	for _, forbidden := range []string{"correct-password", string(hash), "password"} {
		if strings.Contains(strings.ToLower(string(decodedPayload)), strings.ToLower(forbidden)) {
			t.Fatalf("admin session payload leaks password-derived material: %s", decodedPayload)
		}
	}

	setupReq := httptest.NewRequest(http.MethodGet, "/setup", nil)
	setupReq.AddCookie(cookie)
	setupRec := httptest.NewRecorder()
	handler.ServeHTTP(setupRec, setupReq)
	if setupRec.Code != http.StatusOK {
		t.Fatalf("setup with admin session got %d body=%s", setupRec.Code, setupRec.Body.String())
	}
}

func TestAuth_AdminSessionEndpointMarksCookiesSecureBehindHTTPSProxy(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	handler := Auth(AuthOptions{
		Mode:                      "bearer",
		BearerTokenProvider:       func() string { return "api-token" },
		AdminUsernameProvider:     func() string { return "admin" },
		AdminPasswordHashProvider: func() string { return string(hash) },
		TrustedProxyCIDRs:         []string{"203.0.113.0/24"},
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	loginReq := httptest.NewRequest(http.MethodPost, "/v1/admin/session", nil)
	loginReq.RemoteAddr = "203.0.113.10:4321"
	loginReq.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:correct-password")))
	loginReq.Header.Set("X-Forwarded-Proto", "https")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("admin session login got %d body=%s", loginRec.Code, loginRec.Body.String())
	}
	for _, cookie := range loginRec.Result().Cookies() {
		if cookie.Name != adminSessionCookieName && cookie.Name != adminCSRFCookieName {
			continue
		}
		if !cookie.Secure {
			t.Fatalf("%s must be Secure behind HTTPS proxy", cookie.Name)
		}
	}
}

func TestAuth_AdminSessionEndpointIgnoresForwardedProtoFromUntrustedRemote(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	handler := Auth(AuthOptions{
		Mode:                      "bearer",
		BearerTokenProvider:       func() string { return "api-token" },
		AdminUsernameProvider:     func() string { return "admin" },
		AdminPasswordHashProvider: func() string { return string(hash) },
		TrustedProxyCIDRs:         []string{"203.0.113.0/24"},
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	loginReq := httptest.NewRequest(http.MethodPost, "/v1/admin/session", nil)
	loginReq.RemoteAddr = "198.51.100.10:4321"
	loginReq.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:correct-password")))
	loginReq.Header.Set("X-Forwarded-Proto", "https")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("admin session login got %d body=%s", loginRec.Code, loginRec.Body.String())
	}
	for _, cookie := range loginRec.Result().Cookies() {
		if cookie.Name != adminSessionCookieName && cookie.Name != adminCSRFCookieName {
			continue
		}
		if cookie.Secure {
			t.Fatalf("%s must not trust X-Forwarded-Proto from untrusted remote", cookie.Name)
		}
	}
}

func TestAuth_AdminSessionStateChangeRequiresCSRFHeader(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	handler := Auth(AuthOptions{
		Mode:                      "bearer",
		BearerTokenProvider:       func() string { return "api-token" },
		AdminUsernameProvider:     func() string { return "admin" },
		AdminPasswordHashProvider: func() string { return string(hash) },
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !EnforceAdminCSRF(w, r) {
				return
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	loginReq := httptest.NewRequest(http.MethodPost, "/v1/admin/session", nil)
	loginReq.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:correct-password")))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("admin session login got %d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range loginRec.Result().Cookies() {
		switch cookie.Name {
		case adminSessionCookieName:
			sessionCookie = cookie
		case adminCSRFCookieName:
			csrfCookie = cookie
		}
	}
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatalf("login did not issue session and csrf cookies: %+v", loginRec.Result().Cookies())
	}

	missingHeaderReq := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	missingHeaderReq.AddCookie(sessionCookie)
	missingHeaderReq.AddCookie(csrfCookie)
	missingHeaderRec := httptest.NewRecorder()
	handler.ServeHTTP(missingHeaderRec, missingHeaderReq)
	if missingHeaderRec.Code != http.StatusForbidden {
		t.Fatalf("admin session write without CSRF should get 403, got %d", missingHeaderRec.Code)
	}

	allowedReq := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	allowedReq.AddCookie(sessionCookie)
	allowedReq.AddCookie(csrfCookie)
	allowedReq.Header.Set(adminCSRFHeaderName, csrfCookie.Value)
	allowedRec := httptest.NewRecorder()
	handler.ServeHTTP(allowedRec, allowedReq)
	if allowedRec.Code != http.StatusOK {
		t.Fatalf("admin session write with CSRF should pass, got %d body=%s", allowedRec.Code, allowedRec.Body.String())
	}
}

func TestAuth_AdminSessionEndpointClearsCookie(t *testing.T) {
	handler := Auth(AuthOptions{Mode: "bearer"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/session", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout got %d", rec.Code)
	}
	cookies := rec.Result().Cookies()
	// S-13: logout also clears the paired CSRF cookie.
	if len(cookies) != 2 {
		t.Fatalf("logout cookies = %d, want 2 (cleared session + cleared csrf)", len(cookies))
	}
	seenSession := false
	seenCSRF := false
	for _, c := range cookies {
		if c.MaxAge >= 0 {
			t.Fatalf("logout cookie %q has MaxAge = %d, want negative (cleared)", c.Name, c.MaxAge)
		}
		switch c.Name {
		case adminSessionCookieName:
			seenSession = true
		case adminCSRFCookieName:
			seenCSRF = true
		}
	}
	if !seenSession || !seenCSRF {
		t.Fatalf("logout did not clear both cookies: session=%v csrf=%v (cookies=%+v)", seenSession, seenCSRF, cookies)
	}
}
