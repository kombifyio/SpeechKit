//go:build linux

package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

func serveServerSettingsWithBearer(app *App, req *http.Request) *httptest.ResponseRecorder {
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	middleware.Auth(middleware.AuthOptions{
		Mode:                "bearer",
		BearerTokenProvider: func() string { return "test-token" },
	})(app.Mux).ServeHTTP(rec, req)
	return rec
}

func serveServerSettingsWithBearerRole(app *App, req *http.Request, role string) *httptest.ResponseRecorder {
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	middleware.Auth(middleware.AuthOptions{
		Mode:                "bearer",
		BearerTokenProvider: func() string { return "test-token" },
		BearerRole:          role,
	})(app.Mux).ServeHTTP(rec, req)
	return rec
}

func TestActiveVoiceAgentModeSettingUsesDirectProviderProfiles(t *testing.T) {
	tests := []struct {
		provider  string
		model     string
		wantID    string
		wantModel string
	}{
		{provider: "google", wantID: "realtime.google.gemini-native-audio", wantModel: "gemini-3.1-flash-live-preview"},
		{provider: "deepgram", wantID: "realtime.deepgram.voice-agent", wantModel: "flux-general-multi"},
		{provider: "assemblyai", wantID: "realtime.assemblyai.voice-agent", wantModel: "assemblyai-voice-agent"},
		{provider: "openai", wantID: "realtime.openai.gpt-realtime-2", wantModel: "gpt-realtime-2"},
		{provider: "assemblyai", model: "custom-agent", wantID: "realtime.assemblyai.voice-agent", wantModel: "custom-agent"},
	}
	for _, tt := range tests {
		t.Run(tt.provider+"/"+tt.model, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.VoiceAgent.Provider = tt.provider
			cfg.VoiceAgent.Model = tt.model
			got := activeVoiceAgentModeSetting(cfg)
			if got.ProviderKind != "direct_provider" {
				t.Fatalf("provider kind = %q, want direct_provider", got.ProviderKind)
			}
			if got.ProfileID != tt.wantID {
				t.Fatalf("profile id = %q, want %q", got.ProfileID, tt.wantID)
			}
			if got.Model != tt.wantModel {
				t.Fatalf("model = %q, want %q", got.Model, tt.wantModel)
			}
		})
	}
}

func TestActiveServerCredentialSettingsIncludesVoiceAgentProviders(t *testing.T) {
	cfg := &config.Config{}
	cfg.Providers.Deepgram.Enabled = true
	cfg.Providers.Deepgram.APIKeyEnv = "DG_TEST_KEY"
	cfg.Providers.AssemblyAI.Enabled = true
	cfg.Providers.AssemblyAI.APIKeyEnv = "AAI_TEST_KEY"

	got := activeServerCredentialSettings(cfg)
	if got.Deepgram.Enabled == nil || !*got.Deepgram.Enabled || got.Deepgram.Env != "DG_TEST_KEY" {
		t.Fatalf("Deepgram credential = %#v", got.Deepgram)
	}
	if got.AssemblyAI.Enabled == nil || !*got.AssemblyAI.Enabled || got.AssemblyAI.Env != "AAI_TEST_KEY" {
		t.Fatalf("AssemblyAI credential = %#v", got.AssemblyAI)
	}
}

func TestRegisterServerSettings_ServesSafeSnapshot(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "secret-value")

	app := &App{
		Cfg:     &config.Config{},
		Mux:     http.NewServeMux(),
		Health:  NewHealthRegistry(),
		Version: "test-version",
		Modes: map[Mode]bool{
			ModeDictation: true,
		},
	}
	app.Cfg.Providers.OpenAI.Enabled = true
	app.Cfg.Providers.OpenAI.APIKeyEnv = "OPENAI_API_KEY"
	app.Cfg.LocalLLM.Enabled = true
	app.Cfg.LocalLLM.AssistModel = "speechkit-local-llm"

	registerServerSettings(app)

	rec := serveServerSettingsWithBearer(app, httptest.NewRequest(http.MethodGet, "/v1/server/settings", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/server/settings = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-value") {
		t.Fatal("settings response leaked a credential value")
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("settings JSON: %v", err)
	}
	if body["version"] != "test-version" {
		t.Fatalf("version = %v", body["version"])
	}
	if _, ok := body["catalog"].(map[string]any); !ok {
		t.Fatalf("settings response should include provider catalog, got %#v", body["catalog"])
	}
}

