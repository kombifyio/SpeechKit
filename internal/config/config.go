package config

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/kombifyio/SpeechKit/internal/secrets"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

var (
	dopplerLookPath              = exec.LookPath
	dopplerSecretLookup          = secrets.DefaultDopplerSecretLookup
	managedHFBuildEnabled        string
	managedDevServerBuildEnabled string
	managedHFDefaultOptIn        string
	managedDopplerDefaultProject string
	managedDopplerDefaultConfig  string
	readBuildInfo                = defaultReadBuildInfo
)

type buildInfo struct {
	MainPath string
}

const (
	HotkeyBehaviorPushToTalk = "push_to_talk"
	HotkeyBehaviorToggle     = "toggle"

	VoiceAgentCloseBehaviorContinue = "continue"
	VoiceAgentCloseBehaviorNewChat  = "new_chat"

	OverlayFeedbackModeBigProductivity = "big_productivity"
	OverlayFeedbackModeSmallFeedback   = "small_feedback"

	DefaultLocalLLMBaseURL = "http://127.0.0.1:8082/v1"
	DefaultLocalLLMModel   = "ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M"
	DefaultLocalSTTModel   = "ggml-large-v3-turbo.bin"
	ManagedDevServerURL    = "https://speechkit.kombify.dev"

	DefaultDictatePrimaryProfileID    = "stt.local.whispercpp"
	DefaultAssistPrimaryProfileID     = "assist.builtin.gemma4-e4b"
	DefaultVoiceAgentPrimaryProfileID = "realtime.builtin.pipeline"

	// defaultGeminiNativeAudioModel is the primary real-time audio-to-audio
	// model. As of April 2026 this is Gemini 3.1 Flash Live (preview) â€”
	// Google's latest native-audio model per
	// https://ai.google.dev/gemini-api/docs/models/gemini-3.1-flash-live-preview.
	//
	// Note: "preview" means the model ID may change; the stable 2.5 model
	// below is kept as a same-provider fallback so deployments never break
	// when 3.1 has upstream hiccups.
	defaultGeminiNativeAudioModel = "gemini-3.1-flash-live-preview"

	// fallbackGeminiNativeAudioModel is the last-GA Gemini Live model, used
	// as primary fallback when gemini-3.1-flash-live-preview is unavailable
	// or the preview endpoint returns an error.
	fallbackGeminiNativeAudioModel = "gemini-2.5-flash-native-audio-preview-12-2025"
)

type Config struct {
	General        GeneralConfig        `toml:"general"`
	Audio          AudioConfig          `toml:"audio"`
	UI             UIConfig             `toml:"ui"`
	Vocabulary     VocabularyConfig     `toml:"vocabulary"`
	Assist         AssistConfig         `toml:"assist"`
	Shortcuts      ShortcutsConfig      `toml:"shortcuts"`
	ModelSelection ModelSelectionConfig `toml:"model_selection"`

	// ServerConnection points the device/local-target at a remote SpeechKit
	// Server-Target. Only consulted when at least one mode in ModelSelection
	// has mode_source = "server". Disabled by default; the desktop app runs
	// fully self-contained until a user opts a mode into server-side
	// execution (typically via onboarding or settings).
	ServerConnection ServerConnectionConfig `toml:"server_connection"`

	Local       LocalConfig       `toml:"local"`
	LocalLLM    LocalLLMConfig    `toml:"local_llm"`
	VPS         VPSConfig         `toml:"vps"`
	HuggingFace HuggingFaceConfig `toml:"huggingface"`
	Routing     RoutingConfig     `toml:"routing"`
	Feedback    FeedbackConfig    `toml:"feedback"` // legacy compat; prefer Store
	Store       StoreConfig       `toml:"store"`
	Providers   ProvidersConfig   `toml:"providers"`
	TTS         TTSConfig         `toml:"tts"`
	VoiceAgent  VoiceAgentConfig  `toml:"voice_agent"`

	// Server configures the standalone Linux server binary (cmd/speechkit-server).
	// All fields are optional; the desktop app (cmd/speechkit) ignores them entirely.
	Server    ServerConfig     `toml:"server"`
	Personas  []PersonaConfig  `toml:"personas"`
	Roles     []RoleConfig     `toml:"roles"`
	Sequences []SequenceConfig `toml:"sequences"`
}

