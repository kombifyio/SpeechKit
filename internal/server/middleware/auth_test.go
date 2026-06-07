//go:build linux

package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestAuth_BearerRejectsMissingHeader(t *testing.T) {
	t.Setenv("TEST_BEARER", "correct-horse-battery-staple")
	handler := Auth(AuthOptions{Mode: "bearer", BearerTokenEnv: "TEST_BEARER"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuth_BearerAcceptsMatch(t *testing.T) {
	t.Setenv("TEST_BEARER", "correct-horse-battery-staple")
	called := false
	handler := Auth(AuthOptions{Mode: "bearer", BearerTokenEnv: "TEST_BEARER"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			id := IdentityFromContext(r.Context())
			if id.Source != "bearer" {
				t.Fatalf("identity source should be bearer, got %q", id.Source)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	req.Header.Set("Authorization", "Bearer correct-horse-battery-staple")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatalf("inner handler should have been invoked")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAuth_BearerCanAttachConfiguredRole(t *testing.T) {
	t.Setenv("TEST_BEARER", "correct-horse-battery-staple")
	handler := Auth(AuthOptions{
		Mode:           "bearer",
		BearerTokenEnv: "TEST_BEARER",
		BearerRole:     "admin",
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := IdentityFromContext(r.Context())
			if id.Role != "admin" {
				t.Fatalf("identity role = %q, want admin", id.Role)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	req.Header.Set("Authorization", "Bearer correct-horse-battery-staple")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

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

func TestAdminCSRF_TokenIsDeterministicForSessionToken(t *testing.T) {
	tok := csrfTokenFor("session-cookie-value")
	again := csrfTokenFor("session-cookie-value")
	if tok != again {
		t.Fatalf("csrfTokenFor is not deterministic: %q vs %q", tok, again)
	}
	other := csrfTokenFor("different-session-value")
	if tok == other {
		t.Fatalf("csrfTokenFor returned the same token for different sessions")
	}
	if tok == "" {
		t.Fatal("csrfTokenFor returned empty token")
	}
}

func TestValidateAdminCSRF_RejectsMissingHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "sess.abc"})
	req.AddCookie(&http.Cookie{Name: adminCSRFCookieName, Value: csrfTokenFor("sess.abc")})
	if ValidateAdminCSRF(req) {
		t.Fatal("ValidateAdminCSRF must require X-CSRF-Token header")
	}
}

func TestValidateAdminCSRF_RejectsMissingCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	req.Header.Set(adminCSRFHeaderName, "anything")
	if ValidateAdminCSRF(req) {
		t.Fatal("ValidateAdminCSRF must require CSRF cookie")
	}
}

func TestValidateAdminCSRF_RejectsHeaderCookieMismatch(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "sess.abc"})
	req.AddCookie(&http.Cookie{Name: adminCSRFCookieName, Value: csrfTokenFor("sess.abc")})
	req.Header.Set(adminCSRFHeaderName, "wrong-token")
	if ValidateAdminCSRF(req) {
		t.Fatal("ValidateAdminCSRF must reject header that does not match cookie")
	}
}

func TestValidateAdminCSRF_RejectsStaleCSRFAgainstFreshSession(t *testing.T) {
	// CSRF cookie was minted for an older session; current session
	// cookie is different. The HMAC binding must catch the mismatch.
	staleCSRF := csrfTokenFor("old-session-value")
	req := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "new-session-value"})
	req.AddCookie(&http.Cookie{Name: adminCSRFCookieName, Value: staleCSRF})
	req.Header.Set(adminCSRFHeaderName, staleCSRF)
	if ValidateAdminCSRF(req) {
		t.Fatal("ValidateAdminCSRF must reject stale CSRF bound to a different session")
	}
}

func TestValidateAdminCSRF_AcceptsMatchedTriple(t *testing.T) {
	const sess = "sess.xyz"
	tok := csrfTokenFor(sess)
	req := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: sess})
	req.AddCookie(&http.Cookie{Name: adminCSRFCookieName, Value: tok})
	req.Header.Set(adminCSRFHeaderName, tok)
	if !ValidateAdminCSRF(req) {
		t.Fatal("ValidateAdminCSRF must accept a matched (session-cookie, csrf-cookie, header) triple")
	}
}

func TestEnforceAdminCSRF_BypassesNonAdminSessionIdentity(t *testing.T) {
	// Bearer / edge-HMAC / smoke callers must NOT be required to send
	// X-CSRF-Token — their credentials are not browser-attached.
	for _, source := range []string{"bearer", "edge_hmac", "smoke", "none", ""} {
		t.Run("source="+source, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
			ctx := InjectIdentityForTest(req.Context(), Identity{UserID: "svc", Source: source})
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()
			if !EnforceAdminCSRF(rec, req) {
				t.Fatalf("EnforceAdminCSRF must bypass when Source=%q (no CSRF needed)", source)
			}
			if rec.Code != http.StatusOK { // unwritten = default 200
				t.Fatalf("EnforceAdminCSRF wrote a response for Source=%q (code=%d)", source, rec.Code)
			}
		})
	}
}

