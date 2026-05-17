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

func TestApplyServerRuntimeDefaults_UsesPublicURLEnv(t *testing.T) {
	t.Setenv(ServerSelfHostedDefaultsEnv, "true")
	t.Setenv("SPEECHKIT_PUBLIC_URL", " https://speechkit.example.dev/ ")

	cfg := defaults()
	ApplyServerRuntimeDefaults(cfg)

	if cfg.Server.PublicURL != "https://speechkit.example.dev" {
		t.Fatalf("Server.PublicURL = %q, want speechkit public URL from env", cfg.Server.PublicURL)
	}
}

func TestApplyServerRuntimeDefaults_UsesPublicURLEnvWithoutSelfHostedDefaults(t *testing.T) {
	t.Setenv("SPEECHKIT_PUBLIC_URL", " https://api.example.test/v1/speechkit/ ")

	cfg := defaults()
	ApplyServerRuntimeDefaults(cfg)

	if cfg.Server.PublicURL != "https://api.example.test/v1/speechkit" {
		t.Fatalf("Server.PublicURL = %q, want public URL from env", cfg.Server.PublicURL)
	}
	if cfg.VPS.Enabled {
		t.Fatal("VPS defaults should not be enabled by public URL env alone")
	}
	if cfg.LocalLLM.Enabled {
		t.Fatal("LocalLLM defaults should not be enabled by public URL env alone")
	}
}

func TestApplyServerRuntimeDefaults_UsesServerPublicURLAlias(t *testing.T) {
	t.Setenv("SPEECHKIT_SERVER_PUBLIC_URL", " http://localhost:8080/ ")

	cfg := defaults()
	ApplyServerRuntimeDefaults(cfg)

	if cfg.Server.PublicURL != "http://localhost:8080" {
		t.Fatalf("Server.PublicURL = %q, want server public URL alias from env", cfg.Server.PublicURL)
	}
}

func TestApplyServerRuntimeDefaults_PublicURLEnvWinsOverAlias(t *testing.T) {
	t.Setenv("SPEECHKIT_PUBLIC_URL", " https://canonical.example.test/ ")
	t.Setenv("SPEECHKIT_SERVER_PUBLIC_URL", " http://localhost:8080/ ")

	cfg := defaults()
	ApplyServerRuntimeDefaults(cfg)

	if cfg.Server.PublicURL != "https://canonical.example.test" {
		t.Fatalf("Server.PublicURL = %q, want canonical public URL env to win", cfg.Server.PublicURL)
	}
}

func TestApplyServerRuntimeDefaults_UsesPostgresDSNEnvWithoutSelfHostedDefaults(t *testing.T) {
	postgresDSN := postgresTestDSN("speechkit", "secret", "db", "speechkit", "")
	t.Setenv("POSTGRES_DSN", " "+postgresDSN+" ")

	cfg := defaults()
	ApplyServerRuntimeDefaults(cfg)

	if cfg.Store.Backend != "postgres" {
		t.Fatalf("Store.Backend = %q, want postgres", cfg.Store.Backend)
	}
	if cfg.Store.PostgresDSN != postgresDSN {
		t.Fatalf("Store.PostgresDSN = %q, want env DSN", cfg.Store.PostgresDSN)
	}
	if cfg.Store.SQLitePath != "" {
		t.Fatalf("Store.SQLitePath = %q, want empty when postgres DSN env is set", cfg.Store.SQLitePath)
	}
	if cfg.VPS.Enabled {
		t.Fatal("VPS defaults should not be enabled by postgres DSN env alone")
	}
}

func TestApplyServerRuntimeDefaults_EnablesLiveKitTokenMintingFromRuntimeEnv(t *testing.T) {
	t.Setenv("LIVEKIT_URL", " wss://livekit.example.com/ ")
	t.Setenv("LIVEKIT_API_KEY", "present")
	t.Setenv("LIVEKIT_API_SECRET", "present")

	cfg := defaults()
	notes := ApplyServerRuntimeDefaults(cfg)

	if !cfg.Server.LiveKit.Enabled {
		t.Fatal("Server.LiveKit.Enabled = false, want true when URL/key/secret env vars are present")
	}
	if cfg.Server.LiveKit.URL != "wss://livekit.example.com" {
		t.Fatalf("Server.LiveKit.URL = %q", cfg.Server.LiveKit.URL)
	}
	if cfg.Server.LiveKit.TokenTTLSec != 600 || cfg.Server.LiveKit.RoomPrefix != "speechkit-va" {
		t.Fatalf("LiveKit defaults = ttl %d prefix %q", cfg.Server.LiveKit.TokenTTLSec, cfg.Server.LiveKit.RoomPrefix)
	}
	if len(notes) == 0 {
		t.Fatal("expected startup notes for LiveKit runtime defaults")
	}
}

func TestApplyServerRuntimeDefaults_PrefersSpeechKitPostgresDSNEnv(t *testing.T) {
	defaultDSN := postgresTestDSN("speechkit", "secret", "default", "speechkit", "")
	overrideDSN := postgresTestDSN("speechkit", "secret", "override", "speechkit", "")
	t.Setenv("POSTGRES_DSN", defaultDSN)
	t.Setenv("SPEECHKIT_POSTGRES_DSN", overrideDSN)

	cfg := defaults()
	ApplyServerRuntimeDefaults(cfg)

	if cfg.Store.PostgresDSN != overrideDSN {
		t.Fatalf("Store.PostgresDSN = %q, want SpeechKit-specific env DSN", cfg.Store.PostgresDSN)
	}
}
