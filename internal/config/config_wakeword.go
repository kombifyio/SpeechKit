package config

// Wake-word, hands-free activation, and auto-end configuration types.

// Hands-Free target-mode values for HandsFreeConfig.TargetMode.
const (
	HandsFreeTargetAssist              = "assist"
	HandsFreeTargetVoiceAgent          = "voice_agent"
	HandsFreeTargetDictationUIAssisted = "dictation_ui_assisted"
)

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

// HandsFreeConfig is SpeechKit's user-facing no/low-UI activation model.
// It is not a fourth mode: TargetMode selects Dictation, Assist, or Voice
// Agent behavior while this block controls wake activation, auto-end, and
// hands-free speaker output.
type HandsFreeConfig struct {
	// Enabled gates hands-free activation. Default false (opt-in).
	Enabled bool `toml:"enabled"`

	// ActivationPhraseID picks one of wakeword.DefaultCatalog's curated
	// phrases. The low-level detector mirrors this value to Wakeword.PhraseID.
	ActivationPhraseID string `toml:"activation_phrase_id"`

	// TargetMode is one of "assist", "voice_agent", or
	// "dictation_ui_assisted". Dictation hands-free still requires a visible
	// text target or explicit commit surface.
	TargetMode string `toml:"target_mode"`

	// AutoEndSilenceCutoffSec ends wake-triggered sessions after this many
	// seconds of silence. Zero falls back to the framework default.
	AutoEndSilenceCutoffSec int `toml:"auto_end_silence_cutoff_sec"`

	// VoiceOutputEnabled allows Assist/Voice-Agent hands-free experiences to
	// speak. Dictation UI-assisted targets should keep this false.
	VoiceOutputEnabled bool `toml:"voice_output_enabled"`
}

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
	// more false-rejects. Defaults to 1.
	//
	// openWakeWord scores one frame per 80ms, so this is a duration gate:
	// N frames demands the score hold above Threshold for N*80ms without a
	// single dip. A wake phrase is only ~500ms long and the score spikes
	// rather than plateaus, so values above 3 (240ms) make the phrase
	// effectively undetectable — the counter resets on the first dip and
	// never reaches N. Raise Threshold to cut false-accepts; do not raise
	// this past 3.
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
