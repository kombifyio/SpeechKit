//go:build linux

package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

func TestRegisterTestUI_ServesModeTester(t *testing.T) {
	app := &App{
		Mux:     http.NewServeMux(),
		Version: "test-version",
		Modes: map[Mode]bool{
			ModeDictation:  true,
			ModeAssist:     true,
			ModeVoiceAgent: true,
		},
	}

	registerTestUI(app)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	app.Mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("expected text/html content type, got %q", got)
	}

	if body := rec.Body.String(); !strings.Contains(body, "SpeechKit Server Smoke") {
		t.Fatalf("test UI body should contain smoke title")
	}
}

func TestRegisterTestUI_ServesSmokeWithInlineAssetCSPHashes(t *testing.T) {
	app := &App{Mux: http.NewServeMux()}
	registerTestUI(app)

	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("smoke UI must set a CSP for its inline assets")
	}
	for _, want := range []string{"style-src 'sha256-", "script-src 'sha256-", "connect-src 'self' ws: wss:"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("smoke UI CSP missing %q: %s", want, csp)
		}
	}
	if strings.Contains(csp, "'unsafe-inline'") {
		t.Fatalf("smoke UI CSP must use hashes instead of unsafe-inline: %s", csp)
	}
}

func TestRegisterTestUI_ServesSetupOnly(t *testing.T) {
	app := &App{Mux: http.NewServeMux()}
	registerTestUI(app)

	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /setup should return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("GET /setup should return text/html content type, got %q", got)
	}

	if body := rec.Body.String(); !strings.Contains(body, "SpeechKit Server Setup") {
		t.Fatalf("setup UI body should contain setup title")
	}
}

func TestRegisterTestUI_RejectsExtraUIPaths(t *testing.T) {
	app := &App{Mux: http.NewServeMux()}
	registerTestUI(app)

	for _, path := range []string{"/test-ui", "/test-ui/", "/admin", "/admin/"} {
		rec := httptest.NewRecorder()
		app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s should return 404, got %d", path, rec.Code)
		}
	}
}

func TestRegisterTestUI_AllowsHEADAndRejectsOtherMethods(t *testing.T) {
	app := &App{Mux: http.NewServeMux()}
	registerTestUI(app)

	head := httptest.NewRecorder()
	app.Mux.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/", nil))
	if head.Code != http.StatusOK {
		t.Fatalf("HEAD / should return 200, got %d", head.Code)
	}

	setupHead := httptest.NewRecorder()
	app.Mux.ServeHTTP(setupHead, httptest.NewRequest(http.MethodHead, "/setup", nil))
	if setupHead.Code != http.StatusOK {
		t.Fatalf("HEAD /setup should return 200, got %d", setupHead.Code)
	}

	post := httptest.NewRecorder()
	app.Mux.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/setup", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /setup should return 405, got %d", post.Code)
	}
	if got := post.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("unexpected Allow header %q", got)
	}
}

func TestServerPublicPaths_IncludeOnlyAlwaysPublicUIPaths(t *testing.T) {
	paths := serverPublicPaths()
	for _, want := range []string{"/", "/healthz", "/readyz", "/readyz/strict"} {
		found := false
		for _, got := range paths {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("serverPublicPaths should include %q, got %#v", want, paths)
		}
	}
	for _, forbidden := range []string{"/setup", "/setup/", "/test-ui", "/test-ui/", "/admin", "/admin/", "/v1/server/settings", "/api/v1/server/settings"} {
		for _, got := range paths {
			if got == forbidden {
				t.Fatalf("serverPublicPaths should not include extra UI path %q, got %#v", forbidden, paths)
			}
		}
	}
}

func TestServerBootstrapPaths_ExposeSetupOnlyDuringBootstrap(t *testing.T) {
	paths := serverBootstrapPaths()
	for _, want := range []string{"/setup", "/setup/"} {
		found := false
		for _, got := range paths {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("serverBootstrapPaths should include %q, got %#v", want, paths)
		}
	}
	for _, forbidden := range []string{"/", "/healthz", "/readyz", "/v1/server/settings", "/api/v1/server/settings"} {
		for _, got := range paths {
			if got == forbidden {
				t.Fatalf("serverBootstrapPaths should not include %q, got %#v", forbidden, paths)
			}
		}
	}
}

