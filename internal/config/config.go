package config

import "strings"

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

	OverlayFeedbackModeBigProductivity = "big_productivity"
	OverlayFeedbackModeSmallFeedback   = "small_feedback"

	DefaultLocalLLMBaseURL = "http://127.0.0.1:8082/v1"
	DefaultLocalLLMModel   = "ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M"
	DefaultLocalSTTModel   = "ggml-small.bin"
	DefaultLocalSTTPort    = 9000

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

	// defaultGeminiNativeAudioModel is the primary real-time audio-to-audio
	// model. As of April 2026 this is Gemini 3.1 Flash Live (preview) —
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
	Update      UpdateConfig      `toml:"update"`
	Logging     LoggingConfig     `toml:"logging"`
	Audit       AuditConfig       `toml:"audit"`
	Telemetry   TelemetryConfig   `toml:"telemetry"`
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

	// Wakeword configures the always-on "Hey Quby" activation-word listener.
	// Read by cmd/speechkit (Device-Target) and any library embedder; the
	// Server-Target ignores this block in v1.
	Wakeword WakewordConfig `toml:"wakeword"`
}

// Wake-word default-mode values for WakewordConfig.DefaultMode.
const (
	WakewordDefaultModeDictate    = "dictate"
	WakewordDefaultModeAssist     = "assist"
	WakewordDefaultModeVoiceAgent = "voice_agent"
)

// Wake-word backend values for WakewordConfig.Backend.
const (
	WakewordBackendSherpaKWS           = "sherpa_kws"
	WakewordBackendLiveKitOpenWakeWord = "livekit_openwakeword"
	WakewordBackendSTTPhrase           = "stt_phrase"
)

// WakewordConfig configures the always-on activation-word listener.
//
// When Enabled is true the Device-Target opens a dedicated low-volume audio
// session that continuously feeds the wake-word detector. A successful
// detection synthesises a key-down event on DefaultMode's hotkey binding,
// which the existing mode dispatcher treats identically to a real hotkey
// press. Audio for wake detection NEVER leaves the device.
type WakewordConfig struct {
	// Enabled gates the entire feature. Default false (opt-in).
	Enabled bool `toml:"enabled"`

	// Backend selects the local detector implementation. The Windows app
	// ships Sherpa-ONNX KWS, LiveKit/openWakeWord ONNX, and STT phrase-match
	// as explicit selectable paths so test builds can compare detector
	// behaviour without silently falling back to a different implementation.
	Backend string `toml:"backend"`

	// PhraseID picks one of SpeechKit's curated wake phrases from
	// wakeword.DefaultCatalog (e.g. "hey_quby", "hey_computer",
	// "hey_jarvis", "hey_mira"). When set, the corresponding ONNX file is
	// resolved automatically and Phrase/ModelPath below are ignored. When
	// empty, the explicit Phrase + ModelPath fields are used instead
	// (custom phrase mode). Switching via this field is how users pick a
	// different wake phrase in settings without editing paths by hand.
	PhraseID string `toml:"phrase_id"`

	// Phrase is the display label of the trained wake phrase, surfaced in
	// the tray and status feed. It has NO effect on detection — the ONNX
	// model encodes the actual phrase(s). One model can be trained to
	// fire on multiple pronunciation variants (e.g. "Hey Cubi" and
	// "Hey Kubi" for the same brand "Quby") via target_phrases in the
	// training yaml; the display label here remains a single brand string.
	//
	// Ignored when PhraseID matches a catalog entry (the catalog's
	// DisplayName is used instead).
	Phrase string `toml:"phrase"`

	// ModelPath is the path to the trained phrase prediction model (.onnx).
	// Empty resolves to <data_dir>/models/wakeword/hey_quby.onnx at runtime.
	//
	// Ignored when PhraseID matches a catalog entry (the catalog's
	// FileName is resolved inside the wake-word models directory).
	ModelPath string `toml:"model_path"`

	// MelspecModelPath and EmbeddingModelPath point at the shared
	// openWakeWord upstream models. Empty values resolve to the same
	// directory as ModelPath with canonical filenames.
	MelspecModelPath   string `toml:"melspec_model_path"`
	EmbeddingModelPath string `toml:"embedding_model_path"`

	// DefaultMode is the runtime mode triggered when the wake phrase fires.
	// One of "dictate" | "assist" | "voice_agent". Defaults to voice_agent.
	DefaultMode string `toml:"default_mode"`

	// Threshold is the minimum probability to count a frame as a hit.
	// Range (0.0, 1.0]. Backend-specific defaults when this is 0:
	//   - LiveKit/openWakeWord: 0.5 (Wyoming/openWakeWord canonical)
	//   - Sherpa-onnx KWS: 0.25 (sherpa-onnx upstream default)
	//   - STT phrase match: 0 (substring match, no acoustic probability)
	// Use the in-app "Test wake word" self-test in Settings to calibrate
	// for your specific microphone + environment instead of guessing.
	Threshold float64 `toml:"threshold"`

	// MinConsecutiveFrames is the number of consecutive above-threshold
	// frames required before a trigger fires. Higher = fewer false-accepts,
	// more false-rejects. Defaults to 2.
	MinConsecutiveFrames int `toml:"min_consecutive_frames"`

	// CooldownMs is the minimum gap between two triggers, in milliseconds.
	// Defaults to 1500ms.
	CooldownMs int `toml:"cooldown_ms"`

	// DebugMode enables verbose detector diagnostics. When true the sidecars
	// emit per-decode score events (openWakeWord) or set the sherpa-onnx
	// ModelConfig.Debug flag (Sherpa KWS), and the host adapter forwards
	// those signals into the user-visible log feed. Default false — only flip
	// on while tuning a wake phrase, the score event stream is high-volume.
	DebugMode bool `toml:"debug_mode"`

	// AutoEnd controls the framework-level auto-end policy applied to any
	// session that the wake-word triggered. Wake-word-origin Voice-Agent
	// activations terminate automatically on silence after this many
	// seconds, or when the user utters one of the configured exit
	// phrases. Empty values fall back to wakeword.DefaultAutoEndConfig
	// (10s silence + DE/EN exit phrases) so a TOML without an [auto_end]
	// block still gets the framework baseline. There is intentionally no
	// hard-cap on session duration — Voice-Agent is designed for
	// multi-hour dialogs and a forced cap would break regular use.
	AutoEnd WakewordAutoEndConfig `toml:"auto_end"`

	// TrainingData controls the optional activation-capture pipeline:
	// the sidecar saves the surrounding audio of each detection to a
	// local directory and, when explicitly opted-in, uploads those
	// clips to a SpeechKit-Server for training-data collection. ALL
	// fields default to OFF — see docs/wakeword-training-data.md.
	TrainingData WakewordTrainingDataConfig `toml:"training_data"`
}

