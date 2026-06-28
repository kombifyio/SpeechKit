//go:build linux

package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

func TestServerMiddlewareChain_CORSPreflightBypassesBearerAuth(t *testing.T) {
	const origin = "https://workbench.example.test"
	called := false
	handler := newAdapterTestHandler(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/v1/private", func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})
	}, origin)

	req := httptest.NewRequest(http.MethodOptions, "/v1/private", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204 body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("preflight must terminate before auth and private handler")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, origin)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want Origin", got)
	}
}

func TestServerMiddlewareChain_PrivateRoutesRequireBearerAndAttachIdentity(t *testing.T) {
	const origin = "https://workbench.example.test"
	var identity middleware.Identity
	handler := newAdapterTestHandler(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/v1/private", func(w http.ResponseWriter, r *http.Request) {
			identity = middleware.IdentityFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})
	}, origin)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/private", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("missing bearer status = %d, want 401 body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/private", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Authorization", "Bearer adapter-test-token")
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, req)

	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, want 200 body=%s", authorized.Code, authorized.Body.String())
	}
	if identity.Source != "bearer" {
		t.Fatalf("identity source = %q, want bearer", identity.Source)
	}
	if got := authorized.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("authorized CORS origin = %q, want %q", got, origin)
	}
}

func TestServerMiddlewareChain_HealthzStaysPublicBehindBearerAuth(t *testing.T) {
	handler := newAdapterTestHandler(t, nil, "https://workbench.example.test")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
}

func newAdapterTestHandler(t *testing.T, mount func(*http.ServeMux), allowedOrigins ...string) http.Handler {
	t.Helper()
	t.Setenv("SPEECHKIT_ADAPTER_TEST_TOKEN", "adapter-test-token")
	cfg, err := config.Load(t.TempDir() + "/missing-config.toml")
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	cfg.Server.ListenAddr = "127.0.0.1:0"
	cfg.Server.AuthMode = string(middleware.AuthModeBearer)
	cfg.Server.BearerTokenEnv = "SPEECHKIT_ADAPTER_TEST_TOKEN"
	cfg.Server.CORSAllowedOrigins = allowedOrigins
	cfg.Server.RateLimitRPS = 100
	cfg.Server.RateLimitBurst = 100
	cfg.Server.Security.Disabled = true

	app := newServerApp(cfg, RunOptions{Version: "adapter-test"})
	registerCoreEndpoints(app)
	if mount != nil {
		mount(app.Mux)
	}
	chain, err := serverMiddlewareChain(context.Background(), cfg, app)
	if err != nil {
		t.Fatalf("server middleware chain: %v", err)
	}
	return chain(app.Mux)
}