func TestServerTicketUpgradeIsPublicButSessionMintRequiresAuthentication(t *testing.T) {
	protected := middleware.Auth(middleware.AuthOptions{
		Mode:                "bearer",
		BearerTokenProvider: func() string { return "service-token" },
		AllowPublicPaths:    serverPublicPaths(),
		AllowPublicRoutes:   serverPublicRoutes(),
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	upgrade := httptest.NewRecorder()
	protected.ServeHTTP(upgrade, httptest.NewRequest(http.MethodGet, "/v1/voiceagent/sessions/ticket-id/ws", nil))
	if upgrade.Code != http.StatusNoContent {
		t.Fatalf("ticketed websocket upgrade = %d, want public handoff", upgrade.Code)
	}

	mint := httptest.NewRecorder()
	protected.ServeHTTP(mint, httptest.NewRequest(http.MethodPost, "/v1/voiceagent/sessions", nil))
	if mint.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous session mint = %d, want authentication challenge", mint.Code)
	}
}

func TestHostedOperatorSurfaceRequiresAuthentication(t *testing.T) {
	t.Setenv(config.ServerOperatorUIPublicEnv, "false")
	t.Setenv(config.ServerDetailedReadinessPublicEnv, "false")

	protected := middleware.Auth(middleware.AuthOptions{
		Mode:                 "bearer",
		BearerTokenProvider:  func() string { return "operator-token" },
		AllowPublicPaths:     serverPublicPaths(),
		AllowPublicRoutes:    serverPublicRoutes(),
		AllowBootstrapPaths:  serverBootstrapPaths(),
		AllowBootstrapRoutes: serverBootstrapAuthRoutes(),
		BootstrapAllowed: func(*http.Request) bool {
			return true
		},
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, path := range []string{"/", "/assistant", "/setup", "/readyz", "/readyz/strict", "/v1/server/settings"} {
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous GET %s = %d, want authentication challenge", path, rec.Code)
		}

		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer operator-token")
		authorized := httptest.NewRecorder()
		protected.ServeHTTP(authorized, req)
		if authorized.Code != http.StatusNoContent {
			t.Fatalf("authenticated GET %s = %d, want access", path, authorized.Code)
		}
	}

	settingsWrite := httptest.NewRecorder()
	protected.ServeHTTP(settingsWrite, httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil))
	if settingsWrite.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous bootstrap settings write = %d, want authentication challenge", settingsWrite.Code)
	}

	health := httptest.NewRecorder()
	protected.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusNoContent {
		t.Fatalf("anonymous GET /healthz = %d, want public liveness", health.Code)
	}
}

func TestServerBootstrapAuthRoutes_ExposeSettingsDuringBootstrap(t *testing.T) {
	routes := serverBootstrapAuthRoutes()
	wants := map[string]map[string]bool{
		"/v1/server/settings": {
			http.MethodGet:   false,
			http.MethodHead:  false,
			http.MethodPatch: false,
		},
		"/api/v1/server/settings": {
			http.MethodGet:   false,
			http.MethodHead:  false,
			http.MethodPatch: false,
		},
	}

	for _, route := range routes {
		methods, ok := wants[route.Path]
		if !ok {
			t.Fatalf("unexpected public route %q", route.Path)
		}
		for _, method := range route.Methods {
			if _, ok := methods[method]; !ok {
				t.Fatalf("unexpected method %q for public route %q", method, route.Path)
			}
			methods[method] = true
		}
	}
	for path, methods := range wants {
		for method := range methods {
			if !methods[method] {
				t.Fatalf("%s should be bootstrap-auth route for %s", method, path)
			}
		}
	}
}

func TestRegisterTestUI_NoOpWhenOnboardingDisabledByEnv(t *testing.T) {
	t.Setenv(config.ServerOnboardingUIEnv, "false")

	app := &App{Mux: http.NewServeMux()}
	registerTestUI(app)

	for _, path := range []string{"/", "/setup", "/setup/"} {
		rec := httptest.NewRecorder()
		app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("with %s=false, GET %s should be 404 (handlers not registered), got %d", config.ServerOnboardingUIEnv, path, rec.Code)
		}
	}
}