// ServerConfig configures the standalone Linux server binary. Used only by
// cmd/speechkit-server; the desktop app never reads these values.
type ServerConfig struct {
	ListenAddr            string   `toml:"listen_addr"`          // e.g. ":8080"
	PublicURL             string   `toml:"public_url"`           // external API base URL, e.g. https://speechkit.example.com/api
	Modes                 []string `toml:"modes"`                // subset of ["dictation","assist","voiceagent"]; empty = all
	AuthMode              string   `toml:"auth_mode"`            // "none" | "bearer" | "edge_hmac" | "bearer_or_edge"
	BearerTokenEnv        string   `toml:"bearer_token_env"`     // env var name holding the bearer token
	EdgeAuthSecretEnv     string   `toml:"edge_auth_secret_env"` // env var name holding the HMAC secret
	CORSAllowedOrigins    []string `toml:"cors_allowed_origins"`
	RateLimitRPS          float64  `toml:"rate_limit_rps"`
	RateLimitBurst        int      `toml:"rate_limit_burst"`
	MaxUploadMB           int      `toml:"max_upload_mb"`
	MaxVoiceAgentSessions int      `toml:"max_voiceagent_sessions"` // global cap
	MaxSessionsPerUser    int      `toml:"max_sessions_per_user"`
	TicketTTLSec          int      `toml:"ticket_ttl_sec"` // Voice Agent WS ticket TTL
	// VoiceAgentIdleTimeoutSec terminates a Voice Agent WebSocket session
	// after N seconds without any client- or provider-side activity.
	// Defaults to 900 (15 min). Set to 0 to disable the server-side idle
	// timeout (kernel-level idle handling stays in effect either way).
	VoiceAgentIdleTimeoutSec int    `toml:"voiceagent_idle_timeout_sec"`
	WhisperBinary            string `toml:"whisper_binary"` // absolute path inside container
	WhisperPort              int    `toml:"whisper_port"`   // loopback port for whisper.cpp server
	ModelDir                 string `toml:"model_dir"`      // persistent volume, e.g. /var/lib/speechkit/models
	LogFormat                string `toml:"log_format"`     // "json" | "text"
	LogLevel                 string `toml:"log_level"`      // "debug" | "info" | "warn" | "error"
}

// PersonaConfig is a TOML-seeded Voice Agent persona. DB entries with the same
// ID override the TOML seed at runtime.
type PersonaConfig struct {
	ID              string            `toml:"id"`
	DisplayName     string            `toml:"display_name"`
	Description     string            `toml:"description"`
	Voice           string            `toml:"voice"`
	Locale          string            `toml:"locale"`
	DefaultRole     string            `toml:"default_role"`
	DefaultSequence string            `toml:"default_sequence"`
	Tags            []string          `toml:"tags"`
	Metadata        map[string]string `toml:"metadata"`
}

// RoleConfig is a TOML-seeded Voice Agent role. Roles are referenced from
// Personas via ID and compose the LiveConfig prompt layers.
type RoleConfig struct {
	ID                          string   `toml:"id"`
	DisplayName                 string   `toml:"display_name"`
	SystemPrompt                string   `toml:"system_prompt"`
	RefinementPrompt            string   `toml:"refinement_prompt"`
	Locale                      string   `toml:"locale"`
	VocabularyHint              string   `toml:"vocabulary_hint"`
	ToolAllowlist               []string `toml:"tool_allowlist"`
	Temperature                 float64  `toml:"temperature"`
	ThinkingEnabled             bool     `toml:"thinking_enabled"`
	ThinkingLevel               string   `toml:"thinking_level"`
	IncludeThoughts             bool     `toml:"include_thoughts"`
	ThinkingBudget              int      `toml:"thinking_budget"`
	AutomaticActivityDetection  bool     `toml:"automatic_activity_detection"`
	VADStartSensitivity         string   `toml:"vad_start_sensitivity"`
	VADEndSensitivity           string   `toml:"vad_end_sensitivity"`
	VADPrefixPaddingMs          int      `toml:"vad_prefix_padding_ms"`
	VADSilenceDurationMs        int      `toml:"vad_silence_duration_ms"`
	ActivityHandling            string   `toml:"activity_handling"`
	TurnCoverage                string   `toml:"turn_coverage"`
	ContextCompressionEnabled   bool     `toml:"context_compression_enabled"`
	ContextCompressionTriggerTk int64    `toml:"context_compression_trigger_tokens"`
	ContextCompressionTargetTk  int64    `toml:"context_compression_target_tokens"`
	EnableAffectiveDialog       bool     `toml:"enable_affective_dialog"`
}

// SequenceConfig is a TOML-seeded multi-step Voice Agent workflow.
type SequenceConfig struct {
	ID          string               `toml:"id"`
	DisplayName string               `toml:"display_name"`
	Description string               `toml:"description"`
	Completion  string               `toml:"completion"` // "all_steps" | "explicit_close" | "max_turns"
	MaxTurns    int                  `toml:"max_turns"`
	Steps       []SequenceStepConfig `toml:"steps"`
}