func TestRegisterServerSettings_ServesMinimalBootstrapSnapshotWithoutAuth(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "secret-value")

	app := &App{
		Cfg:     &config.Config{},
		Mux:     http.NewServeMux(),
		Health:  NewHealthRegistry(),
		Version: "test-version",
	}
	app.Cfg.Server.ModelDir = "/var/lib/speechkit/private-models"
	app.Cfg.Server.AuthMode = "bearer"
	app.Cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"
	app.Cfg.VPS.Enabled = true
	app.Cfg.VPS.URL = "http://internal-whisper:8080"
	app.Cfg.LocalLLM.Enabled = true
	app.Cfg.LocalLLM.BaseURL = "http://internal-llm:11434"
	app.Cfg.Providers.OpenAI.Enabled = true
	app.Cfg.Providers.OpenAI.APIKeyEnv = "OPENAI_API_KEY"

	registerServerSettings(app)

	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/server/settings", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/server/settings = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("settings JSON: %v", err)
	}
	for _, want := range []string{"version", "status", "onboarding", "catalog", "editable"} {
		if _, ok := body[want]; !ok {
			t.Fatalf("bootstrap settings response should include %q, got %#v", want, body)
		}
	}
	for _, forbidden := range []string{"modes", "components", "auth", "stt", "llm", "voice_agent", "tts", "personas"} {
		if _, ok := body[forbidden]; ok {
			t.Fatalf("bootstrap settings response should not include %q: %#v", forbidden, body[forbidden])
		}
	}
	runtime, ok := body["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("bootstrap settings response should include sanitized runtime, got %#v", body["runtime"])
	}
	if _, ok := runtime["settings_write"]; !ok {
		t.Fatalf("bootstrap runtime should include settings_write, got %#v", runtime)
	}
	for key := range runtime {
		if key != "settings_write" {
			t.Fatalf("bootstrap runtime should not include %q: %#v", key, runtime)
		}
	}
	for _, forbidden := range []string{
		"/var/lib/speechkit/private-models",
		"http://internal-whisper:8080",
		"http://internal-llm:11434",
		"OPENAI_API_KEY",
		"SPEECHKIT_SERVER_TOKEN",
		"secret-value",
	} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("bootstrap settings response leaked %q: %s", forbidden, rec.Body.String())
		}
	}
}

func TestRegisterServerSettings_PatchSavesProviderMatrixWithoutLeakingCredential(t *testing.T) {
	t.Setenv(config.ServerSettingsWriteEnv, "true")
	t.Setenv(config.ServerSettingsPathEnv, filepath.Join(t.TempDir(), "server-settings.json"))

	app := &App{
		Cfg:     &config.Config{},
		Mux:     http.NewServeMux(),
		Health:  NewHealthRegistry(),
		Version: "test-version",
		Modes: map[Mode]bool{
			ModeDictation:  true,
			ModeAssist:     true,
			ModeVoiceAgent: true,
		},
	}
	app.Cfg.Server.AuthMode = "bearer"
	app.Cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"

	registerServerSettings(app)

	payload := []byte(`{
		"onboarding_complete": true,
		"modes": {
			"dictation": {"provider_kind":"local_built_in","profile_id":"stt.local.whispercpp","model":"whisper-1"},
			"assist": {"provider_kind":"local_built_in","profile_id":"assist.builtin.gemma4-e4b","model":"ggml-org/gemma-4-E2B-it-GGUF:Q8_0"},
			"voice_agent": {"provider_kind":"local_built_in","profile_id":"realtime.builtin.pipeline","model":"ggml-org/gemma-4-E2B-it-GGUF:Q8_0"}
		},
		"credentials": {
			"google": {"enabled": true, "env": "GOOGLE_AI_API_KEY", "value": "google-secret"}
		},
		"dictation": {
			"dictionary": "kombi fire => Kombify\nAcmeOS"
		},
		"assist": {
			"enabled_tools": ["summarize", "quick_note"]
		},
		"voice_agent": {
			"prompt_template": "Be concise and ask one follow-up question when useful."
		}
	}`)
	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/v1/server/settings", bytes.NewReader(payload)))

	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /v1/server/settings = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "google-secret") {
		t.Fatal("PATCH response leaked raw credential")
	}

	stored, ok, err := config.LoadServerModelSettings(config.ServerSettingsPath(app.Cfg))
	if err != nil {
		t.Fatalf("LoadServerModelSettings: %v", err)
	}
	if !ok {
		t.Fatal("expected saved settings")
	}
	if !stored.OnboardingComplete {
		t.Fatal("onboarding should be marked complete")
	}
	if got := stored.OnboardingVersion; got != "test-version" {
		t.Fatalf("onboarding version = %q, want test-version", got)
	}
	if got := stored.Modes.Assist.ProfileID; got != "assist.builtin.gemma4-e4b" {
		t.Fatalf("assist profile = %q", got)
	}
	if got := stored.Credentials.Google.Value; got != "" {
		t.Fatalf("stored google credential should be write-only, got %q", got)
	}
	if stored.Dictation.Dictionary == nil || *stored.Dictation.Dictionary != "kombi fire => Kombify\nAcmeOS" {
		t.Fatalf("stored dictation dictionary = %#v", stored.Dictation.Dictionary)
	}
	if got := strings.Join(stored.Assist.EnabledTools, ","); got != "summarize,quick_note" {
		t.Fatalf("stored assist tools = %q", got)
	}
	if stored.VoiceAgent.PromptTemplate == nil || *stored.VoiceAgent.PromptTemplate != "Be concise and ask one follow-up question when useful." {
		t.Fatalf("stored voice prompt template = %#v", stored.VoiceAgent.PromptTemplate)
	}

	get := serveServerSettingsWithBearer(app, httptest.NewRequest(http.MethodGet, "/v1/server/settings", nil))
	if strings.Contains(get.Body.String(), "google-secret") {
		t.Fatal("GET response leaked raw credential")
	}
	for _, want := range []string{
		`"dictionary":"kombi fire =\u003e Kombify\nAcmeOS"`,
		`"enabled_tools":["summarize","quick_note"]`,
		`"prompt_template":"Be concise and ask one follow-up question when useful."`,
	} {
		if !strings.Contains(get.Body.String(), want) {
			t.Fatalf("GET response should contain %s, body=%s", want, get.Body.String())
		}
	}
}