// WakewordTrainingDataConfig governs the v0.37.4+ activation-capture
// pipeline. All booleans default to false so the feature has no effect
// unless the user explicitly opts in. The full privacy contract lives
// in docs/wakeword-training-data.md.
type WakewordTrainingDataConfig struct {
	// LocalCaptureEnabled toggles the sidecar ring-buffer + WAV
	// writer that persists each detection's audio to LocalCaptureDir.
	// Default false. Even when true, no network traffic happens
	// unless UploadEnabled is ALSO true.
	LocalCaptureEnabled bool `toml:"local_capture_enabled"`

	// LocalCaptureDir is the filesystem root that holds the captured
	// WAV + JSON pairs. Empty resolves to
	// %LOCALAPPDATA%/SpeechKit/wakeword-activations on Windows.
	LocalCaptureDir string `toml:"local_capture_dir"`

	// LocalMaxFiles caps how many activation pairs live on disk
	// before the oldest get deleted by the rotation worker. Default
	// 500. Set to 0 for unlimited (the retention_days limit still
	// applies).
	LocalMaxFiles int `toml:"local_max_files"`

	// LocalRetentionDays auto-deletes activation pairs older than
	// this many days at sidecar startup and every hour after. Zero
	// means "do not auto-delete by age" (LocalMaxFiles still
	// applies). Default 30.
	LocalRetentionDays int `toml:"local_retention_days"`

	// PreRollMs is the duration of audio captured BEFORE the
	// detection trigger. The sidecar keeps this many milliseconds
	// of PCM in a ring buffer so the moment the detection fires it
	// can write the leading audio that contains the actual wake
	// phrase. Default 1500 (matches the wake-phrase windows the
	// existing detectors are tuned against).
	PreRollMs int `toml:"pre_roll_ms"`

	// PostRollMs is the duration of audio captured AFTER the
	// detection trigger. Useful for catching the speaker continuing
	// past the wake phrase so a labeler can hear whether the
	// utterance was a true positive or noise. Default 500.
	PostRollMs int `toml:"post_roll_ms"`

	// UploadEnabled toggles the background uploader that pushes
	// captured clips to UploadServerURL. Requires both
	// LocalCaptureEnabled=true and UploadServerURL+UploadTokenEnv
	// to be set. Default false.
	UploadEnabled bool `toml:"upload_enabled"`

	// UploadServerURL is the base URL of the SpeechKit-Server that
	// accepts POST /v1/wakeword/activations. Empty falls back to
	// [server_connection].url when set.
	UploadServerURL string `toml:"upload_server_url"`

	// UploadTokenEnv names the env var (resolved via
	// internal/secrets) that holds the bearer token used by the
	// uploader. Default "SPEECHKIT_TRAINING_TOKEN".
	UploadTokenEnv string `toml:"upload_token_env"`

	// UploadOnlyLabeled limits uploads to clips that the user has
	// labeled (correct / false_positive). When true (default),
	// unlabeled clips stay local — privacy-friendly default that
	// only ships audio after explicit user review.
	UploadOnlyLabeled bool `toml:"upload_only_labeled"`

	// UploadIntervalMinutes is the cadence at which the uploader
	// scans LocalCaptureDir and flushes new clips to the server.
	// Default 60.
	UploadIntervalMinutes int `toml:"upload_interval_minutes"`
}