// SequenceStepConfig is a single step inside a SequenceConfig.
type SequenceStepConfig struct {
	ID           string   `toml:"id"`
	Instruction  string   `toml:"instruction"`
	ExitCriteria string   `toml:"exit_criteria"`
	RequireTools []string `toml:"require_tools"`
	MaxTurns     int      `toml:"max_turns"`
}

type StoreConfig struct {
	Backend            string `toml:"backend"` // "sqlite" | "postgres" | registered name
	SQLitePath         string `toml:"sqlite_path"`
	PostgresDSN        string `toml:"postgres_dsn"`
	SaveAudio          bool   `toml:"save_audio"`
	AudioRetentionDays int    `toml:"audio_retention_days"`
	MaxAudioStorageMB  int    `toml:"max_audio_storage_mb"`
}

type GeneralConfig struct {
	Language                 string `toml:"language"`
	Hotkey                   string `toml:"hotkey"` // Deprecated: legacy single-hotkey field kept for config file compat. Use DictateHotkey.
	DictateHotkey            string `toml:"dictate_hotkey"`
	AssistHotkey             string `toml:"assist_hotkey"`
	VoiceAgentHotkey         string `toml:"voice_agent_hotkey"`
	DictateHotkeyBehavior    string `toml:"dictate_hotkey_behavior"`
	AssistHotkeyBehavior     string `toml:"assist_hotkey_behavior"`
	VoiceAgentHotkeyBehavior string `toml:"voice_agent_hotkey_behavior"`
	DictateEnabled           bool   `toml:"dictate_enabled"`
	AssistEnabled            bool   `toml:"assist_enabled"`
	VoiceAgentEnabled        bool   `toml:"voice_agent_enabled"`
	AutoStartOnLaunch        bool   `toml:"auto_start_on_launch"`
	AgentHotkey              string `toml:"agent_hotkey"`
	AgentMode                string `toml:"agent_mode"`  // "assist" or "voice_agent" â€” determines what agent_hotkey triggers
	ActiveMode               string `toml:"active_mode"` // legacy compat
	HotkeyMode               string `toml:"hotkey_mode"` // legacy compat for single behavior setting
	AutoStopSilenceMs        int    `toml:"auto_stop_silence_ms"`
	FastModeSilenceMs        int    `toml:"fast_mode_silence_ms"` // silence threshold for Quick Capture auto-stop
	ModelDownloadDir         string `toml:"model_download_dir"`   // Default directory for downloaded local model files
}

type AudioConfig struct {
	Backend        string `toml:"backend"`
	DeviceID       string `toml:"device_id"`
	OutputDeviceID string `toml:"output_device_id"`
	SampleRate     int    `toml:"sample_rate"`
	Channels       int    `toml:"channels"`
	FrameSizeMs    int    `toml:"frame_size_ms"`
	LatencyHint    string `toml:"latency_hint"`
}

type VocabularyConfig struct {
	Dictionary string `toml:"dictionary"`
}

type AssistConfig struct {
	EnabledTools []string `toml:"enabled_tools"`
}

type ShortcutsConfig struct {
	Locale map[string]ShortcutLocaleConfig `toml:"locale"`
}

type ShortcutLocaleConfig struct {
	LeadingFillers []string `toml:"leading_fillers"`
	CopyLast       []string `toml:"copy_last"`
	InsertLast     []string `toml:"insert_last"`
	Summarize      []string `toml:"summarize"`
	QuickNote      []string `toml:"quick_note"`
}

type ModelSelectionConfig struct {
	Dictate    ModeModelSelection `toml:"dictate"`
	Assist     ModeModelSelection `toml:"assist"`
	VoiceAgent ModeModelSelection `toml:"voice_agent"`
}

// Mode source values for ModeModelSelection.ModeSource. "local" means the
// desktop app runs the mode against the in-process Framework kernel (default,
// preserves all pre-0.26 behaviour). "server" routes the mode through
// ServerConnection to a remote speechkit-server.
const (
	ModeSourceLocal  = "local"
	ModeSourceServer = "server"
)

type ModeModelSelection struct {
	PrimaryProfileID  string `toml:"primary_profile_id"`
	FallbackProfileID string `toml:"fallback_profile_id"`

	// ModeSource selects whether this mode runs locally (Framework kernel
	// in-process, default) or against a remote SpeechKit Server-Target
	// configured under [server_connection]. Empty string is treated as
	// ModeSourceLocal so existing configs keep behaving as before.
	ModeSource string `toml:"mode_source"`
}

