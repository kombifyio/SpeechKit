//go:build linux

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
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

func TestAuth_SmokeTokenIsLowTrustFallback(t *testing.T) {
	// Bearer mode is on, smoke token is configured. A request with the
	// SMOKE token (and no bearer) must succeed but get a smoke identity
	// (Source=smoke, Plan=demo) — never an admin / bearer identity.
	t.Setenv("TEST_SMOKE_TOKEN", "smoke-token-public-demo-001")
	var gotSource, gotPlan, gotUser, gotRole string
	handler := Auth(AuthOptions{
		Mode:               "bearer",
		BearerTokenEnv:     "UNDEFINED_BEARER_VAR",
		BearerRole:         "admin",
		SmokeTokenProvider: func() string { return "smoke-token-public-demo-001" },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := IdentityFromContext(r.Context())
		gotSource = id.Source
		gotPlan = id.Plan
		gotUser = id.UserID
		gotRole = id.Role
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/dictation/transcribe", nil)
	req.Header.Set("Authorization", "Bearer smoke-token-public-demo-001")
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
		SmokeTokenProvider: func() string { return "smoke-token-public-demo-001" },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	req := httptest.NewRequest(http.MethodGet, "/v1/dictation/transcribe", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong smoke token must fail; got %d", rec.Code)
	}
}

func TestAuth_BearerOrOIDCAcceptsStaticBearer(t *testing.T) {
	t.Setenv("TEST_BEARER", "svc-token")
	handler := Auth(AuthOptions{Mode: "bearer_or_oidc", BearerTokenEnv: "TEST_BEARER"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := IdentityFromContext(r.Context())
			if id.Source != "bearer" {
				t.Fatalf("identity source should be bearer, got %q", id.Source)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	req.Header.Set("Authorization", "Bearer svc-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 via static bearer, got %d", rec.Code)
	}
}

func TestAuth_BearerOrOIDCFallsBackToOIDCVerifier(t *testing.T) {
	t.Setenv("TEST_BEARER", "svc-token")
	verifierCalled := false
	handler := Auth(AuthOptions{
		Mode:           "bearer_or_oidc",
		BearerTokenEnv: "TEST_BEARER",
		OIDCVerifier: func(r *http.Request) (Identity, bool) {
			verifierCalled = true
			if r.Header.Get("Authorization") == "Bearer valid-jwt" {
				return Identity{UserID: "user-42", OrgID: "org-1", Source: "oidc"}, true
			}
			return Identity{}, false
		},
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := IdentityFromContext(r.Context())
			if id.UserID != "user-42" || id.Source != "oidc" {
				t.Fatalf("unexpected identity %+v", id)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	req.Header.Set("Authorization", "Bearer valid-jwt")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 via OIDC verifier, got %d", rec.Code)
	}
	if !verifierCalled {
		t.Fatal("OIDC verifier should have been consulted for a non-service bearer")
	}
}

func TestAuth_BearerOrOIDCRejectsWithoutEitherCredential(t *testing.T) {
	t.Setenv("TEST_BEARER", "svc-token")
	handler := Auth(AuthOptions{
		Mode:           "bearer_or_oidc",
		BearerTokenEnv: "TEST_BEARER",
		OIDCVerifier: func(*http.Request) (Identity, bool) {
			return Identity{}, false
		},
	})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/any", nil)
	req.Header.Set("Authorization", "Bearer neither-valid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when neither credential matches, got %d", rec.Code)
	}
}