// WakewordAutoEndConfig is the TOML surface of wakeword.AutoEndConfig.
// SilenceCutoffSec maps to wakeword.AutoEndConfig.SilenceCutoff;
// ExitPhrases is passed through verbatim. The framework defaults are
// applied in wakeword.NewAutoEndPolicy when both fields are zero.
type WakewordAutoEndConfig struct {
	// SilenceCutoffSec is the duration without user audio activity (in
	// whole seconds) after which a wake-word-triggered session ends.
	// Zero falls back to the framework default (10s).
	SilenceCutoffSec int `toml:"silence_cutoff_sec"`

	// ExitPhrases is the case-insensitive substring list checked against
	// each user-transcript snippet. Empty falls back to the framework
	// default (DE+EN common closers: "danke", "tschuess", "ende", "stop",
	// "thanks", "bye", "goodbye", ...).
	ExitPhrases []string `toml:"exit_phrases"`
}

// ServerConfig configures the standalone Linux server binary. Used only by
// cmd/speechkit-server; the desktop app never reads these values.
type ServerConfig struct {
	ListenAddr        string   `toml:"listen_addr"`          // e.g. ":8080"
	PublicURL         string   `toml:"public_url"`           // external API base URL, e.g. https://speechkit.example.com/api
	Modes             []string `toml:"modes"`                // subset of ["dictation","assist","voiceagent"]; empty = all
	AuthMode          string   `toml:"auth_mode"`            // "none" | "bearer" | "edge_hmac" | "bearer_or_edge"
	BearerTokenEnv    string   `toml:"bearer_token_env"`     // env var name holding the bearer token
	BearerRole        string   `toml:"bearer_role"`          // optional role for static bearer callers, e.g. "admin"
	AdminAuthEnabled  bool     `toml:"admin_auth_enabled"`   // enables setup/admin UI username/password login
	AdminUsername     string   `toml:"admin_username"`       // setup/admin UI username; not used by API clients
	AdminPasswordHash string   `toml:"admin_password_hash"`  // bcrypt hash for setup/admin UI login
	EdgeAuthSecretEnv string   `toml:"edge_auth_secret_env"` // env var name holding the HMAC secret
	// SmokeTokenEnv names an optional env var that holds a public-friendly
	// demo bearer token. When set, the smoke UI on `/` embeds the token in
	// the rendered HTML so visitors can run all three modes without
	// configuring credentials. The smoke identity is tagged Source="smoke"
	// (Plan="demo") so handlers and rate-limiters can distinguish demo
	// traffic. Leave empty to disable smoke-from-page entirely; operators
	// must then paste their bearer token in the UI manually.
	SmokeTokenEnv      string   `toml:"smoke_token_env"`
	PublicBaseURL      string   `toml:"public_base_url"`     // public server URL used for returned client URLs
	TrustedProxyCIDRs  []string `toml:"trusted_proxy_cidrs"` // proxies allowed to supply X-Forwarded-* headers
	CORSAllowedOrigins []string `toml:"cors_allowed_origins"`
	RateLimitRPS       float64  `toml:"rate_limit_rps"`
	RateLimitBurst     int      `toml:"rate_limit_burst"`
	// RateLimitEndpointCosts assigns per-endpoint token costs so
	// expensive handlers (LLM, transcription, voice-agent session
	// create) drain the bucket faster than cheap ones. Keys are
	// either "METHOD PATH" (e.g. "POST /v1/dictation/transcribe")
	// or bare PATH. Missing entries default to 1.0. Audit S-4.
	RateLimitEndpointCosts map[string]float64 `toml:"rate_limit_endpoint_costs"`
	// DemoDailyQuota caps how many requests a Plan="demo" identity
	// (the smoke-token surface) may make per UTC day, keyed by
	// UserID + client IP. Zero disables the quota. Audit S-5.
	DemoDailyQuota        int `toml:"demo_daily_quota"`
	MaxUploadMB           int `toml:"max_upload_mb"`
	MaxVoiceAgentSessions int `toml:"max_voiceagent_sessions"` // global cap
	MaxSessionsPerUser    int `toml:"max_sessions_per_user"`
	TicketTTLSec          int `toml:"ticket_ttl_sec"` // Voice Agent WS ticket TTL
	// VoiceAgentIdleTimeoutSec terminates a Voice Agent WebSocket session
	// after N seconds without any client- or provider-side activity.
	// Defaults to 900 (15 min). Set to 0 to disable the server-side idle
	// timeout (kernel-level idle handling stays in effect either way).
	VoiceAgentIdleTimeoutSec int `toml:"voiceagent_idle_timeout_sec"`
	// WSReadLimitBytes caps the per-frame size the Voice Agent WebSocket
	// will read from a client. Zero or negative defaults to 64 KiB,
	// which leaves ample headroom over real PCM chunk sizes (well under
	// 4 KB) without giving a single frame a 1 MiB memory amplification
	// vector. Bumped only for non-standard payloads.
	WSReadLimitBytes int64                `toml:"ws_read_limit_bytes"`
	LiveKit          ServerLiveKitConfig  `toml:"livekit"`
	WhisperBinary    string               `toml:"whisper_binary"` // absolute path inside container
	WhisperPort      int                  `toml:"whisper_port"`   // loopback port for whisper.cpp server
	ModelDir         string               `toml:"model_dir"`      // persistent volume, e.g. /var/lib/speechkit/models
	LogFormat        string               `toml:"log_format"`     // "json" | "text"
	LogLevel         string               `toml:"log_level"`      // "debug" | "info" | "warn" | "error"
	Features         ServerFeaturesConfig `toml:"features"`

	// TrainingData configures the server-side wake-word activation
	// collection endpoint. Default OFF so an operator must explicitly
	// accept training-data uploads from clients. See
	// docs/wakeword-training-data.md.
	TrainingData ServerTrainingDataConfig `toml:"training_data"`
}

