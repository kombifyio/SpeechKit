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
				ProfileID:    "stt.openai.gpt-4o-transcribe",
				Model:        "gpt-4o-transcribe",
			},
			Assist: ServerModeSetting{
				ProviderKind: "local_built_in",
				ProfileID:    "assist.builtin.gemma4-e4b",
				Model:        "ggml-org/gemma-4-E2B-it-GGUF:Q8_0",
			},
			VoiceAgent: ServerModeSetting{
				ProviderKind: "local_built_in",
				ProfileID:    "realtime.builtin.pipeline",
				Model:        "ggml-org/gemma-4-E2B-it-GGUF:Q8_0",
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
	if !cfg.LocalLLM.Enabled || cfg.LocalLLM.AssistModel != "ggml-org/gemma-4-E2B-it-GGUF:Q8_0" || cfg.LocalLLM.AgentModel != "ggml-org/gemma-4-E2B-it-GGUF:Q8_0" {
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

func TestApplyServerModelSettings_AppliesDirectVoiceAgentProviders(t *testing.T) {
	tests := []struct {
		name            string
		profileID       string
		model           string
		wantProvider    string
		wantModel       string
		wantOpenAIModel string
	}{
		{name: "gemini", profileID: "realtime.google.gemini-native-audio", model: "gemini-3.1-flash-live-preview", wantProvider: "gemini", wantModel: "gemini-3.1-flash-live-preview"},
		{name: "deepgram", profileID: "realtime.deepgram.voice-agent", model: "flux-general-multi", wantProvider: "deepgram", wantModel: "flux-general-multi"},
		{name: "assemblyai", profileID: "realtime.assemblyai.voice-agent", model: "assemblyai-voice-agent", wantProvider: "assemblyai", wantModel: "assemblyai-voice-agent"},
		{name: "openai", profileID: "realtime.openai.gpt-realtime-2", model: "gpt-realtime-2", wantProvider: "openai", wantOpenAIModel: "gpt-realtime-2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaults()
			ApplyServerModelSettings(cfg, ServerModelSettings{
				Modes: ServerModeProviderSettings{
					VoiceAgent: ServerModeSetting{
						ProviderKind: "direct_provider",
						ProfileID:    tt.profileID,
						Model:        tt.model,
					},
				},
			})

			if cfg.VoiceAgent.Provider != tt.wantProvider {
				t.Fatalf("VoiceAgent.Provider = %q, want %q", cfg.VoiceAgent.Provider, tt.wantProvider)
			}
			if tt.wantOpenAIModel != "" {
				if cfg.Providers.OpenAI.RealtimeModel != tt.wantOpenAIModel {
					t.Fatalf("OpenAI realtime model = %q, want %q", cfg.Providers.OpenAI.RealtimeModel, tt.wantOpenAIModel)
				}
				return
			}
			if cfg.VoiceAgent.Model != tt.wantModel {
				t.Fatalf("VoiceAgent.Model = %q, want %q", cfg.VoiceAgent.Model, tt.wantModel)
			}
		})
	}
}

func TestApplyServerModelSettings_AppliesDirectDictationProviders(t *testing.T) {
	tests := []struct {
		name       string
		profileID  string
		model      string
		assertFunc func(*testing.T, *Config)
	}{
		{
			name:      "openai",
			profileID: "stt.openai.gpt-4o-transcribe",
			model:     "gpt-4o-transcribe",
			assertFunc: func(t *testing.T, cfg *Config) {
				t.Helper()
				if !cfg.Providers.OpenAI.Enabled || cfg.Providers.OpenAI.STTModel != "gpt-4o-transcribe" {
					t.Fatalf("OpenAI STT = enabled %v model %q", cfg.Providers.OpenAI.Enabled, cfg.Providers.OpenAI.STTModel)
				}
			},
		},
		{
			name:      "groq",
			profileID: "stt.groq.whisper-large-v3-turbo",
			model:     "whisper-large-v3-turbo",
			assertFunc: func(t *testing.T, cfg *Config) {
				t.Helper()
				if !cfg.Providers.Groq.Enabled || cfg.Providers.Groq.STTModel != "whisper-large-v3-turbo" {
					t.Fatalf("Groq STT = enabled %v model %q", cfg.Providers.Groq.Enabled, cfg.Providers.Groq.STTModel)
				}
			},
		},
		{
			name:      "deepgram",
			profileID: "stt.deepgram.nova-3",
			model:     "nova-3",
			assertFunc: func(t *testing.T, cfg *Config) {
				t.Helper()
				if !cfg.Providers.Deepgram.Enabled || cfg.Providers.Deepgram.STTModel != "nova-3" {
					t.Fatalf("Deepgram STT = enabled %v model %q", cfg.Providers.Deepgram.Enabled, cfg.Providers.Deepgram.STTModel)
				}
			},
		},
		{
			name:      "assemblyai",
			profileID: "stt.assemblyai.universal",
			model:     "universal-3-pro,universal-2",
			assertFunc: func(t *testing.T, cfg *Config) {
				t.Helper()
				if !cfg.Providers.AssemblyAI.Enabled || cfg.Providers.AssemblyAI.STTModels != DefaultAssemblyAISTTModels {
					t.Fatalf("AssemblyAI STT = enabled %v models %q", cfg.Providers.AssemblyAI.Enabled, cfg.Providers.AssemblyAI.STTModels)
				}
				if !cfg.Providers.AssemblyAI.StreamingLLM {
					t.Fatal("AssemblyAI streaming LLM should stay on")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaults()
			ApplyServerModelSettings(cfg, ServerModelSettings{
				Modes: ServerModeProviderSettings{
					Dictation: ServerModeSetting{
						ProviderKind: "direct_provider",
						ProfileID:    tt.profileID,
						Model:        tt.model,
					},
				},
			})

			if cfg.Routing.Strategy != "cloud-only" || cfg.Local.Enabled || cfg.VPS.Enabled {
				t.Fatalf("direct dictation routing = strategy %q local %v vps %v", cfg.Routing.Strategy, cfg.Local.Enabled, cfg.VPS.Enabled)
			}
			tt.assertFunc(t, cfg)
		})
	}
}

func TestNormalizedProviderKindUsesFrameworkProfileCatalog(t *testing.T) {
	cases := map[string]string{
		"stt.local.whispercpp":          "local_built_in",
		"assist.ollama.gemma4-e4b":      "local_provider",
		"stt.openrouter.whisper-1":      "cloud_provider",
		"assist.groq.llama-3.3-70b":     "direct_provider",
		"realtime.deepgram.voice-agent": "direct_provider",
		"tts.openedai.kokoro":           "local_provider",
		"stt.google.chirp-3":            "direct_provider",
	}
	for profileID, want := range cases {
		if got := normalizedProviderKind(ServerModeSetting{ProfileID: profileID}); got != want {
			t.Fatalf("normalizedProviderKind(%q) = %q, want %q", profileID, got, want)
		}
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
	if loaded.Modes.Assist.Model != "ggml-org/gemma-4-E2B-it-GGUF:Q8_0" || loaded.Modes.VoiceAgent.Model != "ggml-org/gemma-4-E2B-it-GGUF:Q8_0" {
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

func TestSaveServerModelSettings_RejectsInvalidSecurityAndProviderSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings ServerModelSettings
		want     string
	}{
		{
			name: "admin auth enabled without password",
			settings: ServerModelSettings{
				AdminAuth: ServerAdminAuthSettings{
					Enabled:  boolPtrForTest(true),
					Username: "admin",
				},
			},
			want: "admin_auth.password is required",
		},
		{
			name: "admin password without username",
			settings: ServerModelSettings{
				AdminAuth: ServerAdminAuthSettings{
					PasswordValue: "secret",
				},
			},
			want: "admin_auth.username is required",
		},
		{
			name: "invalid server auth mode",
			settings: ServerModelSettings{
				ServerAuth: ServerAuthSettings{Mode: "none"},
			},
			want: "server_auth.mode must be managed_bearer or self_managed",
		},
		{
			name: "invalid env name",
			settings: ServerModelSettings{
				Credentials: ServerCredentialSettings{
					OpenAI: ServerProviderCredentialSettings{Env: "not valid"},
				},
			},
			want: "openai.env must be a valid environment variable name",
		},
		{
			name: "invalid assist tool",
			settings: ServerModelSettings{
				Assist: ServerAssistSettings{EnabledTools: []string{"summarize", "../shell"}},
			},
			want: "assist.enabled_tools contains an invalid tool id",
		},
		{
			name: "invalid provider kind",
			settings: ServerModelSettings{
				Modes: ServerModeProviderSettings{
					VoiceAgent: ServerModeSetting{ProviderKind: "unknown"},
				},
			},
			want: "voice.provider_kind must be local_built_in",
		},
		{
			name: "invalid voice provider",
			settings: ServerModelSettings{
				VoiceAgent: ServerVoiceAgentSettings{Provider: "shell"},
			},
			want: "voice_agent.provider must be cascaded, gemini/google, openai, deepgram, assemblyai, kombify-agent, or moshi",
		},
		{
			name: "oversized raw credential value",
			settings: ServerModelSettings{
				Credentials: ServerCredentialSettings{
					Google: ServerProviderCredentialSettings{Value: strings.Repeat("x", 4097)},
				},
			},
			want: "google.value is too long",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "server-settings.json")
			err := SaveServerModelSettings(path, tt.settings)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want to contain %q", err, tt.want)
			}
		})
	}
}

func boolPtrForTest(v bool) *bool {
	return &v
}

func stringPtrForTest(v string) *string {
	return &v
}