// ServerConnectionConfig describes how the device/local-target reaches a
// remote SpeechKit server. Read by cmd/speechkit (and any embedded library
// caller) when a ModeModelSelection has mode_source = "server"; the
// Server-Target itself ignores this section.
type ServerConnectionConfig struct {
	// Enabled gates the entire server connection. When false, every mode is
	// forced to run locally regardless of its mode_source. Lets users keep
	// their server URL in config but temporarily flip back to fully local.
	Enabled bool `toml:"enabled"`

	// URL is the base URL of the speechkit-server, e.g.
	// "https://speechkit.example.com" or "http://localhost:8080".
	URL string `toml:"url"`

	// BearerTokenEnv names the env var that holds the bearer token sent in
	// the Authorization header. Defaults to SPEECHKIT_SERVER_TOKEN. The
	// value is never read from the TOML file itself â€” only the env var name
	// is configured here.
	BearerTokenEnv string `toml:"bearer_token_env"`

	// FallbackToLocal makes the device app fall back to the in-process
	// Framework kernel if a server call fails or the server is unreachable.
	// Useful for laptop deployments that may be offline; should be false
	// for kiosks that must never silently downgrade to local processing.
	FallbackToLocal bool `toml:"fallback_to_local"`

	// RequestTimeoutSec caps non-streaming HTTP calls (Dictation, Assist).
	// 0 means no explicit timeout (the underlying http.Client default
	// applies). Voice Agent WebSocket sessions are not affected.
	RequestTimeoutSec int `toml:"request_timeout_sec"`
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

// ResolvedModeSource returns the effective ModeSource for this mode,
// normalising the empty default to ModeSourceLocal. Use this everywhere
// instead of reading sel.ModeSource directly so a missing TOML field does
// not silently mean "server".
func (sel ModeModelSelection) ResolvedModeSource() string {
	switch strings.TrimSpace(strings.ToLower(sel.ModeSource)) {
	case ModeSourceServer:
		return ModeSourceServer
	default:
		return ModeSourceLocal
	}
}

type UIConfig struct {
	OverlayEnabled          bool                           `toml:"overlay_enabled"`
	OverlayPosition         string                         `toml:"overlay_position"` // "top", "bottom", "left", "right"
	OverlayMovable          bool                           `toml:"overlay_movable"`
	OverlayFreeX            int                            `toml:"overlay_free_x"`
	OverlayFreeY            int                            `toml:"overlay_free_y"`
	OverlayMonitorPositions map[string]OverlayFreePosition `toml:"overlay_monitor_positions"`
	Visualizer              string                         `toml:"visualizer"`
	Design                  string                         `toml:"design"`
	AssistOverlayMode       string                         `toml:"assist_overlay_mode"`
	VoiceAgentOverlayMode   string                         `toml:"voice_agent_overlay_mode"`
}

type OverlayFreePosition struct {
	X int `toml:"x"`
	Y int `toml:"y"`
}

type LocalConfig struct {
	Enabled   bool   `toml:"enabled"`
	Model     string `toml:"model"`
	ModelPath string `toml:"model_path"`
	Port      int    `toml:"port"`
	GPU       string `toml:"gpu"`
}

type LocalLLMConfig struct {
	Enabled      bool   `toml:"enabled"`
	BaseURL      string `toml:"base_url"`
	Model        string `toml:"model"`
	ModelPath    string `toml:"model_path"`
	Port         int    `toml:"port"`
	GPU          string `toml:"gpu"`
	UtilityModel string `toml:"utility_model"`
	AssistModel  string `toml:"assist_model"`
	AgentModel   string `toml:"agent_model"`
}

type VPSConfig struct {
	Enabled   bool   `toml:"enabled"`
	URL       string `toml:"url"`
	Model     string `toml:"model"`
	APIKeyEnv string `toml:"api_key_env"`
}

type HuggingFaceConfig struct {
	Enabled      bool   `toml:"enabled"`
	Model        string `toml:"model"`
	UtilityModel string `toml:"utility_model"`
	AssistModel  string `toml:"assist_model"`
	AgentModel   string `toml:"agent_model"`
	TokenEnv     string `toml:"token_env"`
}

type RoutingConfig struct {
	Strategy                string  `toml:"strategy"`
	PreferLocalUnderSeconds float64 `toml:"prefer_local_under_seconds"`
	ParallelCloud           bool    `toml:"parallel_cloud"`
	ReplaceOnBetter         bool    `toml:"replace_on_better"`
}

type FeedbackConfig struct {
	SaveAudio          bool   `toml:"save_audio"`
	AudioRetentionDays int    `toml:"audio_retention_days"`
	DBPath             string `toml:"db_path"`
	MaxAudioStorageMB  int    `toml:"max_audio_storage_mb"`
}

// ProvidersConfig groups all external provider configurations.
type ProvidersConfig struct {
	OpenAI     OpenAIProviderConfig     `toml:"openai"`
	Groq       GroqProviderConfig       `toml:"groq"`
	Google     GoogleProviderConfig     `toml:"google"`
	Ollama     OllamaProviderConfig     `toml:"ollama"`
	OpenRouter OpenRouterProviderConfig `toml:"openrouter"`
}

type OpenAIProviderConfig struct {
	Enabled       bool   `toml:"enabled"`
	APIKeyEnv     string `toml:"api_key_env"`
	STTModel      string `toml:"stt_model"`
	UtilityModel  string `toml:"utility_model"`
	AssistModel   string `toml:"assist_model"`
	AgentModel    string `toml:"agent_model"`
	TTSModel      string `toml:"tts_model"`
	TTSVoice      string `toml:"tts_voice"`
	RealtimeModel string `toml:"realtime_model"`
}

type GroqProviderConfig struct {
	Enabled      bool   `toml:"enabled"`
	APIKeyEnv    string `toml:"api_key_env"`
	STTModel     string `toml:"stt_model"`
	UtilityModel string `toml:"utility_model"`
	AssistModel  string `toml:"assist_model"`
	AgentModel   string `toml:"agent_model"`
}

type GoogleProviderConfig struct {
	Enabled      bool   `toml:"enabled"`
	APIKeyEnv    string `toml:"api_key_env"`
	STTModel     string `toml:"stt_model"`
	UtilityModel string `toml:"utility_model"`
	AssistModel  string `toml:"assist_model"`
	AgentModel   string `toml:"agent_model"`
}

type OllamaProviderConfig struct {
	Enabled      bool   `toml:"enabled"`
	BaseURL      string `toml:"base_url"`
	STTModel     string `toml:"stt_model"`
	UtilityModel string `toml:"utility_model"`
	AssistModel  string `toml:"assist_model"`
	AgentModel   string `toml:"agent_model"`
}

type OpenRouterProviderConfig struct {
	Enabled      bool   `toml:"enabled"`
	APIKeyEnv    string `toml:"api_key_env"`
	STTModel     string `toml:"stt_model"`
	UtilityModel string `toml:"utility_model"`
	AssistModel  string `toml:"assist_model"`
	AgentModel   string `toml:"agent_model"`
}

// TTSConfig configures text-to-speech for Assist Mode.
type TTSConfig struct {
	Enabled     bool           `toml:"enabled"`
	Strategy    string         `toml:"strategy"` // "cloud-first", "local-first", "cloud-only", "local-only"
	Voice       string         `toml:"voice"`    // Global default voice override
	Speed       float64        `toml:"speed"`    // Global speed 0.25-4.0, default 1.0
	Format      string         `toml:"format"`   // "mp3", "wav", "opus", "pcm"
	OpenAI      TTSOpenAI      `toml:"openai"`
	Google      TTSGoogle      `toml:"google"`
	HuggingFace TTSHuggingFace `toml:"huggingface"`
	Local       TTSLocal       `toml:"local"`
}

type TTSOpenAI struct {
	Enabled bool   `toml:"enabled"`
	Model   string `toml:"model"` // "tts-1" or "tts-1-hd"
	Voice   string `toml:"voice"` // alloy, echo, fable, onyx, nova, shimmer
}

type TTSGoogle struct {
	Enabled bool   `toml:"enabled"`
	Voice   string `toml:"voice"` // e.g. "de-DE-Neural2-B"
}

type TTSHuggingFace struct {
	Enabled bool   `toml:"enabled"`
	Model   string `toml:"model"` // e.g. "parler-tts/parler-tts-mini-multilingual-v1.1"
}

type TTSLocal struct {
	Enabled   bool   `toml:"enabled"`
	Model     string `toml:"model"`
	ModelPath string `toml:"model_path"`
	Port      int    `toml:"port"`
}

// VoiceAgentConfig configures the real-time Voice Agent Mode.
type VoiceAgentConfig struct {
	Enabled bool `toml:"enabled"`
	// Provider selects the backend that drives a Voice Agent session.
	// Supported values:
	//   ""          (default) â€” same as "gemini"
	//   "gemini"    â€” Google Gemini Live (cloud, GOOGLE_AI_API_KEY required)
	//   "cascaded"  â€” self-hosted whisper.cpp â†’ Genkit agent LLM â†’ TTS pipeline
	//                 (CPU-capable; no external realtime dependency)
	//   "moshi"     â€” self-hosted Kyutai Moshi Rust server (GPU required, M9b)
	//
	// The Server-Target reads this field via cmd/speechkit-server; the Device-
	// Target currently always uses "gemini" and ignores it.
	Provider                        string `toml:"provider"`
	Model                           string `toml:"model"`             // Real-time model ID (e.g. "gemini-3.1-flash-live-preview")
	FallbackModel                   string `toml:"fallback_model"`    // Fallback real-time model
	Voice                           string `toml:"voice"`             // Voice name for real-time model
	AgentProfileID                  string `toml:"agent_profile_id"`  // Built-in Voice Agent profile ID; "default" preserves current behavior.
	AgentSequenceID                 string `toml:"agent_sequence_id"` // Optional workflow sequence ID; empty uses the selected persona default.
	FrameworkPrompt                 string `toml:"framework_prompt"`  // Durable host/framework instruction that defines the Voice Agent behavior
	RefinementPrompt                string `toml:"refinement_prompt"` // User-specific refinement appended to the framework prompt
	Instruction                     string `toml:"instruction"`       // Legacy alias for FrameworkPrompt
	AutoStartOnLaunch               bool   `toml:"auto_start_on_launch"`
	CloseBehavior                   string `toml:"close_behavior"` // "continue" keeps the conversation window in the taskbar; "new_chat" ends the current chat on close
	ReminderAfterIdleSec            int    `toml:"reminder_after_idle_sec"`
	DeactivateAfterIdleSec          int    `toml:"deactivate_after_idle_sec"`
	PipelineFallback                bool   `toml:"pipeline_fallback"` // Use STT -> Agent LLM -> optional TTS when the selected Voice Agent profile is not native realtime.
	ShowPrompter                    bool   `toml:"show_prompter"`     // Show live transcript prompter window
	EnableSessionSummary            bool   `toml:"enable_session_summary"`
	EnableInputTranscript           bool   `toml:"enable_input_transcript"`
	EnableOutputTranscript          bool   `toml:"enable_output_transcript"`
	EnableAffectiveDialog           bool   `toml:"enable_affective_dialog"`
	ThinkingEnabled                 bool   `toml:"thinking_enabled"`
	IncludeThoughts                 bool   `toml:"include_thoughts"`
	ThinkingBudget                  int    `toml:"thinking_budget"`
	ThinkingLevel                   string `toml:"thinking_level"`
	ContextCompressionEnabled       bool   `toml:"context_compression_enabled"`
	ContextCompressionTriggerTokens int64  `toml:"context_compression_trigger_tokens"`
	ContextCompressionTargetTokens  int64  `toml:"context_compression_target_tokens"`
	AutomaticActivityDetection      bool   `toml:"automatic_activity_detection"`
	ActivityHandling                string `toml:"activity_handling"`
	TurnCoverage                    string `toml:"turn_coverage"`
	VADStartSensitivity             string `toml:"vad_start_sensitivity"`
	VADEndSensitivity               string `toml:"vad_end_sensitivity"`
	VADPrefixPaddingMs              int    `toml:"vad_prefix_padding_ms"`
	VADSilenceDurationMs            int    `toml:"vad_silence_duration_ms"`
}

// Load reads config from the given path. Falls back to defaults if file not found.
func Load(path string) (*Config, error) {
	cfg := defaults()

	if path == "" {
		path = defaultConfigPath()
	}

	data, err := os.ReadFile(path) // #nosec G304 -- path is the application config path supplied by startup/config plumbing.
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	// Warn (but do not block) when the config file is accessible to group or
	// other users on POSIX systems. No-op on Windows where Go's os.FileMode
	// does not reflect NTFS ACLs.
	if warning, permErr := checkConfigFilePermissions(path); permErr == nil && warning != "" {
		slog.Warn("config file permissions are loose", "msg", warning)
	}

	meta, err := toml.Decode(string(data), cfg)
	if err != nil {
		slog.Warn("malformed config.toml, using defaults", "err", err)
		return defaults(), nil
	}

	// Bridge legacy [feedback] to [store] if store.backend is not explicitly set.
	if cfg.Store.Backend == "" || cfg.Store.Backend == "sqlite" {
		if cfg.Feedback.DBPath != "" && !meta.IsDefined("store", "sqlite_path") && cfg.Store.SQLitePath == "" {
			cfg.Store.SQLitePath = cfg.Feedback.DBPath
		}
		if cfg.Feedback.MaxAudioStorageMB > 0 && !meta.IsDefined("store", "max_audio_storage_mb") && cfg.Store.MaxAudioStorageMB == 0 {
			cfg.Store.MaxAudioStorageMB = cfg.Feedback.MaxAudioStorageMB
		}
		if cfg.Feedback.AudioRetentionDays > 0 && !meta.IsDefined("store", "audio_retention_days") && cfg.Store.AudioRetentionDays == 0 {
			cfg.Store.AudioRetentionDays = cfg.Feedback.AudioRetentionDays
		}
		if !meta.IsDefined("store", "save_audio") {
			cfg.Store.SaveAudio = cfg.Feedback.SaveAudio
		}
	}

	backfillLegacyAssistModels(meta, cfg)
	backfillLegacyModeHotkeys(meta, cfg)
	backfillStartupBehavior(meta, cfg)
	backfillVoiceAgentPromptLayers(meta, cfg)
	backfillVoiceAgentSessionSummary(meta, cfg)
	cfg.VoiceAgent.AgentProfileID = voiceagentprofile.NormalizeID(cfg.VoiceAgent.AgentProfileID)
	cfg.VoiceAgent.AgentSequenceID = strings.TrimSpace(cfg.VoiceAgent.AgentSequenceID)
	cfg.VoiceAgent.CloseBehavior = NormalizeVoiceAgentCloseBehavior(
		cfg.VoiceAgent.CloseBehavior,
		VoiceAgentCloseBehaviorContinue,
	)
	cfg.UI.AssistOverlayMode = NormalizeOverlayFeedbackMode(
		cfg.UI.AssistOverlayMode,
		OverlayFeedbackModeSmallFeedback,
	)
	cfg.UI.VoiceAgentOverlayMode = NormalizeOverlayFeedbackMode(
		cfg.UI.VoiceAgentOverlayMode,
		OverlayFeedbackModeSmallFeedback,
	)
	ApplyManagedDevServerDefaults(cfg)

	return cfg, nil
}

func Save(path string, cfg *Config) error {
	if path == "" {
		path = defaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	file, err := os.Create(path) // #nosec G304 -- path is the application config path supplied by startup/config plumbing.
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	defer file.Close() //nolint:errcheck // file close on write, error not actionable after encode

	if err := toml.NewEncoder(file).Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	return nil
}

func defaultConfigPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "config.toml")
}

