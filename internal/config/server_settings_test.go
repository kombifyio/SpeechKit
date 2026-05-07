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
	if sanitized := SanitizeServerModelSettings(loaded); sanitized.Credentials.Google.Value != "" {
		t.Fatal("sanitized settings should remove raw credential values")
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

func TestServerSettingsPathPrefersEnvThenSQLiteDirectory(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "from-env.json")
	t.Setenv(ServerSettingsPathEnv, " "+envPath+" ")
	if got := ServerSettingsPath(&Config{}); got != envPath {
		t.Fatalf("ServerSettingsPath env = %q, want %q", got, envPath)
	}

	t.Setenv(ServerSettingsPathEnv, "")
	cfg := defaults()
	cfg.Store.SQLitePath = filepath.Join(t.TempDir(), "speechkit.db")
	want := filepath.Join(filepath.Dir(cfg.Store.SQLitePath), "server-settings.json")
	if got := ServerSettingsPath(cfg); got != want {
		t.Fatalf("ServerSettingsPath sqlite = %q, want %q", got, want)
	}
}

func TestNormalizeServerModelSettingsCleansMultilineToolsAndProfile(t *testing.T) {
	settings := ServerModelSettings{
		Dictation: ServerDictationSettings{Dictionary: stringPtrForTest(" first \r\n\r\n second ")},
		Assist:    ServerAssistSettings{EnabledTools: []string{" Summarize ", "summarize", "", "COPY_LAST"}},
		VoiceAgent: ServerVoiceAgentSettings{
			AgentProfileID: "brainstorming_companion",
			PromptTemplate: stringPtrForTest(" line one \r line two "),
		},
	}

	normalized := NormalizeServerModelSettings(settings)

	if got := *normalized.Dictation.Dictionary; got != "first\n\nsecond" {
		t.Fatalf("dictionary = %q", got)
	}
	if got := *normalized.VoiceAgent.PromptTemplate; got != "line one\nline two" {
		t.Fatalf("prompt = %q", got)
	}
	if got := normalized.Assist.EnabledTools; len(got) != 2 || got[0] != "summarize" || got[1] != "copy_last" {
		t.Fatalf("enabled tools = %#v", got)
	}
	if normalized.VoiceAgent.AgentProfileID != "brainstorming_companion" {
		t.Fatalf("profile = %q", normalized.VoiceAgent.AgentProfileID)
	}
}

func TestApplyServerModelSettingsCoversCredentialAndModeBranches(t *testing.T) {
	t.Setenv("OPENAI_UI_KEY", "")
	t.Setenv("GROQ_UI_KEY", "")
	t.Setenv("GOOGLE_UI_KEY", "")
	t.Setenv("HF_UI_KEY", "")
	t.Setenv("OPENROUTER_UI_KEY", "")

	cfg := defaults()
	settings := ServerModelSettings{
		Credentials: ServerCredentialSettings{
			OpenAI:      ServerProviderCredentialSettings{Enabled: boolPtrForTest(true), Env: "OPENAI_UI_KEY", Value: "openai-value"},
			Groq:        ServerProviderCredentialSettings{Enabled: boolPtrForTest(true), Env: "GROQ_UI_KEY", Value: "groq-value"},
			Google:      ServerProviderCredentialSettings{Enabled: boolPtrForTest(true), Env: "GOOGLE_UI_KEY", Value: "google-value"},
			HuggingFace: ServerProviderCredentialSettings{Enabled: boolPtrForTest(true), Env: "HF_UI_KEY", Value: "hf-value"},
			OpenRouter:  ServerProviderCredentialSettings{Enabled: boolPtrForTest(true), Env: "OPENROUTER_UI_KEY", Value: "openrouter-value"},
		},
		Modes: ServerModeProviderSettings{
			Dictation: ServerModeSetting{
				ProviderKind: "cloud_provider",
				ProfileID:    "stt.openrouter.whisper",
				Model:        "openrouter-stt",
			},
			Assist: ServerModeSetting{
				ProviderKind: "direct_provider",
				ProfileID:    "assist.google.gemini",
				Model:        "gemini-2.5",
			},
			VoiceAgent: ServerModeSetting{
				ProviderKind: "local_provider",
				ProfileID:    "realtime.ollama.pipeline",
				Model:        "llama-local",
			},
		},
		STT:        ServerSTTSettings{Enabled: boolPtrForTest(false), URL: " http://stt.local ", Model: " stt-model "},
		LLM:        ServerLLMSettings{Enabled: boolPtrForTest(true), BaseURL: " http://llm.local ", UtilityModel: " util ", AssistModel: " assist ", AgentModel: " agent "},
		VoiceAgent: ServerVoiceAgentSettings{Provider: " GEMINI ", AgentProfileID: "brainstorming_companion", AgentSequenceID: " seq-1 "},
		TTS:        ServerOptionalTTSSettings{Enabled: boolPtrForTest(true)},
	}

	notes := ApplyServerModelSettings(cfg, settings)

	if len(notes) < 10 {
		t.Fatalf("notes = %#v, want substantial settings application", notes)
	}
	for envName, want := range map[string]string{
		"OPENAI_UI_KEY":     "openai-value",
		"GROQ_UI_KEY":       "groq-value",
		"GOOGLE_UI_KEY":     "google-value",
		"HF_UI_KEY":         "hf-value",
		"OPENROUTER_UI_KEY": "openrouter-value",
	} {
		if got := os.Getenv(envName); got != want {
			t.Fatalf("%s = %q, want %q", envName, got, want)
		}
	}
	if !cfg.Providers.OpenRouter.Enabled || cfg.Providers.OpenRouter.STTModel != "openrouter-stt" {
		t.Fatalf("openrouter STT = enabled %v model %q", cfg.Providers.OpenRouter.Enabled, cfg.Providers.OpenRouter.STTModel)
	}
	if !cfg.Providers.Google.Enabled || cfg.Providers.Google.AssistModel != "gemini-2.5" {
		t.Fatalf("google assist = enabled %v model %q", cfg.Providers.Google.Enabled, cfg.Providers.Google.AssistModel)
	}
	if !cfg.Providers.Ollama.Enabled || cfg.Providers.Ollama.AgentModel != "llama-local" {
		t.Fatalf("ollama agent = enabled %v model %q", cfg.Providers.Ollama.Enabled, cfg.Providers.Ollama.AgentModel)
	}
	if cfg.VPS.Enabled || cfg.VPS.URL != "http://stt.local" || cfg.VPS.Model != "stt-model" {
		t.Fatalf("stt = enabled %v url %q model %q", cfg.VPS.Enabled, cfg.VPS.URL, cfg.VPS.Model)
	}
	if !cfg.LocalLLM.Enabled || cfg.LocalLLM.BaseURL != "http://llm.local" || cfg.LocalLLM.UtilityModel != "util" {
		t.Fatalf("llm = enabled %v base %q utility %q", cfg.LocalLLM.Enabled, cfg.LocalLLM.BaseURL, cfg.LocalLLM.UtilityModel)
	}
	if cfg.VoiceAgent.Provider != "gemini" || cfg.VoiceAgent.AgentProfileID != "brainstorming_companion" || cfg.VoiceAgent.AgentSequenceID != "seq-1" {
		t.Fatalf("voice agent = provider %q profile %q sequence %q", cfg.VoiceAgent.Provider, cfg.VoiceAgent.AgentProfileID, cfg.VoiceAgent.AgentSequenceID)
	}
	if !cfg.TTS.Enabled {
		t.Fatal("TTS should be enabled")
	}
}