func TestSetupHandler_RequiresAdminIdentityWhenAppSealed(t *testing.T) {
	app := &App{
		Cfg:       &config.Config{},
		Mux:       http.NewServeMux(),
		AuthState: middleware.NewAuthState("bearer", "SPEECHKIT_SERVER_TOKEN", "", "owner", "$2a$04$3ZQhRz6fJb3kQGN9cE1uD.RZ8c3E9oB3z4ED5CzSMYRhhAv7n4EHa"),
	}
	app.Cfg.Server.AdminAuthEnabled = true
	app.Cfg.Server.AdminUsername = "owner"
	app.Cfg.Server.AdminPasswordHash = "$2a$04$3ZQhRz6fJb3kQGN9cE1uD.RZ8c3E9oB3z4ED5CzSMYRhhAv7n4EHa"
	registerTestUI(app)
	app.bootstrapSealed.Store(true)

	for _, path := range []string{"/setup", "/setup/"} {
		rec := httptest.NewRecorder()
		app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("sealed app should require admin identity for GET %s, got %d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	adminHandler := middleware.Auth(middleware.AuthOptions{
		Mode:                "bearer",
		BearerTokenProvider: func() string { return "admin-token" },
		BearerRole:          "admin",
	})(app.Mux)
	adminReq := httptest.NewRequest(http.MethodGet, "/setup", nil)
	adminReq.Header.Set("Authorization", "Bearer admin-token")
	adminRec := httptest.NewRecorder()
	adminHandler.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("sealed app should serve setup to admin identity, got %d body=%s", adminRec.Code, adminRec.Body.String())
	}
	if body := adminRec.Body.String(); !strings.Contains(body, "SpeechKit Server Setup") {
		t.Fatalf("admin setup UI body should contain setup title, got %s", body)
	}

	// Smoke (`/`) is harmless and remains reachable so operators can
	// still view runtime status after onboarding completes.
	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("sealed app should still serve smoke at /, got %d", rec.Code)
	}
}

func TestSetupHandler_AllowsFirstRunWhenNoAdminExists(t *testing.T) {
	app := &App{
		Cfg:       &config.Config{},
		Mux:       http.NewServeMux(),
		AuthState: middleware.NewAuthState("bearer", "SPEECHKIT_SERVER_TOKEN", "", "", ""),
	}
	registerTestUI(app)
	app.bootstrapSealed.Store(true)

	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("first-run setup without admin should be reachable, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSetupHandler_AllowsSealedSetupWhenAdminAuthDisabled(t *testing.T) {
	app := &App{
		Cfg:       &config.Config{},
		Mux:       http.NewServeMux(),
		AuthState: middleware.NewAuthState("bearer", "SPEECHKIT_SERVER_TOKEN", "", "owner", "$2a$04$3ZQhRz6fJb3kQGN9cE1uD.RZ8c3E9oB3z4ED5CzSMYRhhAv7n4EHa"),
	}
	app.Cfg.Server.AdminAuthEnabled = false
	app.Cfg.Server.AdminUsername = "owner"
	app.Cfg.Server.AdminPasswordHash = "$2a$04$3ZQhRz6fJb3kQGN9cE1uD.RZ8c3E9oB3z4ED5CzSMYRhhAv7n4EHa"
	registerTestUI(app)
	app.bootstrapSealed.Store(true)

	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin-auth-disabled setup should be reachable in handler, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRegisterAPIAlias_RewritesAPIV1ToLegacyV1(t *testing.T) {
	mux := http.NewServeMux()
	registerAPIAlias(mux)

	var gotPath string
	var gotPrefix string
	mux.HandleFunc("/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotPrefix = r.Header.Get("X-SpeechKit-API-Prefix")
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected alias to route to /v1 handler, got %d", rec.Code)
	}
	if gotPath != "/v1/ping" {
		t.Fatalf("alias target path = %q, want /v1/ping", gotPath)
	}
	if gotPrefix != "/api" {
		t.Fatalf("api prefix header = %q, want /api", gotPrefix)
	}
}
