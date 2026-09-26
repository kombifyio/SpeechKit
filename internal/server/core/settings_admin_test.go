//go:build linux

package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

func TestRegisterServerSettings_PatchCreatesAdminSessionLogin(t *testing.T) {
	t.Setenv(config.ServerSettingsWriteEnv, "true")
	t.Setenv(config.ServerSettingsPathEnv, filepath.Join(t.TempDir(), "server-settings.json"))
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "")

	app := &App{
		Cfg:       &config.Config{},
		Mux:       http.NewServeMux(),
		Health:    NewHealthRegistry(),
		Version:   "test-version",
		AuthState: middleware.NewAuthState("bearer", "SPEECHKIT_SERVER_TOKEN", "", "", ""),
	}
	app.Cfg.Server.AuthMode = "bearer"
	app.Cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"

	registerServerSettings(app)

	payload := []byte(`{
		"onboarding_complete": true,
		"admin_auth": {
			"username": "owner",
			"password": "correct-password"
		},
		"server_auth": {
			"mode": "managed_bearer",
			"bearer_token_env": "SPEECHKIT_SERVER_TOKEN",
			"generate_token": true
		}
	}`)
	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/v1/server/settings", bytes.NewReader(payload)))

	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap PATCH = %d body=%s", rec.Code, rec.Body.String())
	}
	stored, ok, err := config.LoadServerModelSettings(config.ServerSettingsPath(app.Cfg))
	if err != nil {
		t.Fatalf("LoadServerModelSettings: %v", err)
	}
	if !ok {
		t.Fatal("expected saved settings")
	}
	if stored.AdminAuth.Username != "owner" || stored.AdminAuth.PasswordHash == "" || stored.AdminAuth.PasswordValue != "" {
		t.Fatalf("stored admin auth = %+v", stored.AdminAuth)
	}

	var adminCookie, csrfCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		switch cookie.Name {
		case middleware.AdminSessionCookieName:
			adminCookie = cookie
		case middleware.AdminCSRFCookieName:
			csrfCookie = cookie
		}
	}
	if adminCookie == nil {
		t.Fatal("bootstrap PATCH should set an admin session cookie")
	}
	if csrfCookie == nil {
		t.Fatal("bootstrap PATCH should also set a CSRF cookie (audit S-13)")
	}
	adminReq := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", strings.NewReader(`{"onboarding_complete":true}`))
	adminReq.AddCookie(adminCookie)
	adminReq.AddCookie(csrfCookie)
	adminReq.Header.Set(middleware.AdminCSRFHeaderName, csrfCookie.Value)
	adminRec := httptest.NewRecorder()
	middleware.Auth(middleware.AuthOptions{
		ModeProvider:              app.AuthState.Mode,
		BearerTokenProvider:       app.AuthState.BearerToken,
		AdminUsernameProvider:     app.AuthState.AdminUsername,
		AdminPasswordHashProvider: app.AuthState.AdminPasswordHash,
	})(app.Mux).ServeHTTP(adminRec, adminReq)

	if adminRec.Code != http.StatusOK {
		t.Fatalf("admin session PATCH after bootstrap = %d body=%s", adminRec.Code, adminRec.Body.String())
	}

	// Audit S-13: same admin-session cookie WITHOUT the X-CSRF-Token
	// header must be refused with 403 csrf_required.
	noCSRFReq := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", strings.NewReader(`{"onboarding_complete":true}`))
	noCSRFReq.AddCookie(adminCookie)
	noCSRFReq.AddCookie(csrfCookie)
	noCSRFRec := httptest.NewRecorder()
	middleware.Auth(middleware.AuthOptions{
		ModeProvider:              app.AuthState.Mode,
		BearerTokenProvider:       app.AuthState.BearerToken,
		AdminUsernameProvider:     app.AuthState.AdminUsername,
		AdminPasswordHashProvider: app.AuthState.AdminPasswordHash,
	})(app.Mux).ServeHTTP(noCSRFRec, noCSRFReq)
	if noCSRFRec.Code != http.StatusForbidden {
		t.Fatalf("admin session PATCH without X-CSRF-Token header = %d, want 403", noCSRFRec.Code)
	}
	if !strings.Contains(noCSRFRec.Body.String(), "csrf_required") {
		t.Fatalf("body without CSRF = %q, want csrf_required envelope", noCSRFRec.Body.String())
	}

	// Bearer-token callers bypass CSRF via EnforceAdminCSRF's Source !=
	// "admin_session" branch — verified directly in
	// middleware.TestEnforceAdminCSRF_BypassesNonAdminSessionIdentity
	// (source=bearer, edge_hmac, smoke, none, ""). No need to set up
	// the post-bootstrap bearer environment here just to repeat that
	// coverage end-to-end.
}

