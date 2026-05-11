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

func TestAuth_BasicAdminAcceptsConfiguredPasswordHash(t *testing.T) {
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
			if id.UserID != "admin" || id.Role != "admin" || id.Source != "basic" {
				t.Fatalf("identity = %+v, want admin basic identity", id)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:correct-password")))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("basic admin should authenticate, got %d body=%s", rec.Code, rec.Body.String())
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
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("expected HTML content type, got %q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "SpeechKit Admin Sign-In Required") {
		t.Fatalf("expected browser auth page, got %s", body)
	}
	if strings.Contains(body, "Kombify Cloud SSO") {
		t.Fatalf("browser auth page should not mention Kombify Cloud SSO: %s", body)
	}
	for _, want := range []string{"Admin username", "Admin password"} {
		if !strings.Contains(body, want) {
			t.Fatalf("browser auth page should contain %q, got %s", want, body)
		}
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatal("expected WWW-Authenticate challenge")
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