func (cfg *Config) LegacyAgentHotkey() string {
	if cfg == nil {
		return ""
	}
	return cfg.General.LegacyAgentHotkey()
}

func (g GeneralConfig) LegacyAgentHotkey() string {
	if strings.TrimSpace(g.AgentHotkey) != "" {
		return strings.TrimSpace(g.AgentHotkey)
	}
	if strings.TrimSpace(g.AgentMode) == "voice_agent" {
		return strings.TrimSpace(g.VoiceAgentHotkey)
	}
	return strings.TrimSpace(g.AssistHotkey)
}

// ResolveSecret resolves a secret by name. Checks environment first, then Doppler CLI
// using either explicit DOPPLER_PROJECT/DOPPLER_CONFIG env vars or build-embedded
// managed Doppler defaults.
func ResolveSecret(envName string) string {
	if strings.TrimSpace(envName) == "" {
		return ""
	}
	value, _, err := secrets.ResolveNamedSecret(envName, func() string {
		return ResolveSecretFromEnvironmentOrDoppler(envName)
	})
	if err == nil && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return ResolveSecretFromEnvironmentOrDoppler(envName)
}

func ResolveSecretFromEnvironmentOrDoppler(envName string) string {
	if v := os.Getenv(envName); v != "" {
		return v
	}
	return dopplerGet(envName)
}