func TestRegisterServerSettings_PatchGeneratesWriteOnlyServerToken(t *testing.T) {
	t.Setenv(config.ServerSettingsWriteEnv, "true")
	t.Setenv(config.ServerSettingsPathEnv, filepath.Join(t.TempDir(), "server-settings.json"))
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "")

	app := &App{
		Cfg:     &config.Config{},
		Mux:     http.NewServeMux(),
		Health:  NewHealthRegistry(),
		Version: "test-version",
	}
	app.Cfg.Server.AuthMode = "bearer"
	app.Cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"

	registerServerSettings(app)

	payload := []byte(`{
		"onboarding_complete": true,
		"server_auth": {
			"mode": "managed_bearer",
			"bearer_token_env": "SPEECHKIT_SERVER_TOKEN",
			"generate_token": true
		}
	}`)
	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/v1/server/settings", bytes.NewReader(payload)))

	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /v1/server/settings = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		GeneratedToken struct {
			Token    string `json:"token"`
			Env      string `json:"env"`
			AuthMode string `json:"auth_mode"`
		} `json:"generated_token"`
		Desired config.ServerModelSettings `json:"desired"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("settings response JSON: %v", err)
	}
	if body.GeneratedToken.Token == "" {
		t.Fatal("expected one-time generated token in response")
	}
	if body.GeneratedToken.Env != "SPEECHKIT_SERVER_TOKEN" || body.GeneratedToken.AuthMode != "bearer" {
		t.Fatalf("generated token metadata = env %q auth %q", body.GeneratedToken.Env, body.GeneratedToken.AuthMode)
	}
	if strings.Contains(rec.Body.String(), `"token_value"`) {
		t.Fatal("PATCH response must not expose server auth token_value")
	}
	if body.Desired.ServerAuth.TokenValue != "" || body.Desired.ServerAuth.GenerateToken != nil {
		t.Fatalf("desired settings should hide write-only auth fields: %+v", body.Desired.ServerAuth)
	}
	if body.Desired.ServerAuth.BearerTokenEnv != "" {
		t.Fatalf("bootstrap desired settings should hide bearer token env, got %q", body.Desired.ServerAuth.BearerTokenEnv)
	}
	if strings.Contains(rec.Body.String(), `"settings"`) || strings.Contains(rec.Body.String(), `"message"`) {
		t.Fatalf("bootstrap PATCH response should not include full settings/message: %s", rec.Body.String())
	}
	if got := os.Getenv("SPEECHKIT_SERVER_TOKEN"); got != body.GeneratedToken.Token {
		t.Fatalf("runtime bearer token env = %q, want generated token", got)
	}
	if app.Cfg.Server.AuthMode != "bearer" {
		t.Fatalf("runtime auth mode = %q, want bearer", app.Cfg.Server.AuthMode)
	}

	stored, ok, err := config.LoadServerModelSettings(config.ServerSettingsPath(app.Cfg))
	if err != nil {
		t.Fatalf("LoadServerModelSettings: %v", err)
	}
	if !ok {
		t.Fatal("expected saved settings")
	}
	if stored.ServerAuth.TokenValue != "" || stored.ServerAuth.GenerateToken != nil {
		t.Fatalf("stored settings should hide write-only auth fields: %+v", stored.ServerAuth)
	}
}

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