func TestEnforceAdminCSRF_RejectsAdminSessionWithoutToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	ctx := InjectIdentityForTest(req.Context(), Identity{UserID: "admin", Source: "admin_session", Role: "admin"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	if EnforceAdminCSRF(rec, req) {
		t.Fatal("EnforceAdminCSRF must reject admin-session caller without CSRF token")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "csrf_required") {
		t.Fatalf("body = %q, want csrf_required envelope", rec.Body.String())
	}
}

func TestAuth_BearerReadsTokenProviderPerRequest(t *testing.T) {
	token := ""
	handler := Auth(AuthOptions{
		Mode:                "bearer",
		BearerTokenProvider: func() string { return token },
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	reqBefore := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	reqBefore.Header.Set("Authorization", "Bearer generated-token")
	recBefore := httptest.NewRecorder()
	handler.ServeHTTP(recBefore, reqBefore)
	if recBefore.Code != http.StatusUnauthorized {
		t.Fatalf("empty generated token should fail closed, got %d", recBefore.Code)
	}

	token = "generated-token"
	reqAfter := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	reqAfter.Header.Set("Authorization", "Bearer generated-token")
	recAfter := httptest.NewRecorder()
	handler.ServeHTTP(recAfter, reqAfter)
	if recAfter.Code != http.StatusOK {
		t.Fatalf("updated generated token should authenticate, got %d", recAfter.Code)
	}
}

func TestAuth_BearerRejectsWrongToken(t *testing.T) {
	t.Setenv("TEST_BEARER", "correct-horse-battery-staple")
	handler := Auth(AuthOptions{Mode: "bearer", BearerTokenEnv: "TEST_BEARER"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuth_PublicPathBypassesCheck(t *testing.T) {
	t.Setenv("TEST_BEARER", "correct-horse-battery-staple")
	handler := Auth(AuthOptions{
		Mode:             "bearer",
		BearerTokenEnv:   "TEST_BEARER",
		AllowPublicPaths: []string{"/healthz"},
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("healthz should bypass auth; got %d", rec.Code)
	}
}

func TestAuth_BootstrapRouteBypassesOnlyWhenAllowed(t *testing.T) {
	bootstrapAllowed := true
	handler := Auth(AuthOptions{
		Mode: "bearer",
		AllowBootstrapRoutes: []PublicRoute{
			{Path: "/v1/server/settings", Methods: []string{http.MethodPatch}},
		},
		BootstrapAllowed: func(*http.Request) bool { return bootstrapAllowed },
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	bootstrapReq := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	bootstrapRec := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapRec, bootstrapReq)
	if bootstrapRec.Code != http.StatusOK {
		t.Fatalf("bootstrap settings write should bypass auth, got %d", bootstrapRec.Code)
	}

	bootstrapAllowed = false
	rejectedReq := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	rejectedRec := httptest.NewRecorder()
	handler.ServeHTTP(rejectedRec, rejectedReq)
	if rejectedRec.Code != http.StatusUnauthorized {
		t.Fatalf("bootstrap settings write should require auth after bootstrap, got %d", rejectedRec.Code)
	}
}

func TestAuth_BootstrapPathBypassesOnlyWhenAllowed(t *testing.T) {
	bootstrapAllowed := true
	handler := Auth(AuthOptions{
		Mode:                "bearer",
		AllowBootstrapPaths: []string{"/setup"},
		BootstrapAllowed:    func(*http.Request) bool { return bootstrapAllowed },
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	bootstrapReq := httptest.NewRequest(http.MethodGet, "/setup", nil)
	bootstrapRec := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapRec, bootstrapReq)
	if bootstrapRec.Code != http.StatusOK {
		t.Fatalf("bootstrap setup path should bypass auth, got %d", bootstrapRec.Code)
	}

	bootstrapAllowed = false
	rejectedReq := httptest.NewRequest(http.MethodGet, "/setup", nil)
	rejectedRec := httptest.NewRecorder()
	handler.ServeHTTP(rejectedRec, rejectedReq)
	if rejectedRec.Code != http.StatusUnauthorized {
		t.Fatalf("bootstrap setup path should require auth after bootstrap, got %d", rejectedRec.Code)
	}
}

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

func TestAuth_PublicRouteBypassesOnlyConfiguredMethods(t *testing.T) {
	t.Setenv("TEST_BEARER", "correct-horse-battery-staple")
	handler := Auth(AuthOptions{
		Mode:           "bearer",
		BearerTokenEnv: "TEST_BEARER",
		AllowPublicRoutes: []PublicRoute{
			{Path: "/v1/server/settings", Methods: []string{http.MethodGet, http.MethodHead}},
		},
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	readReq := httptest.NewRequest(http.MethodGet, "/v1/server/settings", nil)
	readRec := httptest.NewRecorder()
	handler.ServeHTTP(readRec, readReq)
	if readRec.Code != http.StatusOK {
		t.Fatalf("settings GET should bypass auth; got %d", readRec.Code)
	}

	writeReq := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	writeRec := httptest.NewRecorder()
	handler.ServeHTTP(writeRec, writeReq)
	if writeRec.Code != http.StatusUnauthorized {
		t.Fatalf("settings PATCH without auth should be rejected; got %d", writeRec.Code)
	}

	authorizedWrite := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	authorizedWrite.Header.Set("Authorization", "Bearer correct-horse-battery-staple")
	authorizedRec := httptest.NewRecorder()
	handler.ServeHTTP(authorizedRec, authorizedWrite)
	if authorizedRec.Code != http.StatusOK {
		t.Fatalf("settings PATCH with auth should pass; got %d", authorizedRec.Code)
	}
}

func TestAuth_PublicRouteCanMatchPrefixAndSuffix(t *testing.T) {
	t.Setenv("TEST_BEARER", "correct-horse-battery-staple")
	handler := Auth(AuthOptions{
		Mode:           "bearer",
		BearerTokenEnv: "TEST_BEARER",
		AllowPublicRoutes: []PublicRoute{
			{PathPrefix: "/v1/voiceagent/sessions/", PathSuffix: "/ws", Methods: []string{http.MethodGet}},
		},
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	wsReq := httptest.NewRequest(http.MethodGet, "/v1/voiceagent/sessions/session-1/ws?ticket=t", nil)
	wsRec := httptest.NewRecorder()
	handler.ServeHTTP(wsRec, wsReq)
	if wsRec.Code != http.StatusOK {
		t.Fatalf("ticket websocket route should bypass auth; got %d", wsRec.Code)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/v1/voiceagent/sessions/session-1/ws?ticket=t", nil)
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong method should require auth; got %d", postRec.Code)
	}

	transcriptReq := httptest.NewRequest(http.MethodGet, "/v1/voiceagent/sessions/session-1/transcript", nil)
	transcriptRec := httptest.NewRecorder()
	handler.ServeHTTP(transcriptRec, transcriptReq)
	if transcriptRec.Code != http.StatusUnauthorized {
		t.Fatalf("other session routes should require auth; got %d", transcriptRec.Code)
	}
}

func TestAuth_NoneAllowsRequestAndAttachesAnonymousIdentity(t *testing.T) {
	called := false
	handler := Auth(AuthOptions{Mode: "none"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			id := IdentityFromContext(r.Context())
			if id.UserID != "anonymous" || id.OrgID != "public" || id.Plan != "public" || id.Source != "none" {
				t.Fatalf("unexpected anonymous identity: %+v", id)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/voiceagent/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatalf("inner handler should have been invoked")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuth_NoneRefusedWhenRequireAuthenticatedMode(t *testing.T) {
	// Defence-in-depth on top of config.ValidateServerProductionAuth.
	// When RequireAuthenticatedMode is true (set by bootstrap for
	// non-loopback binds), AuthModeNone must NOT issue the anonymous
	// Identity — the request falls through to writeAuthError → 401.
	handler := Auth(AuthOptions{Mode: "none", RequireAuthenticatedMode: true})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/voiceagent/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("RequireAuthenticatedMode must refuse anonymous when mode=none; got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuth_EmptyModeRefusedWhenRequireAuthenticatedMode(t *testing.T) {
	// Empty Mode resolves to AuthModeNone at the mode-resolution step
	// (auth.go:344). RequireAuthenticatedMode must still block.
	handler := Auth(AuthOptions{Mode: "", RequireAuthenticatedMode: true})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("empty mode + RequireAuthenticatedMode must refuse; got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuth_SmokeFallbackStillWorksWithRequireAuthenticatedMode(t *testing.T) {
	// Defence-in-depth must not break the smoke path: a valid smoke
	// token with mode=none + RequireAuthenticatedMode=true should still
	// authenticate via the smoke fallback. Only the implicit anonymous
	// identity is suppressed.
	smokeProvider := func() string { return "demo-token-xyz" }
	handler := Auth(AuthOptions{
		Mode:                     "none",
		RequireAuthenticatedMode: true,
		SmokeTokenProvider:       smokeProvider,
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := IdentityFromContext(r.Context())
			if id.Source != "smoke" {
				t.Fatalf("expected smoke identity, got %+v", id)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/voiceagent/sessions", nil)
	req.Header.Set("Authorization", "Bearer demo-token-xyz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("smoke fallback must work when RequireAuthenticatedMode is on; got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuth_BearerEmptyTokenFailsClosed(t *testing.T) {
	// Env var not set → token empty → every request must be rejected.
	handler := Auth(AuthOptions{Mode: "bearer", BearerTokenEnv: "UNDEFINED_BEARER_VAR"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token env must fail closed; got %d", rec.Code)
	}
}

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

func TestAuthStateAccessorsAndUpdates(t *testing.T) {
	t.Setenv("TEST_AUTH_STATE_BEARER", "bearer-secret")
	t.Setenv("TEST_AUTH_STATE_EDGE", "edge-secret")
	state := NewAuthState("bearer", "TEST_AUTH_STATE_BEARER", "TEST_AUTH_STATE_EDGE", "admin", "hash")

	if got := state.Mode(); got != "bearer" {
		t.Fatalf("mode = %q", got)
	}
	if got := state.BearerTokenEnv(); got != "TEST_AUTH_STATE_BEARER" {
		t.Fatalf("bearer env = %q", got)
	}
	if got := state.BearerToken(); got != "bearer-secret" {
		t.Fatalf("bearer token = %q", got)
	}
	if got := state.EdgeSecret(); got != "edge-secret" {
		t.Fatalf("edge secret = %q", got)
	}
	if got := state.AdminUsername(); got != "admin" {
		t.Fatalf("admin username = %q", got)
	}
	if got := state.AdminPasswordHash(); got != "hash" {
		t.Fatalf("admin hash = %q", got)
	}

	state.Set("edge_hmac", " ", "TEST_AUTH_STATE_EDGE_2")
	state.SetAdmin("root", "")
	t.Setenv("TEST_AUTH_STATE_EDGE_2", "edge-secret-2")

	if got := state.Mode(); got != "edge_hmac" {
		t.Fatalf("updated mode = %q", got)
	}
	if got := state.BearerTokenEnv(); got != "TEST_AUTH_STATE_BEARER" {
		t.Fatalf("blank bearer env update should be ignored, got %q", got)
	}
	if got := state.EdgeSecret(); got != "edge-secret-2" {
		t.Fatalf("updated edge secret = %q", got)
	}
	if got := state.AdminUsername(); got != "root" {
		t.Fatalf("updated admin username = %q", got)
	}
	if got := state.AdminPasswordHash(); got != "hash" {
		t.Fatalf("blank admin hash update should be ignored, got %q", got)
	}
}

func TestNilAuthStateAccessorsFailClosed(t *testing.T) {
	var state *AuthState
	if state.Mode() != "" || state.BearerTokenEnv() != "" || state.EdgeSecret() != "" || state.AdminUsername() != "" || state.AdminPasswordHash() != "" {
		t.Fatal("nil AuthState accessors should return empty values")
	}
	state.Set("bearer", "TOKEN", "EDGE")
	state.SetAdmin("admin", "hash")
}

func TestChain_OrderOutermostFirst(t *testing.T) {
	var order []string
	mw := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name+":before")
				next.ServeHTTP(w, r)
				order = append(order, name+":after")
			})
		}
	}
	handler := Chain(mw("a"), mw("b"), mw("c"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	want := []string{"a:before", "b:before", "c:before", "handler", "c:after", "b:after", "a:after"}
	if len(order) != len(want) {
		t.Fatalf("unexpected length: got %v want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order[%d] = %q want %q", i, order[i], want[i])
		}
	}
}

func TestAuth_SmokeTokenIsLowTrustFallback(t *testing.T) {
	// Bearer mode is on, smoke token is configured. A request with the
	// SMOKE token (and no bearer) must succeed but get a smoke identity
	// (Source=smoke, Plan=demo) — never an admin / bearer identity.
	t.Setenv("TEST_SMOKE_TOKEN", "sk-smoke-public-demo-001")
	var gotSource, gotPlan, gotUser, gotRole string
	handler := Auth(AuthOptions{
		Mode:               "bearer",
		BearerTokenEnv:     "UNDEFINED_BEARER_VAR",
		BearerRole:         "admin",
		SmokeTokenProvider: func() string { return "sk-smoke-public-demo-001" },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := IdentityFromContext(r.Context())
		gotSource = id.Source
		gotPlan = id.Plan
		gotUser = id.UserID
		gotRole = id.Role
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/dictation/transcribe", nil)
	req.Header.Set("Authorization", "Bearer sk-smoke-public-demo-001")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("smoke token must be accepted; got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotSource != "smoke" {
		t.Fatalf("identity source = %q, want smoke", gotSource)
	}
	if gotPlan != "demo" {
		t.Fatalf("identity plan = %q, want demo", gotPlan)
	}
	if gotUser != "smoke" {
		t.Fatalf("identity user = %q, want smoke", gotUser)
	}
	if gotRole != "" {
		t.Fatalf("smoke identity must never inherit BearerRole; got %q", gotRole)
	}
}

func TestAuth_SmokeTokenDisabledWhenNotConfigured(t *testing.T) {
	// No SmokeTokenProvider → smoke fallback is off → unauth request
	// (no bearer, no smoke) still 401.
	handler := Auth(AuthOptions{
		Mode:           "bearer",
		BearerTokenEnv: "UNDEFINED_BEARER_VAR",
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	req := httptest.NewRequest(http.MethodGet, "/v1/dictation/transcribe", nil)
	req.Header.Set("Authorization", "Bearer some-random-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unset smoke must not auth a stray bearer; got %d", rec.Code)
	}
}

func TestAuth_SmokeTokenRejectsMismatch(t *testing.T) {
	// Smoke token configured, but client sends a different value → 401.
	handler := Auth(AuthOptions{
		Mode:               "bearer",
		BearerTokenEnv:     "UNDEFINED_BEARER_VAR",
		SmokeTokenProvider: func() string { return "sk-smoke-public-demo-001" },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	req := httptest.NewRequest(http.MethodGet, "/v1/dictation/transcribe", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong smoke token must fail; got %d", rec.Code)
	}
}

func TestAuthState_SmokeTokenResolvesFromEnv(t *testing.T) {
	t.Setenv("TEST_SMOKE_ENV", "sk-smoke-from-env")
	state := NewAuthState("bearer", "TEST_BEARER", "TEST_EDGE", "", "")
	if got := state.SmokeToken(); got != "" {
		t.Fatalf("smoke token must be empty until SetSmokeTokenEnv is called; got %q", got)
	}
	state.SetSmokeTokenEnv("TEST_SMOKE_ENV")
	if got := state.SmokeToken(); got != "sk-smoke-from-env" {
		t.Fatalf("smoke token = %q, want sk-smoke-from-env", got)
	}
}
