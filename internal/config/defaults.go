package config

import (
	"strings"

	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

func defaults() *Config {
	cfg := &Config{
		General: GeneralConfig{
			Language:                 "de",
			Hotkey:                   "ctrl+win",
			DictateHotkey:            "ctrl+win",
			AssistHotkey:             "win+alt",
			VoiceAgentHotkey:         "ctrl+shift",
			DictateHotkeyBehavior:    HotkeyBehaviorHoldToTalk,
			AssistHotkeyBehavior:     HotkeyBehaviorHoldToTalk,
			VoiceAgentHotkeyBehavior: HotkeyBehaviorHoldToTalk,
			DictateEnabled:           true,
			AssistEnabled:            true,
			VoiceAgentEnabled:        true,
			AutoStartOnLaunch:        false,
			AgentHotkey:              "win+alt",
			AgentMode:                "assist",
			ActiveMode:               "none",
			HotkeyMode:               HotkeyBehaviorHoldToTalk,
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
			AuthMode:          ServerConnectionAuthModeBearer,
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
			Port:    DefaultLocalSTTPort,
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
		Update: UpdateConfig{
			Enabled:            true,
			ManifestURL:        "https://api.github.com/repos/kombifyio/SpeechKit/releases/latest",
			CheckIntervalHours: 6,
		},
		// SpeechKit has two log surfaces and BOTH are opt-in:
		//
		//  1. Logging.Level   — general application log (transcription events,
		//                       mode switches, wake-word triggers). Visible in
		//                       the dashboard's "Logs" tab when enabled.
		//                       Default "off" — privacy-first; the user opts
		//                       in to "info" or "debug" via Settings or the
		//                       SPEECHKIT_LOG_LEVEL env var.
		//
		//  2. Audit.Enabled   — structured compliance trail (SOC2/ISO27001
		//                       evidence; no transcript content, only event
		//                       metadata). Default false — opt-in via
		//                       Settings → Compliance. Enterprises that need
		//                       the audit trail flip it on AND set retention.
		//
		// Reason: SpeechKit is shipped as a privacy-first reference
		// implementation; users with no compliance obligations should not
		// have either log writing to disk by default. Operators who DO need
		// either log have a single toggle each.
		Logging: LoggingConfig{
			MaxFileSizeMB: 50,
			MaxFiles:      30,
			Level:         "off",
		},
		Audit: AuditConfig{
			Enabled:       false,
			RetentionDays: 90,
		},
		Telemetry: TelemetryConfig{
			UpdateCheck: true,
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
			// can be configured explicitly per deployment via the separate
			// model_selection section.
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
			HoldReleaseGraceSec:             10,
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
				RealtimeModel: "gpt-realtime-2",
			},
			Groq: GroqProviderConfig{
				APIKeyEnv:    "GROQ_API_KEY", //nolint:gosec // not a credential, field name triggers false positive
				STTModel:     "whisper-large-v3-turbo",
				UtilityModel: "llama-3.1-8b-instant",
				AssistModel:  "llama-3.3-70b-versatile",
				AgentModel:   "llama-3.3-70b-versatile",
			},
			Google: GoogleProviderConfig{
				APIKeyEnv:    GoogleAIAPIKeyEnv,         //nolint:gosec // not a credential, field name triggers false positive
				STTAPIKeyEnv: GoogleSTTDefaultAPIKeyEnv, //nolint:gosec // not a credential, field name triggers false positive
				STTModel:     "chirp_3",
				UtilityModel: "gemini-2.5-flash-lite",
				AssistModel:  "gemini-2.5-flash",
				AgentModel:   "gemini-2.5-pro",
				// EU default: reflects target enterprise customer base.
				// US customers override with "us-central1" in config.toml.
				Region: "europe-west3",
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
		Wakeword: WakewordConfig{
			Enabled:              false, // opt-in: mic is only opened when the user explicitly turns this on
			Backend:              WakewordBackendSherpaKWS,
			PhraseID:             "hey_quby",
			Phrase:               "", // empty -> catalog DisplayName for PhraseID is used
			ModelPath:            "", // empty -> catalog FileName resolved against wake-word models dir
			MelspecModelPath:     "",
			EmbeddingModelPath:   "",
			DefaultMode:          WakewordDefaultModeVoiceAgent,
			Threshold:            0, // 0 -> catalog RecommendedThreshold for PhraseID is used
			MinConsecutiveFrames: 1,
			CooldownMs:           1500,
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
			TicketTTLSec:             90,
			VoiceAgentIdleTimeoutSec: 900,
			LiveKit: ServerLiveKitConfig{
				Enabled:      false,
				URL:          "",
				APIKeyEnv:    "LIVEKIT_API_KEY",
				APISecretEnv: "LIVEKIT_API_SECRET",
				TokenTTLSec:  600,
				RoomPrefix:   "speechkit-va",
			},
			WhisperBinary: "/usr/local/bin/whisper-server",
			WhisperPort:   8180,
			ModelDir:      "/var/lib/speechkit/models",
			LogFormat:     "json",
			LogLevel:      "info",
			Features: ServerFeaturesConfig{
				Catalog:      true,
				StorageReads: true,
				Vocabulary:   true,
				TTSDirect:    true,
			},
		},
	}
	return cfg
}

func BuiltInPrimaryModelSelectionDefaults() ModelSelectionConfig {
	return ModelSelectionConfig{
		Dictate: ModeModelSelection{
			PrimaryProfileID: DefaultDictatePrimaryProfileID,
			ModeSource:       ModeSourceLocal,
		},
		Assist: ModeModelSelection{
			PrimaryProfileID: DefaultAssistPrimaryProfileID,
			ModeSource:       ModeSourceLocal,
		},
		VoiceAgent: ModeModelSelection{
			PrimaryProfileID: DefaultVoiceAgentPrimaryProfileID,
			ModeSource:       ModeSourceLocal,
		},
	}
}

func applyBuiltInPrimaryModelSelectionDefaults(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	changed := false
	changed = applyBuiltInPrimaryModelSelectionDefault(&cfg.ModelSelection.Dictate, DefaultDictatePrimaryProfileID) || changed
	changed = applyBuiltInPrimaryModelSelectionDefault(&cfg.ModelSelection.Assist, DefaultAssistPrimaryProfileID) || changed
	changed = applyBuiltInPrimaryModelSelectionDefault(&cfg.ModelSelection.VoiceAgent, DefaultVoiceAgentPrimaryProfileID) || changed
	return changed
}

func applyBuiltInPrimaryModelSelectionDefault(selection *ModeModelSelection, primaryProfileID string) bool {
	if selection == nil {
		return false
	}
	changed := false
	if strings.TrimSpace(selection.ModeSource) == "" {
		selection.ModeSource = ModeSourceLocal
		changed = true
	}
	primaryProfileID = strings.TrimSpace(primaryProfileID)
	if primaryProfileID == "" {
		return changed
	}
	if strings.TrimSpace(selection.PrimaryProfileID) != "" || strings.TrimSpace(selection.FallbackProfileID) != "" {
		return changed
	}
	selection.PrimaryProfileID = primaryProfileID
	return true
}
