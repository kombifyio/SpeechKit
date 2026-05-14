package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyServerModelSettings_AppliesProviderMatrixAndCredentialEnv(t *testing.T) {
	t.Setenv("SPEECHKIT_UI_OPENAI_KEY", "")

	cfg := defaults()
	settings := ServerModelSettings{
		OnboardingComplete: true,
		Modes: ServerModeProviderSettings{
			Dictation: ServerModeSetting{
				ProviderKind: "direct_provider",
				ProfileID:    "stt.openai.whisper-1",
				Model:        "whisper-1",
			},
			Assist: ServerModeSetting{
				ProviderKind: "local_built_in",
				ProfileID:    "assist.builtin.gemma4-e4b",
				Model:        "ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M",
			},
			VoiceAgent: ServerModeSetting{
				ProviderKind: "local_built_in",
				ProfileID:    "realtime.builtin.pipeline",
				Model:        "ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M",
			},
		},
		Credentials: ServerCredentialSettings{
			OpenAI: ServerProviderCredentialSettings{
				Enabled: boolPtrForTest(true),
				Env:     "SPEECHKIT_UI_OPENAI_KEY",
				Value:   "sk-test-value",
			},
		},
		Dictation: ServerDictationSettings{
			Dictionary: stringPtrForTest("kombi fire => Kombify\nAcmeOS"),
		},
		Assist: ServerAssistSettings{
			EnabledTools: []string{"summarize"},
		},
		VoiceAgent: ServerVoiceAgentSettings{
			PromptTemplate: stringPtrForTest("Be concise."),
		},
	}

	notes := ApplyServerModelSettings(cfg, settings)

	if len(notes) == 0 {
		t.Fatal("expected notes for applied settings")
	}
	if cfg.VPS.Enabled {
		t.Fatal("direct provider dictation should disable self-hosted VPS STT")
	}
	if !cfg.Providers.OpenAI.Enabled {
		t.Fatal("OpenAI provider should be enabled")
	}
	if got := cfg.Routing.Strategy; got != "cloud-only" {
		t.Fatalf("direct provider dictation should route to cloud STT, got strategy %q", got)
	}
	if cfg.Local.Enabled {
		t.Fatal("direct provider dictation should disable local STT fallback")
	}
	if cfg.Providers.OpenAI.APIKeyEnv != "SPEECHKIT_UI_OPENAI_KEY" {
		t.Fatalf("OpenAI key env = %q", cfg.Providers.OpenAI.APIKeyEnv)
	}
	if got := os.Getenv("SPEECHKIT_UI_OPENAI_KEY"); got != "sk-test-value" {
		t.Fatalf("OpenAI key env value = %q", got)
	}
	if !cfg.LocalLLM.Enabled || cfg.LocalLLM.AssistModel != "ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M" || cfg.LocalLLM.AgentModel != "ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M" {
		t.Fatalf("local LLM = enabled %v assist %q agent %q", cfg.LocalLLM.Enabled, cfg.LocalLLM.AssistModel, cfg.LocalLLM.AgentModel)
	}
	if cfg.VoiceAgent.Provider != "cascaded" {
		t.Fatalf("voice provider = %q, want cascaded", cfg.VoiceAgent.Provider)
	}
	if cfg.Vocabulary.Dictionary != "kombi fire => Kombify\nAcmeOS" {
		t.Fatalf("dictionary = %q", cfg.Vocabulary.Dictionary)
	}
	if got := cfg.Assist.EnabledTools; len(got) != 1 || got[0] != "summarize" {
		t.Fatalf("assist enabled tools = %#v", got)
	}
	if cfg.VoiceAgent.FrameworkPrompt != "Be concise." {
		t.Fatalf("voice prompt = %q", cfg.VoiceAgent.FrameworkPrompt)
	}
}

func TestSaveServerModelSettings_DropsWriteOnlyCredentialValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-settings.json")
	settings := ServerModelSettings{
		OnboardingComplete: true,
		ServerAuth: ServerAuthSettings{
			Mode:           "managed_bearer",
			BearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
			GenerateToken:  boolPtrForTest(true),
			TokenValue:     "server-secret-token",
		},
		AdminAuth: ServerAdminAuthSettings{
			Enabled:       boolPtrForTest(true),
			Username:      "admin",
			PasswordValue: "admin-password",
		},
		Credentials: ServerCredentialSettings{
			Google: ServerProviderCredentialSettings{
				Enabled: boolPtrForTest(true),
				Env:     "GOOGLE_AI_API_KEY",
				Value:   "google-secret",
			},
		},
	}

	if err := SaveServerModelSettings(path, settings); err != nil {
		t.Fatalf("SaveServerModelSettings: %v", err)
	}
	loaded, ok, err := LoadServerModelSettings(path)
	if err != nil {
		t.Fatalf("LoadServerModelSettings: %v", err)
	}
	if !ok {
		t.Fatal("expected saved settings")
	}
	if got := loaded.Credentials.Google.Value; got != "" {
		t.Fatalf("stored google key should be empty, got %q", got)
	}
	if got := loaded.ServerAuth.TokenValue; got != "" {
		t.Fatalf("stored server auth token should be empty, got %q", got)
	}
	if loaded.ServerAuth.GenerateToken != nil {
		t.Fatal("stored server auth generate flag should be empty")
	}
	if loaded.AdminAuth.PasswordValue != "" {
		t.Fatalf("stored admin password should be empty, got %q", loaded.AdminAuth.PasswordValue)
	}
	if loaded.AdminAuth.Username != "admin" {
		t.Fatalf("stored admin username = %q, want admin", loaded.AdminAuth.Username)
	}
	if loaded.AdminAuth.Enabled == nil || !*loaded.AdminAuth.Enabled {
		t.Fatal("stored admin auth should be enabled")
	}
	if loaded.AdminAuth.PasswordHash == "" {
		t.Fatal("stored admin password hash should be set")
	}
	if sanitized := SanitizeServerModelSettings(loaded); sanitized.Credentials.Google.Value != "" {
		t.Fatal("sanitized settings should remove raw credential values")
	}
}

