package config

import "testing"

func TestApplyServerRuntimeDefaults_OptInConfiguresSelfHostedStack(t *testing.T) {
	t.Setenv(ServerSelfHostedDefaultsEnv, "true")
	t.Setenv("GOOGLE_AI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	cfg := defaults()
	notes := ApplyServerRuntimeDefaults(cfg)

	if len(notes) == 0 {
		t.Fatal("expected startup notes for applied defaults")
	}
	if !cfg.VPS.Enabled || cfg.VPS.URL != defaultServerSTTURL {
		t.Fatalf("VPS STT default = enabled %v url %q", cfg.VPS.Enabled, cfg.VPS.URL)
	}
	if cfg.VPS.Model != "whisper-1" {
		t.Fatalf("VPS STT model = %q, want whisper-1", cfg.VPS.Model)
	}
	if !cfg.LocalLLM.Enabled || cfg.LocalLLM.BaseURL != defaultServerLLMBaseURL {
		t.Fatalf("LocalLLM default = enabled %v base %q", cfg.LocalLLM.Enabled, cfg.LocalLLM.BaseURL)
	}
	if defaultServerLLMModel != "ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M" {
		t.Fatalf("default server LLM model = %q, want ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M", defaultServerLLMModel)
	}
	if cfg.LocalLLM.AssistModel != defaultServerLLMModel || cfg.LocalLLM.AgentModel != defaultServerLLMModel {
		t.Fatalf("LocalLLM models = assist %q agent %q", cfg.LocalLLM.AssistModel, cfg.LocalLLM.AgentModel)
	}
	if cfg.VoiceAgent.Provider != "cascaded" {
		t.Fatalf("VoiceAgent.Provider = %q, want cascaded", cfg.VoiceAgent.Provider)
	}
	if cfg.TTS.Enabled {
		t.Fatal("TTS should be disabled when no configured TTS credential exists")
	}
	if len(cfg.Personas) != 1 || cfg.Personas[0].ID != "default" {
		t.Fatalf("default persona not seeded: %#v", cfg.Personas)
	}
	if len(cfg.Roles) != 1 || cfg.Roles[0].ID != "default-role" {
		t.Fatalf("default role not seeded: %#v", cfg.Roles)
	}
	if cfg.Store.SQLitePath != defaultServerSQLitePath {
		t.Fatalf("Store.SQLitePath = %q, want %q", cfg.Store.SQLitePath, defaultServerSQLitePath)
	}
}

func TestApplyServerRuntimeDefaults_OptOutLeavesDefaults(t *testing.T) {
	cfg := defaults()
	ApplyServerRuntimeDefaults(cfg)

	if cfg.VPS.Enabled {
		t.Fatal("VPS should remain disabled without opt-in")
	}
	if cfg.LocalLLM.Enabled {
		t.Fatal("LocalLLM should remain disabled without opt-in")
	}
	if cfg.VoiceAgent.Provider != "" {
		t.Fatalf("VoiceAgent.Provider = %q, want empty default", cfg.VoiceAgent.Provider)
	}
}

func TestApplyServerRuntimeDefaults_NormalizesLegacyServerLLMAlias(t *testing.T) {
	t.Setenv(ServerSelfHostedDefaultsEnv, "true")
	t.Setenv("SPEECHKIT_SELFHOSTED_LLM_MODEL", "speechkit-gemma4")
	t.Setenv("GOOGLE_AI_API_KEY", "")

	cfg := defaults()
	ApplyServerRuntimeDefaults(cfg)

	if cfg.LocalLLM.AssistModel != defaultServerLLMModel || cfg.LocalLLM.AgentModel != defaultServerLLMModel {
		t.Fatalf("LocalLLM models = assist %q agent %q, want %q", cfg.LocalLLM.AssistModel, cfg.LocalLLM.AgentModel, defaultServerLLMModel)
	}
}

func TestApplyServerRuntimeDefaults_GoogleKeyKeepsGeminiDefault(t *testing.T) {
	t.Setenv(ServerSelfHostedDefaultsEnv, "true")
	t.Setenv("GOOGLE_AI_API_KEY", "present")

	cfg := defaults()
	ApplyServerRuntimeDefaults(cfg)

	if cfg.VoiceAgent.Provider == "cascaded" {
		t.Fatal("Google credential should keep the Gemini Live default")
	}
}