// VoiceAgentLimitsConfig configures Voice Agent session capacity. Zero values
// are treated as unset and fall back to the legacy [server] limits.
type VoiceAgentLimitsConfig struct {
	MaxGlobalSessions      int `toml:"max_global_sessions"`
	MaxPerIdentitySessions int `toml:"max_per_identity_sessions"`
}

// ServerTrainingDataConfig governs the server-side wake-word
// activation pipeline. AcceptUploads defaults to false so POST
// /v1/wakeword/activations returns 503 until an operator explicitly
// opts in.
type ServerTrainingDataConfig struct {
	// AcceptUploads gates POST /v1/wakeword/activations. When false
	// the endpoint returns 503 with a clear "feature disabled"
	// payload so device-side uploaders back off gracefully. Default
	// false.
	AcceptUploads bool `toml:"accept_uploads"`

	// AudioDir is the filesystem root where uploaded audio files
	// are stored. Empty resolves to <data>/wakeword-activations/ in
	// the container. Files land under <audio_dir>/<org>/<user>/<id>.wav.
	AudioDir string `toml:"audio_dir"`

	// PerUserQuotaBytes caps how many bytes one user can have on
	// disk before the server rejects further uploads with 413. Zero
	// = unlimited. Default 1 GiB (1073741824).
	PerUserQuotaBytes int64 `toml:"per_user_quota_bytes"`

	// RetentionDays auto-deletes uploaded clips older than this
	// many days via the maintenance worker. Zero = no auto-delete.
	// Default 180.
	RetentionDays int `toml:"retention_days"`
}

