package config

import "github.com/kombifyio/SpeechKit/internal/voiceagentprofile"

func defaults() *Config {
	cfg := &Config{
		General: GeneralConfig{
			Language:                 "de",
			Hotkey:                   "ctrl+win",
			DictateHotkey:            "ctrl+win",
			AssistHotkey:             "win+alt",
			VoiceAgentHotkey:         "ctrl+shift",
			DictateHotkeyBehavior:    HotkeyBehaviorPushToTalk,
			AssistHotkeyBehavior:     HotkeyBehaviorPushToTalk,
			VoiceAgentHotkeyBehavior: HotkeyBehaviorPushToTalk,
			DictateEnabled:           true,
			AssistEnabled:            true,
			VoiceAgentEnabled:        true,
			AutoStartOnLaunch:        false,
			AgentHotkey:              "win+alt",
			AgentMode:                "assist",
			ActiveMode:               "none",
			HotkeyMode:               HotkeyBehaviorPushToTalk,
			AutoStopSilenceMs:        500,
			FastModeSilenceMs:        1500,
		},
		Audio: AudioConfig{
			Backend:     "windows-wasapi-malgo",
			SampleRate:  16000,
			Channels:    1,
			FrameSizeMs: 32,
			LatencyHint: "interactive",
		},
		Vocabulary: VocabularyConfig{
			Dictionary: "",
		},
		ModelSelection: BuiltInPrimaryModelSelectionDefaults(),
		ServerConnection: ServerConnectionConfig{
			Enabled:           false,
			URL:               "",
			BearerTokenEnv:    "SPEECHKIT_SERVER_TOKEN", //nolint:gosec // env var name, not a credential
			FallbackToLocal:   true,
			RequestTimeoutSec: 30,
		},
		UI: UIConfig{
			OverlayEnabled:          true,
			OverlayPosition:         "bottom",
			OverlayMovable:          false,
			OverlayFreeX:            0,
			OverlayFreeY:            0,
			OverlayMonitorPositions: map[string]OverlayFreePosition{},
			Visualizer:              "pill",
			Design:                  "default",
			AssistOverlayMode:       OverlayFeedbackModeSmallFeedback,
			VoiceAgentOverlayMode:   OverlayFeedbackModeSmallFeedback,
		},
		Local: LocalConfig{
			Enabled: true,
			Model:   DefaultLocalSTTModel,
			Port:    8080,
			GPU:     "auto",
		},
		LocalLLM: LocalLLMConfig{
			Enabled:      false,
			BaseURL:      DefaultLocalLLMBaseURL,
			Model:        DefaultLocalLLMModel,
			Port:         8082,
			GPU:          "auto",
			UtilityModel: DefaultLocalLLMModel,
			AssistModel:  DefaultLocalLLMModel,
			AgentModel:   DefaultLocalLLMModel,
		},
		VPS: VPSConfig{
			Enabled:   false,
			APIKeyEnv: "VPS_API_KEY",
		},
		HuggingFace: HuggingFaceConfig{
			Enabled:      false,
			Model:        "openai/whisper-large-v3",
			UtilityModel: "",
			AssistModel:  "",
			AgentModel:   "",
			TokenEnv:     "HF_TOKEN", //nolint:gosec // not a credential, field name triggers false positive
		},
		Routing: RoutingConfig{
			Strategy:                "local-only",
			PreferLocalUnderSeconds: 10,
			ParallelCloud:           false,
			ReplaceOnBetter:         false,
		},
		Feedback: FeedbackConfig{
			SaveAudio:          true,
			AudioRetentionDays: 7,
			MaxAudioStorageMB:  500,
		},
		Store: StoreConfig{
			Backend:            "sqlite",
			SaveAudio:          true,
			AudioRetentionDays: 7,
			MaxAudioStorageMB:  500,
		},
		TTS: TTSConfig{
			Enabled:  true,
			Strategy: "cloud-first",
			Speed:    1.0,
			Format:   "mp3",
			OpenAI: TTSOpenAI{
				Enabled: true,
				Model:   "tts-1",
				Voice:   "nova",
			},
			Google: TTSGoogle{
				Enabled: false,
				Voice:   "en-US-Neural2-J",
			},
			HuggingFace: TTSHuggingFace{
				Enabled: false,
				Model:   "parler-tts/parler-tts-mini-multilingual-v1.1",
			},
			Local: TTSLocal{
				Enabled: false,
				Model:   "hexgrad/Kokoro-82M",
				Port:    8081,
			},
		},
		VoiceAgent: VoiceAgentConfig{
			Enabled: true,
			Model:   defaultGeminiNativeAudioModel,
			// Same-provider Gemini fallback keeps Voice Agent up when the 3.1
			// preview endpoint has transient issues. Cross-provider fallbacks
			// (OpenAI gpt-realtime-mini) can be configured explicitly per
			// deployment via the separate model_selection section.
			FallbackModel:                   fallbackGeminiNativeAudioModel,
			Voice:                           "Kore",
			AgentProfileID:                  voiceagentprofile.DefaultID,
			AgentSequenceID:                 "",
			FrameworkPrompt:                 "",
			RefinementPrompt:                "",
			Instruction:                     "",
			AutoStartOnLaunch:               false,
			CloseBehavior:                   VoiceAgentCloseBehaviorContinue,
			ReminderAfterIdleSec:            300,
			DeactivateAfterIdleSec:          900,
			PipelineFallback:                false,
			ShowPrompter:                    true,
			EnableSessionSummary:            true,
			EnableInputTranscript:           true,
			EnableOutputTranscript:          true,
			EnableAffectiveDialog:           false,
			ThinkingEnabled:                 false,
			IncludeThoughts:                 false,
			ThinkingBudget:                  0,
			ThinkingLevel:                   "medium",
			ContextCompressionEnabled:       true,
			ContextCompressionTriggerTokens: 12000,
			ContextCompressionTargetTokens:  6000,
			AutomaticActivityDetection:      true,
			ActivityHandling:                "start_of_activity_interrupts",
			TurnCoverage:                    "turn_includes_only_activity",
			VADStartSensitivity:             "low",
			VADEndSensitivity:               "low",
			VADPrefixPaddingMs:              100,
			VADSilenceDurationMs:            700,
		},
		Providers: ProvidersConfig{
			OpenAI: OpenAIProviderConfig{
				APIKeyEnv:     "OPENAI_API_KEY", //nolint:gosec // not a credential, field name triggers false positive
				STTModel:      "whisper-1",      // Fallback only; HuggingFace is primary STT
				UtilityModel:  "gpt-5.4-mini-2026-03-17",
				AssistModel:   "gpt-5.4-2026-03-05",
				AgentModel:    "gpt-5.4-2026-03-05",
				TTSModel:      "tts-1",
				TTSVoice:      "nova",
				RealtimeModel: "gpt-realtime-mini",
			},
			Groq: GroqProviderConfig{
				APIKeyEnv:    "GROQ_API_KEY", //nolint:gosec // not a credential, field name triggers false positive
				STTModel:     "whisper-large-v3-turbo",
				UtilityModel: "llama-3.1-8b-instant",
				AssistModel:  "llama-3.3-70b-versatile",
				AgentModel:   "llama-3.3-70b-versatile",
			},
			Google: GoogleProviderConfig{
				APIKeyEnv:    "GOOGLE_AI_API_KEY", //nolint:gosec // not a credential, field name triggers false positive
				STTModel:     "chirp_3",
				UtilityModel: "gemini-2.5-flash-lite",
				AssistModel:  "gemini-2.5-flash",
				AgentModel:   "gemini-2.5-pro",
			},
			Ollama: OllamaProviderConfig{
				BaseURL:      "http://localhost:11434",
				UtilityModel: "gemma4:e4b",
				AssistModel:  "gemma4:e4b",
				AgentModel:   "gemma4:e4b",
			},
			OpenRouter: OpenRouterProviderConfig{
				APIKeyEnv:    "OPENROUTER_API_KEY", //nolint:gosec // not a credential, field name triggers false positive
				STTModel:     "openai/whisper-1",
				UtilityModel: "meta-llama/llama-3.1-8b-instruct",
				AssistModel:  "google/gemini-2.5-flash",
				AgentModel:   "google/gemini-2.5-flash",
			},
		},
		Server: ServerConfig{
			ListenAddr:               ":8080",
			PublicURL:                "",
			Modes:                    nil, // nil = all three modes enabled
			AuthMode:                 "bearer",
			BearerTokenEnv:           "SPEECHKIT_SERVER_TOKEN", //nolint:gosec // env var name, not a credential
			BearerRole:               "",
			EdgeAuthSecretEnv:        "EDGE_AUTH_SECRET", //nolint:gosec // env var name, not a credential
			CORSAllowedOrigins:       []string{},
			RateLimitRPS:             10,
			RateLimitBurst:           20,
			MaxUploadMB:              25,
			MaxVoiceAgentSessions:    100,
			MaxSessionsPerUser:       3,
			TicketTTLSec:             30,
			VoiceAgentIdleTimeoutSec: 900,
			WhisperBinary:            "/usr/local/bin/whisper-server",
			WhisperPort:              8180,
			ModelDir:                 "/var/lib/speechkit/models",
			LogFormat:                "json",
			LogLevel:                 "info",
			Features: ServerFeaturesConfig{
				Catalog:      true,
				StorageReads: true,
				Vocabulary:   true,
				TTSDirect:    true,
			},
		},
	}
	ApplyManagedDevServerDefaults(cfg)
	return cfg
}