func TestApplyServerModelSettings_AppliesAdminPasswordHash(t *testing.T) {
	cfg := defaults()
	settings := ServerModelSettings{
		AdminAuth: ServerAdminAuthSettings{
			Enabled:      boolPtrForTest(true),
			Username:     "speechkit-admin",
			PasswordHash: "$2a$04$3ZQhRz6fJb3kQGN9cE1uD.RZ8c3E9oB3z4ED5CzSMYRhhAv7n4EHa",
		},
	}

	notes := ApplyServerModelSettings(cfg, settings)

	if cfg.Server.AdminUsername != "speechkit-admin" {
		t.Fatalf("admin username = %q, want speechkit-admin", cfg.Server.AdminUsername)
	}
	if !cfg.Server.AdminAuthEnabled {
		t.Fatal("admin auth should be enabled")
	}
	if cfg.Server.AdminPasswordHash != settings.AdminAuth.PasswordHash {
		t.Fatal("admin password hash was not applied")
	}
	if len(notes) == 0 {
		t.Fatal("expected admin auth application notes")
	}
}

func TestApplyServerModelSettings_AppliesManagedServerAuthToken(t *testing.T) {
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "")

	cfg := defaults()
	cfg.Server.AuthMode = "none"
	settings := ServerModelSettings{
		ServerAuth: ServerAuthSettings{
			Mode:           "managed_bearer",
			BearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
			TokenValue:     "generated-server-token",
		},
	}

	notes := ApplyServerModelSettings(cfg, settings)

	if cfg.Server.AuthMode != "bearer" {
		t.Fatalf("server auth mode = %q, want bearer", cfg.Server.AuthMode)
	}
	if cfg.Server.BearerTokenEnv != "SPEECHKIT_SERVER_TOKEN" {
		t.Fatalf("bearer token env = %q", cfg.Server.BearerTokenEnv)
	}
	if got := os.Getenv("SPEECHKIT_SERVER_TOKEN"); got != "generated-server-token" {
		t.Fatalf("generated bearer env = %q", got)
	}
	if len(notes) == 0 {
		t.Fatal("expected auth application notes")
	}
}

func TestLoadServerModelSettings_NormalizesLegacyServerLLMAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-settings.json")
	settings := ServerModelSettings{
		Modes: ServerModeProviderSettings{
			Assist: ServerModeSetting{
				ProviderKind: "local_built_in",
				ProfileID:    "assist.builtin.gemma4-e4b",
				Model:        "speechkit-gemma4",
			},
			VoiceAgent: ServerModeSetting{
				ProviderKind: "local_built_in",
				ProfileID:    "realtime.builtin.pipeline",
				Model:        "speechkit-gemma4",
			},
		},
	}

	if err := SaveServerModelSettings(path, settings); err != nil {
		t.Fatalf("SaveServerModelSettings: %v", err)
	}
	loaded, ok, err := LoadServerModelSettings(path)
	if err != nil {
		t.Fatalf("LoadServerModelSettings: %v", err)
	}
	if !ok {
		t.Fatal("expected saved settings")
	}
	if loaded.Modes.Assist.Model != "ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M" || loaded.Modes.VoiceAgent.Model != "ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M" {
		t.Fatalf("models = assist %q voice %q", loaded.Modes.Assist.Model, loaded.Modes.VoiceAgent.Model)
	}
}

func TestSaveServerModelSettings_RejectsProviderURLUserInfo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-settings.json")
	settings := ServerModelSettings{
		STT: ServerSTTSettings{
			URL: "https://userinfo@speechkit.example.com",
		},
	}

	err := SaveServerModelSettings(path, settings)
	if err == nil {
		t.Fatal("expected provider URL with user-info to be rejected")
	}
	if !strings.Contains(err.Error(), "user-info") {
		t.Fatalf("expected user-info validation error, got %v", err)
	}
}

func TestSaveServerModelSettings_AllowsSelfHostedProviderURLs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-settings.json")
	settings := ServerModelSettings{
		STT: ServerSTTSettings{
			URL: "http://speechkit-whisper:8080",
		},
		LLM: ServerLLMSettings{
			BaseURL: "http://127.0.0.1:8081/v1",
		},
	}

	if err := SaveServerModelSettings(path, settings); err != nil {
		t.Fatalf("SaveServerModelSettings: %v", err)
	}
}

func boolPtrForTest(v bool) *bool {
	return &v
}

func stringPtrForTest(v string) *string {
	return &v
}
