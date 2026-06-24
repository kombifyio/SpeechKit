package config

// Device/Local-Target configuration types: general app, audio, UI, store,
// vocabulary, customization, speech defaults, assist, and shortcuts.

type StoreConfig struct {
	Backend            string `toml:"backend"` // "sqlite" | "postgres" | registered name
	SQLitePath         string `toml:"sqlite_path"`
	PostgresDSN        string `toml:"postgres_dsn"`
	SaveAudio          bool   `toml:"save_audio"`
	AudioRetentionDays int    `toml:"audio_retention_days"`
	MaxAudioStorageMB  int    `toml:"max_audio_storage_mb"`
}

type GeneralConfig struct {
	Language                       string `toml:"language"`
	Hotkey                         string `toml:"hotkey"` // Deprecated: legacy single-hotkey field kept for config file compat. Use DictateHotkey.
	DictateHotkey                  string `toml:"dictate_hotkey"`
	AssistHotkey                   string `toml:"assist_hotkey"`
	VoiceAgentHotkey               string `toml:"voice_agent_hotkey"`
	DictateHotkeyBehavior          string `toml:"dictate_hotkey_behavior"`
	AssistHotkeyBehavior           string `toml:"assist_hotkey_behavior"`
	VoiceAgentHotkeyBehavior       string `toml:"voice_agent_hotkey_behavior"`
	DictateEnabled                 bool   `toml:"dictate_enabled"`
	AssistEnabled                  bool   `toml:"assist_enabled"`
	VoiceAgentEnabled              bool   `toml:"voice_agent_enabled"`
	AutoStartOnLaunch              bool   `toml:"auto_start_on_launch"`
	EagerWarmup                    bool   `toml:"eager_warmup"`
	AgentHotkey                    string `toml:"agent_hotkey"`
	AgentMode                      string `toml:"agent_mode"`  // "assist" or "voice_agent" — determines what agent_hotkey triggers
	ActiveMode                     string `toml:"active_mode"` // legacy compat
	HotkeyMode                     string `toml:"hotkey_mode"` // legacy compat for single behavior setting
	AutoStopSilenceMs              int    `toml:"auto_stop_silence_ms"`
	FastModeSilenceMs              int    `toml:"fast_mode_silence_ms"`              // silence threshold for Quick Capture auto-stop
	DictateSilenceTimeoutSec       int    `toml:"dictate_silence_timeout_sec"`       // total silence in seconds before dictate auto-stops; 0 disables
	DictationIntermediateSegmentMs int    `toml:"dictation_intermediate_segment_ms"` // minimum utterance size before live dictation emits a pause-bounded segment
	ModelDownloadDir               string `toml:"model_download_dir"`                // Default directory for downloaded local model files
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

type CustomizationConfig struct {
	ActiveTemplateIDs []string `toml:"active_template_ids"`
}

// SpeechDefaultsConfig holds provider-neutral voice defaults that can be
// projected into STT, TTS, and Voice Agent provider adapters when supported.
type SpeechDefaultsConfig struct {
	Language       string  `toml:"language"`
	DetectLanguage bool    `toml:"detect_language"`
	Punctuation    bool    `toml:"punctuation"`
	SmartFormat    bool    `toml:"smart_format"`
	VocabularyBias bool    `toml:"vocabulary_bias"`
	Timestamps     bool    `toml:"timestamps"`
	EndpointingMs  int     `toml:"endpointing_ms"`
	TurnDetection  bool    `toml:"turn_detection"`
	Voice          string  `toml:"voice"`
	Speed          float64 `toml:"speed"`
	AudioFormat    string  `toml:"audio_format"`
	// LowConfidenceThreshold flags recognized words whose provider-reported
	// acoustic confidence is below this value (0..1) so the host can surface
	// likely-misrecognized terms. 0 disables the check. Only Deepgram and
	// AssemblyAI expose per-word confidence today.
	LowConfidenceThreshold float64 `toml:"low_confidence_threshold"`
}

type AssistConfig struct {
	EnabledTools []string `toml:"enabled_tools"`

	// IncludeWindowContext controls whether the Device-Target captures the
	// foreground application name + window title and feeds them to the
	// Assist LLM as context. Window titles can be sensitive (document
	// names, chat partners, URLs), so this is an explicit opt-out. The
	// Server-Target ignores this flag — there the integrating client
	// decides whether to send the `app`/`window_title` request fields.
	// Defaults to true (see config/defaults.go).
	IncludeWindowContext bool `toml:"include_window_context"`

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