type ServerLiveKitConfig struct {
	Enabled      bool   `toml:"enabled"`
	URL          string `toml:"url"`            // e.g. wss://livekit.example.com
	APIKeyEnv    string `toml:"api_key_env"`    // env var name holding the LiveKit API key
	APISecretEnv string `toml:"api_secret_env"` // env var name holding the LiveKit API secret
	TokenTTLSec  int    `toml:"token_ttl_sec"`  // join-token TTL
	RoomPrefix   string `toml:"room_prefix"`    // room name prefix for SpeechKit-managed rooms
}

type ServerFeaturesConfig struct {
	Catalog      bool `toml:"catalog"`
	StorageReads bool `toml:"storage_reads"`
	Vocabulary   bool `toml:"vocabulary"`
	TTSDirect    bool `toml:"tts_direct"`
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
	AgentMode                string `toml:"agent_mode"`  // "assist" or "voice_agent" — determines what agent_hotkey triggers
	ActiveMode               string `toml:"active_mode"` // legacy compat
	HotkeyMode               string `toml:"hotkey_mode"` // legacy compat for single behavior setting
	AutoStopSilenceMs        int    `toml:"auto_stop_silence_ms"`
	FastModeSilenceMs        int    `toml:"fast_mode_silence_ms"`        // silence threshold for Quick Capture auto-stop
	DictateSilenceTimeoutSec int    `toml:"dictate_silence_timeout_sec"` // total silence in seconds before dictate auto-stops; 0 disables
	ModelDownloadDir         string `toml:"model_download_dir"`          // Default directory for downloaded local model files
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

	// HomeAssistant configures the optional Home Assistant Conversation
	// API bridge used by the Voice-Companion skill catalog. When
	// URL+TokenEnv are both set, the HA skill is wired automatically;
	// otherwise the skill stays disabled and Voice-Companion commands
	// fall through to the LLM.
	HomeAssistant AssistHomeAssistantConfig `toml:"home_assistant"`
}

// AssistHomeAssistantConfig is the TOML surface for the
// [assist.home_assistant] block.
type AssistHomeAssistantConfig struct {
	// URL is the base URL of the Home Assistant instance, e.g.
	// "https://ha.kombify.io:8123". No trailing slash required —
	// the HA skill trims it.
	URL string `toml:"url"`

	// TokenEnv names the env var (resolved via internal/secrets) that
	// holds a Long-Lived Access Token created via HA → Profile →
	// Long-Lived Access Tokens. The value itself is NEVER stored in
	// the TOML file.
	TokenEnv string `toml:"token_env"`

	// Language overrides the language sent to HA's Conversation API.
	// When empty, the user's locale is used.
	Language string `toml:"language"`
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
	// TTS pins the Voice-Output provider profile + optional fallback that
	// Assist and Voice-Agent use when speaking back to the user. Same shape
	// as the three product-mode selections so the catalog API stays
	// symmetric. Added in v0.37 alongside the hands-free Voice-Companion
	// flow so Thalia + Companion-Live deployments can pick a stable voice
	// (e.g. Google Studio-O DE) without editing the lower-level
	// [tts.providers.*] blocks.
	TTS ModeModelSelection `toml:"tts"`
}

// Mode source values for ModeModelSelection.ModeSource. "local" means the
// desktop app runs the mode against the in-process Framework kernel (default,
// preserves all pre-0.26 behaviour). "server" routes the mode through
// ServerConnection to a remote speechkit-server.
const (
	ModeSourceLocal  = "local"
	ModeSourceServer = "server"
)

