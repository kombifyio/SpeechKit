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
)

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

	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/server/settings", nil))

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

	registerServerSettings(app)

	payload := []byte(`{
		"onboarding_complete": true,
		"modes": {
			"dictation": {"provider_kind":"local_built_in","profile_id":"stt.local.whispercpp","model":"whisper-1"},
			"assist": {"provider_kind":"local_built_in","profile_id":"assist.builtin.gemma4-e4b","model":"ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M"},
			"voice_agent": {"provider_kind":"local_built_in","profile_id":"realtime.builtin.pipeline","model":"ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M"}
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

	get := httptest.NewRecorder()
	app.Mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/server/settings", nil))
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
	app.Cfg.Server.AuthMode = "none"
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
				Model:        "ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M",
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
