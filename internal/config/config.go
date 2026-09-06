package config

import "github.com/kombifyio/SpeechKit/pkg/speechkit/hostconfig"

const (
	// HotkeyBehaviorHoldToTalk is the canonical name for the "hold the
	// shortcut while you speak, release to end" capture model. It replaces
	// the historical push_to_talk value; NormalizeHotkeyBehavior accepts the
	// legacy string as an alias so existing config files keep loading.
	HotkeyBehaviorHoldToTalk = hostconfig.HotkeyBehaviorHoldToTalk
	HotkeyBehaviorToggle     = hostconfig.HotkeyBehaviorToggle

	VoiceAgentCloseBehaviorContinue = hostconfig.VoiceAgentCloseBehaviorContinue
	VoiceAgentCloseBehaviorNewChat  = hostconfig.VoiceAgentCloseBehaviorNewChat

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

	DictationLiveCommitImmediate = "immediate"
	DictationLiveCommitPhrase    = "phrase"
	DictationLiveCommitPassage   = "passage"

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

	// DefaultLocalLLMIdleStopMinutes pauses the bundled model server after a
	// quarter hour without a request; see LocalLLMConfig.IdleStopMinutes.
	DefaultLocalLLMIdleStopMinutes = 15

	// ManagedDevServerURL and ManagedLiveKitURL are referenced by the
	// pre-rewrite internal/config/credentials.go ServerConnection
	// onboarding path. They are scheduled for removal together with that
	// path's in-flight rewrite; do not remove them in isolation or
	// CI will fail with "undefined: ManagedDevServerURL".
	ManagedDevServerURL = "https://speechkit.kombify.io"
	ManagedLiveKitURL   = "wss://livekit.kombify.io"

	DefaultDictatePrimaryProfileID    = hostconfig.DefaultDictatePrimaryProfileID
	DefaultAssistPrimaryProfileID     = hostconfig.DefaultAssistPrimaryProfileID
	DefaultVoiceAgentPrimaryProfileID = hostconfig.DefaultVoiceAgentPrimaryProfileID
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

	// Privacy holds the central network-scope policy ("open",
	// "local_network", "device_only") enforced at every outbound network
	// boundary of the Device-Target. See internal/config/privacy.go.
	Privacy PrivacyConfig `toml:"privacy"`

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

	// AgentBridge configures the External Coding Agent Bridge (desktop-only,
	// default off, fail-closed; AI-VOICE-SPEECHKIT-TARGET.md 2026-08-10).
	// The Server-Target ignores this block entirely.
	AgentBridge AgentBridgeConfig `toml:"agent_bridge"`

	// Meeting configures meeting capture and its note write-ups. Desktop-only;
	// the Server-Target ignores this block.
	Meeting MeetingConfig `toml:"meeting"`
	Copilot CopilotConfig `toml:"copilot"`
}

// MeetingConfig configures meeting capture.
type MeetingConfig struct {
	Enabled bool `toml:"enabled"`
	// AutoDetect offers to take notes when a call starts, which SpeechKit
	// notices by seeing a calling application take the microphone. The check
	// reads process names and nothing else, stores nothing and sends nothing.
	AutoDetect bool `toml:"auto_detect"`
	// AutoDetectApps replaces the built-in list of applications whose
	// microphone use means a call. Empty uses the built-in list, which covers
	// the common clients and browsers.
	AutoDetectApps []string `toml:"auto_detect_apps"`
	// AutoEnhance writes a meeting up as soon as it ends, rather than waiting
	// to be asked.
	AutoEnhance                bool     `toml:"auto_enhance"`
	CompactOnStart             bool     `toml:"compact_on_start"`
	AlwaysOnTop                bool     `toml:"always_on_top"`
	GenerationProvider         string   `toml:"generation_provider"`
	GenerationModel            string   `toml:"generation_model"`
	FallbackPolicy             string   `toml:"fallback_policy"`
	BatchMinutes               int      `toml:"batch_minutes"`
	SummaryLanguage            string   `toml:"summary_language"`
	AdditionalSummaryLanguages []string `toml:"additional_summary_languages"`

	// Screenshot configures the Meeting Mode screenshot quick action and its
	// optional global keyboard shortcut. Captures are taken locally and stay
	// local (recording-session snapshot store): they never enter model prompts
	// and never leave the machine.
	//
	// ScreenshotEnabled toggles the quick action in the meeting UI.
	// ScreenshotHotkey is a combo string (e.g. "ctrl+alt+s"); empty falls back
	// to the default and "none" disables the shortcut.
	// ScreenshotHotkeyEnabled arms the global shortcut while a meeting is live.
	ScreenshotEnabled       bool   `toml:"screenshot_enabled"`
	ScreenshotHotkey        string `toml:"screenshot_hotkey"`
	ScreenshotHotkeyEnabled bool   `toml:"screenshot_hotkey_enabled"`
}

const CopilotTranscriptGrantVersion = 1

// CopilotConfig is desktop-only. Authentication remains in the Copilot CLI's
// operating-system credential store; SpeechKit persists only user preferences
// and the explicit cloud-processing grant for generation inputs.
type CopilotConfig struct {
	Enabled bool   `toml:"enabled"`
	Model   string `toml:"model"`
	CLIPath string `toml:"cli_path"`

	TranscriptGrantProvider  string `toml:"transcript_grant_provider"`
	TranscriptGrantVersion   int    `toml:"transcript_grant_version"`
	TranscriptGrantGrantedAt string `toml:"transcript_grant_granted_at"`
}

func (c CopilotConfig) HasTranscriptGrant() bool {
	return c.Enabled &&
		c.TranscriptGrantProvider == "github_copilot" &&
		c.TranscriptGrantVersion == CopilotTranscriptGrantVersion &&
		c.TranscriptGrantGrantedAt != ""
}