const (
	ServerConnectionAuthModeBearer = "bearer"
	ServerConnectionAuthModeAPIKey = "api_key"
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
	// value is never read from the TOML file itself — only the env var name
	// is configured here.
	BearerTokenEnv string `toml:"bearer_token_env"`

	// AuthMode selects how the resolved token is attached to outbound
	// requests. "bearer" sends Authorization: Bearer <token>. "api_key"
	// sends X-Api-Key: <token> for servers that use header-based API keys.
	// Empty/missing defaults to "bearer".
	AuthMode string `toml:"auth_mode"`

	// FallbackToLocal makes the device app fall back to the in-process
	// Framework kernel if a server call fails or the server is unreachable.
	// Useful for laptop deployments that may be offline; should be false
	// for kiosks that must never silently downgrade to local processing.
	FallbackToLocal bool `toml:"fallback_to_local"`

	// RequestTimeoutSec caps non-streaming HTTP calls (Dictation, Assist).
	// 0 means no explicit timeout (the underlying http.Client default
	// applies). Voice Agent WebSocket sessions are not affected.
	RequestTimeoutSec int `toml:"request_timeout_sec"`

	// ActiveTargetID selects the registered server target copied into the
	// compatibility fields above. Empty means the top-level URL/env/auth fields
	// are an ad-hoc single target.
	ActiveTargetID string `toml:"active_target_id"`

	// Targets is the optional local registry of SpeechKit server endpoints the
	// device can switch between. These are user/operator configured; product
	// builds must not inject private gateway/origin entries here.
	Targets []ServerConnectionTargetConfig `toml:"targets"`
}

