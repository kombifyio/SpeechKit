package config

const (
	// HotkeyBehaviorHoldToTalk is the canonical name for the "hold the
	// shortcut while you speak, release to end" capture model. It replaces
	// the historical push_to_talk value; NormalizeHotkeyBehavior accepts the
	// legacy string as an alias so existing config files keep loading.
	HotkeyBehaviorHoldToTalk = "hold_to_talk"
	HotkeyBehaviorToggle     = "toggle"

	// legacyHotkeyBehaviorPushToTalk is the pre-rename TOML value of
	// HotkeyBehaviorHoldToTalk. NormalizeHotkeyBehavior maps it to the new
	// canonical value so older configs continue to work without a manual edit.
	legacyHotkeyBehaviorPushToTalk = "push_to_talk"

	VoiceAgentCloseBehaviorContinue = "continue"
	VoiceAgentCloseBehaviorNewChat  = "new_chat"

	// VoiceAgentBargeIn* control whether the microphone stays open while the
	// agent is speaking so the user can interrupt mid-answer (full duplex).
	VoiceAgentBargeInAuto   = "auto"
	VoiceAgentBargeInAlways = "always"
	VoiceAgentBargeInNever  = "never"

	OverlayFeedbackModeBigProductivity = "big_productivity"
	OverlayFeedbackModeSmallFeedback   = "small_feedback"

	DictationProcessingModeFinalFull      = "final_full"
	DictationProcessingModeSegmentBatch   = "segment_batch"
	DictationProcessingModeProviderStream = "provider_stream"
	DictationProcessingModeAuto           = "auto"

	AudioInputSourceMicrophone     = "microphone"
	AudioInputSourceSystemLoopback = "system_loopback"
	AudioInputSourceMicAndSystem   = "mic_and_system"

	DefaultLocalLLMBaseURL                = "http://127.0.0.1:8082/v1"
	DefaultLocalLLMModel                  = "ggml-org/gemma-4-E2B-it-GGUF:Q8_0"
	DefaultLocalSTTModel                  = "ggml-small.bin"
	DefaultLocalSTTPort                   = 9000
	DefaultDictationPauseMs               = 1500
	DefaultDictationIntermediateSegmentMs = 6000
	DefaultDictateSilenceTimeoutSec       = 3

	// ManagedDevServerURL and ManagedLiveKitURL are referenced by the
	// pre-rewrite internal/config/credentials.go ServerConnection
	// onboarding path. They are scheduled for removal together with that
	// path's in-flight rewrite; do not remove them in isolation or
	// CI will fail with "undefined: ManagedDevServerURL".
	ManagedDevServerURL = "https://speechkit.kombify.io"
	ManagedLiveKitURL   = "wss://livekit.kombify.io"

	DefaultDictatePrimaryProfileID    = "stt.local.whispercpp"
	DefaultAssistPrimaryProfileID     = "assist.builtin.gemma4-e4b"
	DefaultVoiceAgentPrimaryProfileID = "realtime.builtin.pipeline"
	// DefaultTTSPrimaryProfileID is the Voice-Output profile pre-selected for
	// fresh installs. Google Studio-O (DE) is the v0.37 recommended baseline
	// because operators that have already configured a GOOGLE_AI_API_KEY (the
	// most common cloud-AI key in this stack) get a working voice out of the
	// box; otherwise the fallback (OpenAI tts-1-hd) takes over once an
	// OpenAI key is configured.
	DefaultTTSPrimaryProfileID  = "tts.google.studio-o-de"
	DefaultTTSFallbackProfileID = "tts.openai.tts-1-hd"

	// defaultGeminiNativeAudioModel is the primary general-purpose real-time
	// audio-to-audio dialogue model. As of June 2026 this is Gemini 3.1 Flash
	// Live (preview) per
	// https://ai.google.dev/gemini-api/docs/models/gemini-3.1-flash-live-preview.
	// Google's Gemini 3.5 Live model is currently exposed as the separate
	// live-translation profile, not as the default dialogue Voice Agent.
	//
	// Note: "preview" means the model ID may change; the stable 2.5 model
	// below is kept as a same-provider fallback so deployments never break
	// when 3.1 has upstream hiccups.
	defaultGeminiNativeAudioModel = "gemini-3.1-flash-live-preview"

	// fallbackGeminiNativeAudioModel is the older same-provider Gemini Live
	// fallback when gemini-3.1-flash-live-preview is unavailable or the
	// preview endpoint returns an error.
	fallbackGeminiNativeAudioModel = "gemini-2.5-flash-native-audio-preview-12-2025"
)

type Config struct {
	General        GeneralConfig        `toml:"general"`
	Audio          AudioConfig          `toml:"audio"`
	VAD            VADConfig            `toml:"vad"`
	UI             UIConfig             `toml:"ui"`
	Vocabulary     VocabularyConfig     `toml:"vocabulary"`
	Customization  CustomizationConfig  `toml:"customization"`
	Speech         SpeechDefaultsConfig `toml:"speech"`
	Assist         AssistConfig         `toml:"assist"`
	Shortcuts      ShortcutsConfig      `toml:"shortcuts"`
	ModelSelection ModelSelectionConfig `toml:"model_selection"`

	// Output tunes how the Device-Target injects transcribed text into the
	// focused application (injection strategy, per-app paste overrides).
	// Server- and Local-Target ignore this block.
	Output OutputConfig `toml:"output"`

	// ServerConnection points the device/local-target at a remote SpeechKit
	// Server-Target. Only consulted when at least one mode in ModelSelection
	// has mode_source = "server". Disabled by default; the desktop app runs
	// fully self-contained until a user opts a mode into server-side
	// execution (typically via onboarding or settings).
	ServerConnection ServerConnectionConfig `toml:"server_connection"`

	Local           LocalConfig           `toml:"local"`
	LocalLLM        LocalLLMConfig        `toml:"local_llm"`
	VPS             VPSConfig             `toml:"vps"`
	HuggingFace     HuggingFaceConfig     `toml:"huggingface"`
	Routing         RoutingConfig         `toml:"routing"`
	Update          UpdateConfig          `toml:"update"`
	Performance     PerformanceConfig     `toml:"performance"`
	Logging         LoggingConfig         `toml:"logging"`
	Audit           AuditConfig           `toml:"audit"`
	Telemetry       TelemetryConfig       `toml:"telemetry"`
	Feedback        FeedbackConfig        `toml:"feedback"` // legacy compat; prefer Store
	Store           StoreConfig           `toml:"store"`
	Providers       ProvidersConfig       `toml:"providers"`
	ProviderOptions ProviderOptionsConfig `toml:"provider_options"`
	TTS             TTSConfig             `toml:"tts"`
	VoiceAgent      VoiceAgentConfig      `toml:"voice_agent"`

	// Server configures the standalone Linux server binary (cmd/speechkit-server).
	// All fields are optional; the desktop app (cmd/speechkit) ignores them entirely.
	Server    ServerConfig     `toml:"server"`
	Personas  []PersonaConfig  `toml:"personas"`
	Roles     []RoleConfig     `toml:"roles"`
	Sequences []SequenceConfig `toml:"sequences"`

	// HandsFree is the user-facing activation + optional voice-output layer
	// across the three strict modes. New config writes should prefer this
	// block; Wakeword remains the low-level detector compatibility block.
	HandsFree HandsFreeConfig `toml:"hands_free"`

	// Wakeword configures the always-on "Hey Quby" activation-word listener.
	// Read by cmd/speechkit (Device-Target) and any library embedder; the
	// Server-Target ignores this block in v1.
	Wakeword WakewordConfig `toml:"wakeword"`
}