func TestRegisterServerSettings_PatchRequiresAdminAfterBootstrap(t *testing.T) {
	t.Setenv(config.ServerSettingsWriteEnv, "true")
	t.Setenv(config.ServerSettingsPathEnv, filepath.Join(t.TempDir(), "server-settings.json"))
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "test-token")

	app := &App{
		Cfg:     &config.Config{},
		Mux:     http.NewServeMux(),
		Health:  NewHealthRegistry(),
		Version: "test-version",
	}
	app.Cfg.Server.AuthMode = "bearer"
	app.Cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"
	registerServerSettings(app)

	payload := []byte(`{"onboarding_complete":true}`)
	nonAdmin := serveServerSettingsWithBearerRole(app, httptest.NewRequest(http.MethodPatch, "/v1/server/settings", bytes.NewReader(payload)), "")
	if nonAdmin.Code != http.StatusForbidden {
		t.Fatalf("non-admin PATCH after bootstrap should return 403, got %d body=%s", nonAdmin.Code, nonAdmin.Body.String())
	}
	if !strings.Contains(nonAdmin.Body.String(), "admin_required") {
		t.Fatalf("non-admin PATCH should explain admin_required, got %s", nonAdmin.Body.String())
	}

	admin := serveServerSettingsWithBearerRole(app, httptest.NewRequest(http.MethodPatch, "/v1/server/settings", bytes.NewReader(payload)), "admin")
	if admin.Code != http.StatusOK {
		t.Fatalf("admin PATCH after bootstrap should return 200, got %d body=%s", admin.Code, admin.Body.String())
	}
}

func TestRegisterServerSettings_FirstRunAdminCreateAllowedWithExistingToken(t *testing.T) {
	t.Setenv(config.ServerSettingsWriteEnv, "true")
	t.Setenv(config.ServerSettingsPathEnv, filepath.Join(t.TempDir(), "server-settings.json"))
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "existing-token")

	app := &App{
		Cfg:       &config.Config{},
		Mux:       http.NewServeMux(),
		Health:    NewHealthRegistry(),
		Version:   "test-version",
		AuthState: middleware.NewAuthState("bearer", "SPEECHKIT_SERVER_TOKEN", "", "", ""),
	}
	app.Cfg.Server.AuthMode = "bearer"
	app.Cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"
	registerServerSettings(app)

	payload := []byte(`{
		"onboarding_complete": true,
		"admin_auth": {
			"username": "first-admin",
			"password": "correct-password"
		}
	}`)
	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/v1/server/settings", bytes.NewReader(payload)))
	if rec.Code != http.StatusOK {
		t.Fatalf("first-run admin create should be allowed with existing token, got %d body=%s", rec.Code, rec.Body.String())
	}

	stored, ok, err := config.LoadServerModelSettings(config.ServerSettingsPath(app.Cfg))
	if err != nil || !ok {
		t.Fatalf("LoadServerModelSettings ok=%v err=%v", ok, err)
	}
	if stored.AdminAuth.Username != "first-admin" || stored.AdminAuth.PasswordHash == "" {
		t.Fatalf("stored admin auth = %+v", stored.AdminAuth)
	}

	second := httptest.NewRecorder()
	app.Mux.ServeHTTP(second, httptest.NewRequest(http.MethodPatch, "/v1/server/settings", bytes.NewReader(payload)))
	if second.Code != http.StatusForbidden {
		t.Fatalf("second anonymous admin create should be blocked, got %d body=%s", second.Code, second.Body.String())
	}
}