type ServerConnectionTargetConfig struct {
	ID                string `toml:"id"`
	Label             string `toml:"label"`
	URL               string `toml:"url"`
	BearerTokenEnv    string `toml:"bearer_token_env"`
	AuthMode          string `toml:"auth_mode"`
	FallbackToLocal   bool   `toml:"fallback_to_local"`
	RequestTimeoutSec int    `toml:"request_timeout_sec"`
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

func NormalizeServerConnectionAuthMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case ServerConnectionAuthModeAPIKey:
		return ServerConnectionAuthModeAPIKey
	default:
		return ServerConnectionAuthModeBearer
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

// UpdateConfig controls the auto-update channel. The default values mirror
// the historical hard-coded constants so existing installations keep working.
// Enterprise customers set Enabled = false (full air-gap) or override
// ManifestURL with an internal mirror that serves the same JSON shape as
// https://api.github.com/repos/<owner>/<repo>/releases/latest.
type UpdateConfig struct {
	Enabled                bool   `toml:"enabled"`
	ManifestURL            string `toml:"manifest_url"`
	CheckIntervalHours     int    `toml:"check_interval_hours"`
	SignaturePinThumbprint string `toml:"signature_pin_thumbprint"` // optional Authenticode SHA-1 thumbprint; if set, installer signature verification additionally checks cert thumbprint matches (defense against compromised signing cert)
}

// LoggingConfig controls the general application log — the stream that
// surfaces transcription events, mode switches, wake-word triggers and is
// visible in the dashboard's "Logs" tab when enabled. This is one of two
// independent log surfaces in SpeechKit; the other is AuditConfig (the
// SOC2/ISO27001 compliance trail). Both default to OFF so a privacy-first
// install writes nothing to disk until the operator explicitly opts in.
//
// Level options: "debug" | "info" | "warn" | "error" | "off". The
// SPEECHKIT_LOG_LEVEL environment variable overrides this field at startup
// — the recommended path for support engineers who need a one-session
// debug toggle without touching config.toml. When Level="off" the
// fanoutWriter short-circuits to a no-op before any I/O syscall, so even
// extremely chatty hot paths (overlay sync loop, audio status pumps) carry
// zero log overhead.
//
// MaxFileSizeMB and MaxFiles apply only when Level != "off". They are
// preserved at enterprise-friendly defaults (50 MB / 30 files) for the
// case where an operator opts logging in.
type LoggingConfig struct {
	MaxFileSizeMB int    `toml:"max_file_size_mb"`
	MaxFiles      int    `toml:"max_files"`
	Level         string `toml:"level"` // "debug" | "info" | "warn" | "error" | "off"
}

// AuditConfig controls the dedicated audit-log stream introduced in Phase 0.
// This is the structured compliance trail (SOC2 / ISO27001 evidence) — no
// transcript content, only event metadata (when, who, which model, success
// vs failure). It is one of two independent log surfaces in SpeechKit; the
// other is LoggingConfig (the general application log).
//
// As of 2026-05-19 Enabled defaults to FALSE — opt-in. The earlier
// "default-true so we have evidence" stance was overridden by the privacy
// principle: a user with no compliance obligations should not produce
// audit artefacts on disk by default. Enterprises that need the audit
// trail flip Enabled=true in Settings → Compliance (or via config.toml)
// and configure RetentionDays plus the OTLP exporter.
type AuditConfig struct {
	Enabled         bool   `toml:"enabled"`
	RetentionDays   int    `toml:"retention_days"`
	EventLogEnabled bool   `toml:"event_log_enabled"` // wired in P2.1 (cpv.3.1) — Windows Event Log mirror
	OTLPEndpoint    string `toml:"otlp_endpoint"`     // wired in P2.2 (cpv.3.2) — OTLP exporter
	OTLPCertFile    string `toml:"otlp_cert_file"`
	OTLPKeyFile     string `toml:"otlp_key_file"`
	OTLPCAFile      string `toml:"otlp_ca_file"`
}

// TelemetryConfig is the single switch surface for every outbound non-provider
// HTTP call SpeechKit may make. Today the only such call is the auto-update
// check; future calls (crash reports, usage stats) must add a field here
// rather than create a parallel toggle.
type TelemetryConfig struct {
	UpdateCheck bool `toml:"update_check"`
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
	STTAPIKeyEnv string `toml:"stt_api_key_env"`
	STTModel     string `toml:"stt_model"`
	UtilityModel string `toml:"utility_model"`
	AssistModel  string `toml:"assist_model"`
	AgentModel   string `toml:"agent_model"`
	// Region is the Google Cloud region the customer's API key / project is
	// pinned to. Default "europe-west3" (Frankfurt) reflects the EU-enterprise
	// compliance posture. US customers should explicitly set "us-central1".
	//
	// IMPORTANT: this field feeds the byok.key_updated audit event and the
	// settings UI. It does NOT redirect API traffic — the Gemini Live endpoint
	// is a single global WebSocket (generativelanguage.googleapis.com). Actual
	// data residency is controlled at the Google Cloud project level. Both the
	// project region AND this field must match for the audit event to be
	// accurate. See docs/compliance/byok-gemini-region-pinning.md.
	Region string `toml:"region"`
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
	Piper       TTSPiper       `toml:"piper"`
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

// TTSPiper configures the offline Piper subprocess TTS provider.
// The piper binary must be on PATH (or pointed at via Binary). Voice models
// are NOT bundled — operators run scripts/prepare-piper-voices.{ps1,sh} to
// fetch ONNX voices from rhasspy/piper-voices into VoiceDir.
type TTSPiper struct {
	Enabled  bool   `toml:"enabled"`
	Binary   string `toml:"binary"`    // path to piper executable; empty => "piper" on PATH
	VoiceDir string `toml:"voice_dir"` // directory holding *.onnx voice files
	// DefaultVoices maps a locale short-code ("en", "de", ...) to a voice
	// filename inside VoiceDir. Empty entries fall back to the built-in
	// defaults (en_US-amy-medium.onnx, de_DE-thorsten-medium.onnx).
	DefaultVoices map[string]string `toml:"default_voices"`
	TimeoutSec    int               `toml:"timeout_sec"` // subprocess timeout; 0 => 30 s
}

// VoiceAgentConfig configures the real-time Voice Agent Mode.
type VoiceAgentConfig struct {
	Enabled bool `toml:"enabled"`
	// Provider selects the backend that drives a Voice Agent session.
	// Supported values:
	//   ""          (default) — same as "gemini"
	//   "gemini"    — Google Gemini Live (cloud, GOOGLE_AI_API_KEY required)
	//   "cascaded"  — self-hosted whisper.cpp → Genkit agent LLM → TTS pipeline
	//                 (CPU-capable; no external realtime dependency)
	//   "moshi"     — self-hosted Kyutai Moshi Rust server (GPU required, M9b)
	//
	// The Server-Target reads this field via cmd/speechkit-server; the Device-
	// Target currently always uses "gemini" and ignores it.
	Provider               string `toml:"provider"`
	Model                  string `toml:"model"`             // Real-time model ID (e.g. "gemini-3.1-flash-live-preview")
	FallbackModel          string `toml:"fallback_model"`    // Fallback real-time model
	Voice                  string `toml:"voice"`             // Voice name for real-time model
	AgentProfileID         string `toml:"agent_profile_id"`  // Built-in Voice Agent profile ID; "default" preserves current behavior.
	AgentSequenceID        string `toml:"agent_sequence_id"` // Optional workflow sequence ID; empty uses the selected persona default.
	FrameworkPrompt        string `toml:"framework_prompt"`  // Durable host/framework instruction that defines the Voice Agent behavior
	RefinementPrompt       string `toml:"refinement_prompt"` // User-specific refinement appended to the framework prompt
	Instruction            string `toml:"instruction"`       // Legacy alias for FrameworkPrompt
	AutoStartOnLaunch      bool   `toml:"auto_start_on_launch"`
	CloseBehavior          string `toml:"close_behavior"` // "continue" keeps the conversation window in the taskbar; "new_chat" ends the current chat on close
	ReminderAfterIdleSec   int    `toml:"reminder_after_idle_sec"`
	DeactivateAfterIdleSec int    `toml:"deactivate_after_idle_sec"`
	// HoldReleaseGraceSec controls how long the Voice Agent stays open after
	// the user releases a hold-to-talk shortcut so the model has time to
	// deliver its reply. 0 (or unset) falls back to the kernel default
	// (10 seconds). The Device-Target hard-caps this at 30 seconds; values
	// above that are silently clamped at runtime so a misconfigured profile
	// cannot strand the user in a "still active" session.
	HoldReleaseGraceSec             int                    `toml:"hold_release_grace_sec"`
	PipelineFallback                bool                   `toml:"pipeline_fallback"` // Use STT -> Agent LLM -> optional TTS when the selected Voice Agent profile is not native realtime.
	ShowPrompter                    bool                   `toml:"show_prompter"`     // Show live transcript prompter window
	EnableSessionSummary            bool                   `toml:"enable_session_summary"`
	EnableInputTranscript           bool                   `toml:"enable_input_transcript"`
	EnableOutputTranscript          bool                   `toml:"enable_output_transcript"`
	EnableAffectiveDialog           bool                   `toml:"enable_affective_dialog"`
	ThinkingEnabled                 bool                   `toml:"thinking_enabled"`
	IncludeThoughts                 bool                   `toml:"include_thoughts"`
	ThinkingBudget                  int                    `toml:"thinking_budget"`
	ThinkingLevel                   string                 `toml:"thinking_level"`
	ContextCompressionEnabled       bool                   `toml:"context_compression_enabled"`
	ContextCompressionTriggerTokens int64                  `toml:"context_compression_trigger_tokens"`
	ContextCompressionTargetTokens  int64                  `toml:"context_compression_target_tokens"`
	AutomaticActivityDetection      bool                   `toml:"automatic_activity_detection"`
	ActivityHandling                string                 `toml:"activity_handling"`
	TurnCoverage                    string                 `toml:"turn_coverage"`
	VADStartSensitivity             string                 `toml:"vad_start_sensitivity"`
	VADEndSensitivity               string                 `toml:"vad_end_sensitivity"`
	VADPrefixPaddingMs              int                    `toml:"vad_prefix_padding_ms"`
	VADSilenceDurationMs            int                    `toml:"vad_silence_duration_ms"`
	Limits                          VoiceAgentLimitsConfig `toml:"limits"`
}

// VoiceAgentSessionLimits returns the effective Voice Agent session caps.
// The v0.40.x config surface prefers [voice_agent.limits], while the older
// [server] fields remain supported for existing deployments.
func (cfg *Config) VoiceAgentSessionLimits() VoiceAgentLimitsConfig {
	if cfg == nil {
		return VoiceAgentLimitsConfig{}
	}
	limits := VoiceAgentLimitsConfig{
		MaxGlobalSessions:      cfg.Server.MaxVoiceAgentSessions,
		MaxPerIdentitySessions: cfg.Server.MaxSessionsPerUser,
	}
	if cfg.VoiceAgent.Limits.MaxGlobalSessions > 0 {
		limits.MaxGlobalSessions = cfg.VoiceAgent.Limits.MaxGlobalSessions
	}
	if cfg.VoiceAgent.Limits.MaxPerIdentitySessions > 0 {
		limits.MaxPerIdentitySessions = cfg.VoiceAgent.Limits.MaxPerIdentitySessions
	}
	return limits
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
