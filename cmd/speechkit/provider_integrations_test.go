package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/secrets"
)

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestProviderIntegrationStatesExposeGatewayAndDerivedGeminiSelection(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Providers.Google.Enabled = false
	cfg.ModelSelection.VoiceAgent.PrimaryProfileID = "realtime.google.gemini-native-audio"

	states := providerIntegrationStates(cfg)

	google := states["google"]
	if !google.Enabled {
		t.Fatal("google integration should be enabled when Gemini is already selected")
	}
	if got, want := google.ProviderKind, "direct_provider"; got != want {
		t.Fatalf("google provider kind = %q, want %q", got, want)
	}
	if !containsString(google.SupportedModes, modeVoiceAgent) {
		t.Fatalf("google supported modes = %v, want voice_agent", google.SupportedModes)
	}

	openrouter := states["openrouter"]
	if got, want := openrouter.ProviderKind, "cloud_provider"; got != want {
		t.Fatalf("openrouter provider kind = %q, want %q", got, want)
	}
	if got, want := openrouter.IntegrationKind, "cloud_gateway"; got != want {
		t.Fatalf("openrouter integration kind = %q, want %q", got, want)
	}
	if got, want := openrouter.SetupURL, "https://openrouter.ai/settings/keys"; got != want {
		t.Fatalf("openrouter setup URL = %q, want %q", got, want)
	}
	if got, want := strings.Join(openrouter.SupportedModes, ","), strings.Join([]string{modeDictate, modeAssist, modeVoiceAgent}, ","); got != want {
		t.Fatalf("openrouter supported modes = %q, want %q", got, want)
	}
}

func TestUpdateProviderIntegrationDisablesProviderAndResetsAffectedSelections(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Providers.OpenAI.Enabled = true
	cfg.ModelSelection.Dictate.PrimaryProfileID = "stt.openai.whisper-1"
	cfg.ModelSelection.Assist.PrimaryProfileID = "assist.openai.gpt-5.4"
	cfg.ModelSelection.VoiceAgent.PrimaryProfileID = "realtime.google.gemini-native-audio"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	if _, err := updateProviderIntegration(t.Context(), cfgPath, "openai", false, cfg, &appState{}, &router.Router{}); err != nil {
		t.Fatalf("disable openai: %v", err)
	}

	if cfg.Providers.OpenAI.Enabled {
		t.Fatal("openai provider should be disabled")
	}
	if got, want := cfg.ModelSelection.Dictate.PrimaryProfileID, config.DefaultDictatePrimaryProfileID; got != want {
		t.Fatalf("dictate primary = %q, want %q", got, want)
	}
	if got, want := cfg.ModelSelection.Assist.PrimaryProfileID, config.DefaultAssistPrimaryProfileID; got != want {
		t.Fatalf("assist primary = %q, want %q", got, want)
	}
	if got, want := cfg.ModelSelection.VoiceAgent.PrimaryProfileID, "realtime.google.gemini-native-audio"; got != want {
		t.Fatalf("voice primary = %q, want unchanged %q", got, want)
	}

	if _, err := updateProviderIntegration(t.Context(), cfgPath, "google", false, cfg, &appState{}, &router.Router{}); err != nil {
		t.Fatalf("disable google: %v", err)
	}
	if got, want := cfg.ModelSelection.VoiceAgent.PrimaryProfileID, config.DefaultVoiceAgentPrimaryProfileID; got != want {
		t.Fatalf("voice primary after google disable = %q, want %q", got, want)
	}
}

func TestProviderCredentialSaveEnablesAndPersistsIntegration(t *testing.T) {
	restore := secrets.UseMemoryStoreForTests()
	defer restore()

	cfg := defaultTestConfig()
	cfg.Providers.OpenAI.Enabled = false
	cfg.Providers.OpenAI.APIKeyEnv = "SPEECHKIT_TEST_OPENAI_KEY"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	handler := assetHandler(cfg, cfgPath, &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	form := url.Values{
		"provider":   {"openai"},
		"credential": {"test-openai-key"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/provider-credentials/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !cfg.Providers.OpenAI.Enabled {
		t.Fatal("saving openai credential should enable the integration")
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !loaded.Providers.OpenAI.Enabled {
		t.Fatal("saving openai credential should persist enabled integration")
	}
}