func HuggingFaceTokenEnvName(cfg *Config) string {
	if cfg == nil {
		return "HF_TOKEN"
	}
	if tokenEnv := strings.TrimSpace(cfg.HuggingFace.TokenEnv); tokenEnv != "" {
		return tokenEnv
	}
	return "HF_TOKEN"
}

func HuggingFaceTokenStatus(cfg *Config) (secrets.TokenStatus, error) {
	tokenEnv := HuggingFaceTokenEnvName(cfg)
	return secrets.HuggingFaceTokenStatus(func() string {
		return ResolveSecretFromEnvironmentOrDoppler(tokenEnv)
	})
}

func ResolveHuggingFaceToken(cfg *Config) (string, secrets.TokenStatus, error) {
	tokenEnv := HuggingFaceTokenEnvName(cfg)
	return secrets.ResolveHuggingFaceToken(func() string {
		return ResolveSecretFromEnvironmentOrDoppler(tokenEnv)
	})
}

// dopplerGet tries to resolve a secret via `doppler secrets get` CLI.
func dopplerGet(key string) string {
	dopplerPath := findDopplerExecutable()
	if dopplerPath == "" {
		return ""
	}

	projects := dopplerProjects()
	configs := dopplerConfigs()
	if len(projects) == 0 || len(configs) == 0 {
		return ""
	}

	for _, project := range projects {
		for _, cfg := range configs {
			v, err := dopplerSecretLookup(dopplerPath, key, project, cfg)
			if err == nil && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func findDopplerExecutable() string {
	return secrets.FindDopplerExecutable(dopplerLookPath)
}

func dopplerProjects() []string {
	if rawProject, ok := os.LookupEnv("DOPPLER_PROJECT"); ok {
		if project := strings.TrimSpace(rawProject); project != "" {
			return []string{project}
		}
		return nil
	}
	if project := strings.TrimSpace(managedDopplerDefaultProject); project != "" {
		return []string{project}
	}
	return nil
}

func dopplerConfigs() []string {
	if rawConfig, ok := os.LookupEnv("DOPPLER_CONFIG"); ok {
		if cfg := strings.TrimSpace(rawConfig); cfg != "" {
			return []string{cfg}
		}
		return nil
	}
	if cfg := strings.TrimSpace(managedDopplerDefaultConfig); cfg != "" {
		return []string{cfg}
	}
	return nil
}

func resetDopplerHooksForTests() {
	dopplerLookPath = exec.LookPath
	dopplerSecretLookup = secrets.DefaultDopplerSecretLookup
}

func ApplyManagedIntegrationDefaults(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	if !ManagedHuggingFaceAvailableInBuild() {
		cfg.HuggingFace.Enabled = false
		return false
	}

	if !managedHFOptInEnabled() {
		return false
	}

	if cfg.HuggingFace.Enabled || cfg.VPS.Enabled || cfg.Local.Enabled {
		return false
	}

	if cfg.Routing.Strategy != "cloud-only" {
		return false
	}

	tokenEnv := HuggingFaceTokenEnvName(cfg)
	cfg.HuggingFace.TokenEnv = tokenEnv

	token, _, err := ResolveHuggingFaceToken(cfg)
	if err != nil || token == "" {
		return false
	}

	cfg.HuggingFace.Enabled = true
	if strings.TrimSpace(cfg.HuggingFace.Model) == "" {
		cfg.HuggingFace.Model = "openai/whisper-large-v3"
	}
	return true
}

func ApplyManagedDevServerDefaults(cfg *Config) bool {
	if cfg == nil || !ManagedDevServerAvailableInBuild() {
		return false
	}

	changed := false
	if strings.TrimSpace(cfg.ServerConnection.URL) == "" {
		cfg.ServerConnection.URL = ManagedDevServerURL
		changed = true
	}
	if strings.TrimSpace(cfg.ServerConnection.BearerTokenEnv) == "" {
		cfg.ServerConnection.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"
		changed = true
	}
	if cfg.ServerConnection.RequestTimeoutSec <= 0 {
		cfg.ServerConnection.RequestTimeoutSec = 30
		changed = true
	}
	return changed
}

func managedHFOptInEnabled() bool {
	if raw, ok := os.LookupEnv("SPEECHKIT_ENABLE_MANAGED_HF"); ok {
		return parseManagedBool(raw)
	}
	if strings.TrimSpace(managedHFDefaultOptIn) != "" {
		return parseManagedBool(managedHFDefaultOptIn)
	}
	return defaultManagedHuggingFaceForModule()
}

func ManagedHuggingFaceAvailableInBuild() bool {
	if strings.TrimSpace(managedHFBuildEnabled) != "" {
		return parseManagedBool(managedHFBuildEnabled)
	}
	return defaultManagedHuggingFaceForModule()
}

func ManagedDevServerAvailableInBuild() bool {
	if strings.TrimSpace(managedDevServerBuildEnabled) != "" {
		return parseManagedBool(managedDevServerBuildEnabled)
	}
	return defaultManagedPrivateFeatureForModule()
}

func OverrideManagedHuggingFaceBuildForTests(value string) func() {
	previous := managedHFBuildEnabled
	managedHFBuildEnabled = value
	return func() {
		managedHFBuildEnabled = previous
	}
}

func OverrideManagedDevServerBuildForTests(value string) func() {
	previous := managedDevServerBuildEnabled
	managedDevServerBuildEnabled = value
	return func() {
		managedDevServerBuildEnabled = previous
	}
}

func parseManagedBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func defaultReadBuildInfo() (buildInfo, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return buildInfo{}, false
	}
	return buildInfo{MainPath: strings.TrimSpace(info.Main.Path)}, true
}

func defaultManagedHuggingFaceForModule() bool {
	return defaultManagedPrivateFeatureForModule()
}

func defaultManagedPrivateFeatureForModule() bool {
	info, ok := readBuildInfo()
	if !ok {
		return false
	}
	mainPath := strings.TrimSpace(info.MainPath)
	if mainPath == privateModulePath() {
		return true
	}
	if mainPath == "github.com/kombifyio/SpeechKit" {
		return false
	}
	return false
}

func privateModulePath() string {
	return "github.com/" + "Soulcreek" + "/kombify-SpeechKit"
}