func TestApplyServerModelSettingsCanDisableModes(t *testing.T) {
	cfg := defaults()
	settings := ServerModelSettings{
		Modes: ServerModeProviderSettings{
			Dictation:  ServerModeSetting{Enabled: boolPtrForTest(false)},
			Assist:     ServerModeSetting{Enabled: boolPtrForTest(false)},
			VoiceAgent: ServerModeSetting{Enabled: boolPtrForTest(false)},
		},
	}

	notes := ApplyServerModelSettings(cfg, settings)

	if cfg.General.DictateEnabled || cfg.General.AssistEnabled || cfg.General.VoiceAgentEnabled {
		t.Fatalf("modes enabled = dictate %v assist %v voice %v", cfg.General.DictateEnabled, cfg.General.AssistEnabled, cfg.General.VoiceAgentEnabled)
	}
	if len(notes) != 3 {
		t.Fatalf("notes = %#v, want three disabled-mode notes", notes)
	}
}

func TestSaveServerModelSettingsValidatesUserEditableFields(t *testing.T) {
	cases := []struct {
		name     string
		settings ServerModelSettings
		want     string
	}{
		{
			name:     "invalid auth mode",
			settings: ServerModelSettings{ServerAuth: ServerAuthSettings{Mode: "oauth"}},
			want:     "server_auth.mode",
		},
		{
			name:     "invalid env",
			settings: ServerModelSettings{ServerAuth: ServerAuthSettings{BearerTokenEnv: "1INVALID"}},
			want:     "server_auth.bearer_token_env",
		},
		{
			name:     "invalid tool id",
			settings: ServerModelSettings{Assist: ServerAssistSettings{EnabledTools: []string{"Bad Tool"}}},
			want:     "assist.enabled_tools",
		},
		{
			name:     "invalid provider kind",
			settings: ServerModelSettings{Modes: ServerModeProviderSettings{Assist: ServerModeSetting{ProviderKind: "remote"}}},
			want:     "assist.provider_kind",
		},
		{
			name:     "invalid url",
			settings: ServerModelSettings{LLM: ServerLLMSettings{BaseURL: "file:///tmp/model"}},
			want:     "llm.base_url",
		},
		{
			name:     "invalid voice provider",
			settings: ServerModelSettings{VoiceAgent: ServerVoiceAgentSettings{Provider: "livekit"}},
			want:     "voice_agent.provider",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := SaveServerModelSettings(filepath.Join(t.TempDir(), "settings.json"), tc.settings)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("SaveServerModelSettings error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestNormalizeBehaviorSettingsFallbacks(t *testing.T) {
	if got := NormalizeHotkeyBehavior(" toggle ", "push_to_talk"); got != HotkeyBehaviorToggle {
		t.Fatalf("hotkey behavior = %q", got)
	}
	if got := NormalizeHotkeyBehavior("bad", "bad"); got != HotkeyBehaviorPushToTalk {
		t.Fatalf("recursive hotkey fallback = %q", got)
	}
	if got := NormalizeVoiceAgentCloseBehavior("new_chat", "continue"); got != VoiceAgentCloseBehaviorNewChat {
		t.Fatalf("voice close behavior = %q", got)
	}
	if got := NormalizeVoiceAgentCloseBehavior("bad", "bad"); got != VoiceAgentCloseBehaviorContinue {
		t.Fatalf("recursive close fallback = %q", got)
	}
	if got := NormalizeOverlayFeedbackMode("BIG_PRODUCTIVITY", "small_feedback"); got != OverlayFeedbackModeBigProductivity {
		t.Fatalf("overlay feedback = %q", got)
	}
	if got := NormalizeOverlayFeedbackMode("bad", "bad"); got != OverlayFeedbackModeSmallFeedback {
		t.Fatalf("recursive overlay fallback = %q", got)
	}
}

func boolPtrForTest(v bool) *bool {
	return &v
}

func stringPtrForTest(v string) *string {
	return &v
}