func TestRegisterServerSettings_AdminAuthDisabledDoesNotKeepBootstrapWriteOpen(t *testing.T) {
	t.Setenv(config.ServerSettingsWriteEnv, "true")
	t.Setenv(config.ServerSettingsPathEnv, filepath.Join(t.TempDir(), "server-settings.json"))
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "existing-token")

	app := &App{
		Cfg:       &config.Config{},
		Mux:       http.NewServeMux(),
		Health:    NewHealthRegistry(),
		Version:   "test-version",
		AuthState: middleware.NewAuthState("bearer", "SPEECHKIT_SERVER_TOKEN", "", "", ""),
	}
	app.Cfg.Server.AuthMode = "bearer"
	app.Cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"
	registerServerSettings(app)

	firstPayload := []byte(`{
		"onboarding_complete": true,
		"admin_auth": {
			"enabled": false
		}
	}`)
	first := httptest.NewRecorder()
	app.Mux.ServeHTTP(first, httptest.NewRequest(http.MethodPatch, "/v1/server/settings", bytes.NewReader(firstPayload)))
	if first.Code != http.StatusOK {
		t.Fatalf("first-run settings write should be allowed, got %d body=%s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	app.Mux.ServeHTTP(second, httptest.NewRequest(http.MethodPatch, "/v1/server/settings", bytes.NewReader([]byte(`{"onboarding_complete":true}`))))
	if second.Code != http.StatusForbidden {
		t.Fatalf("anonymous PATCH after completed setup should be blocked, got %d body=%s", second.Code, second.Body.String())
	}

	stored, ok, err := config.LoadServerModelSettings(config.ServerSettingsPath(app.Cfg))
	if err != nil || !ok {
		t.Fatalf("LoadServerModelSettings ok=%v err=%v", ok, err)
	}
	if stored.AdminAuth.Enabled == nil || *stored.AdminAuth.Enabled {
		t.Fatalf("stored admin auth should be explicitly disabled, got %+v", stored.AdminAuth)
	}
	if app.Cfg.Server.AdminAuthEnabled {
		t.Fatal("runtime admin auth should be disabled")
	}
}

func TestRegisterServerSettings_RequiresOnboardingAfterDeployVersionChanges(t *testing.T) {
	t.Setenv(config.ServerOnboardingUIEnv, "true")
	settingsPath := filepath.Join(t.TempDir(), "server-settings.json")
	t.Setenv(config.ServerSettingsPathEnv, settingsPath)

	if err := config.SaveServerModelSettings(settingsPath, config.ServerModelSettings{
		OnboardingComplete: true,
		OnboardingVersion:  "old-version",
		Modes: config.ServerModeProviderSettings{
			Assist: config.ServerModeSetting{
				ProviderKind: "local_built_in",
				ProfileID:    "assist.builtin.gemma4-e4b",
				Model:        "ggml-org/gemma-4-E2B-it-GGUF:Q8_0",
			},
		},
	}); err != nil {
		t.Fatalf("SaveServerModelSettings: %v", err)
	}

	app := &App{
		Cfg:     &config.Config{},
		Mux:     http.NewServeMux(),
		Health:  NewHealthRegistry(),
		Version: "new-version",
	}
	registerServerSettings(app)

	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/server/settings", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/server/settings = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Onboarding struct {
			Complete             bool   `json:"complete"`
			Required             bool   `json:"required"`
			CurrentDeployVersion string `json:"current_deploy_version"`
			CompletedVersion     string `json:"completed_version"`
		} `json:"onboarding"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("settings JSON: %v", err)
	}
	if body.Onboarding.Complete {
		t.Fatal("onboarding should be incomplete after deploy version changes")
	}
	if !body.Onboarding.Required {
		t.Fatal("onboarding should be required after deploy version changes")
	}
	if body.Onboarding.CurrentDeployVersion != "new-version" || body.Onboarding.CompletedVersion != "old-version" {
		t.Fatalf("deploy versions = current %q completed %q", body.Onboarding.CurrentDeployVersion, body.Onboarding.CompletedVersion)
	}
}
