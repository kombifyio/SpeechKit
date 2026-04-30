package config

import (
	"os"
	"path/filepath"
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
	if sanitized := SanitizeServerModelSettings(loaded); sanitized.Credentials.Google.Value != "" {
		t.Fatal("sanitized settings should remove raw credential values")
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

func boolPtrForTest(v bool) *bool {
	return &v
}

func stringPtrForTest(v string) *string {
	return &v
}
