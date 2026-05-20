// Package wakeword implements an always-on, on-device keyword spotter that
// any of the three SpeechKit modes (Dictation, Assist, Voice Agent) can opt
// into.
//
// The package is platform-neutral kernel code. SpeechKit's framework
// architecture splits responsibilities across two layers:
//
//   - Framework kernel (this package): keyword-spotting detector, audio
//     buffering, debounce/cooldown logic, event dispatch contracts.
//     Imported by every client that wants wake-word support.
//
//   - Per-client adapters: own the OS-level audio source, hotkey mapping
//     and status surface. The Windows Device-Target lives in
//     cmd/speechkit/desktop_wakeword.go and is the reference adapter that
//     ships with the SpeechKit demo. Future client targets (Android via
//     gomobile, Local-Target Go CLI, web via WASM bindings, native iOS)
//     plug their audio source and event sink into the same Pipeline.
//
// Wake-word is never a server-side concern in SpeechKit. The Server-Target
// (internal/server) exposes Dictation/Assist/Voice Agent over HTTP/WS but
// has no wake-word surface — running an always-on mic in a server process
// makes no architectural sense and is intentionally out of scope.
//
// Detection runs via the sherpa-onnx Zipformer KWS model
// (github.com/k2-fsa/sherpa-onnx-go). The model is Apache-2.0, ships as a
// single ~17 MB asset with encoder/decoder/joiner ONNX files plus a tokens
// + keywords file, and supports open-vocabulary keyword spotting — users
// declare keywords as text and tune a per-keyword threshold without
// retraining.
package wakeword

import "time"

// Audio contract for the wake-word pipeline. Matches internal/audio and
// internal/vad — 16 kHz mono S16 PCM. The detector accepts the same frame
// cadence the rest of the kernel produces, so a single audio session can
// fan out to VAD + wake-word without resampling.
const (
	SampleRate     = 16000
	BytesPerSample = 2
	Channels       = 1

	// FrameSamples is the unit the audio session delivers per
	// SetPCMHandler callback (80 ms at 16 kHz).
	FrameSamples = 1280
	FrameBytes   = FrameSamples * BytesPerSample
	FrameDur     = 80 * time.Millisecond
)

// Config controls the wake-word pipeline behaviour. Fields map 1:1 onto the
// [wakeword] block in the user TOML config; see internal/config/config.go
// for the on-disk schema.
//
// The new sherpa-onnx-backed implementation does not need
// melspec/embedding/prediction triplets — those legacy fields are kept on
// the config surface (so older user configs do not error out) but ignored
// by this package. The relevant inputs are ModelDir + Keywords (or
// KeywordsFile).
type Config struct {
	// Enabled gates the entire wake-word system. When false, no detector is
	// loaded and no audio session is opened on its behalf. Default false —
	// wake-word is an opt-in feature in line with the dictation industry
	// norm (Wispr Flow, Superwhisper, VoiceInk all default to hotkey-only).
	Enabled bool

	// Phrase is the display label of the active wake phrase (e.g.
	// "Hey Quby"). Surfaced in the tray and status feed. Detection is
	// driven by the BPE-tokenised Keywords/KeywordsFile fields below, not
	// by this label — multiple keywords can map to the same display label.
	Phrase string

	// ModelDir is the directory holding the sherpa-onnx KWS model assets
	// (encoder/decoder/joiner ONNX + tokens.txt). The Windows reference
	// adapter resolves a default of <exe_dir>/wakeword-kws when empty.
	ModelDir string

	// KeywordsFile is the path to a sherpa-onnx keywords.txt file
	// (BPE-tokenised, one keyword per line with optional `:boost` and
	// trailing `@display-name`). Takes precedence over inline Keywords
	// when both are set. The Windows adapter ships a default file beside
	// the KWS model.
	KeywordsFile string

	// Keywords is the inline alternative to KeywordsFile — one
	// BPE-tokenised entry per slice element. Use this for programmatic
	// callers (tests, ad-hoc clients) that do not want to materialise a
	// file on disk.
	Keywords []string

	// DefaultMode is the runtime mode triggered when a keyword fires.
	// One of "dictate" | "assist" | "voice_agent".
	DefaultMode string

	// Threshold is the minimum acoustic probability for sherpa-onnx to
	// emit a keyword hit (0.0–1.0, lower = more sensitive). Each keyword
	// can override it via the `:boost` suffix in KeywordsFile; this is the
	// global default.
	Threshold float32

	// MinConsecutiveFrames is the number of decoded windows the same
	// keyword must fire in before the pipeline emits a DetectionEvent.
	// 1 = fire immediately, 2 = require two consecutive hits (default).
	MinConsecutiveFrames int

	// Cooldown is the minimum interval between two emitted DetectionEvents
	// for the same keyword. Prevents the same utterance triggering twice
	// and gives the downstream mode time to spin up before another fire.
	Cooldown time.Duration

	// AutoEnd steuert das automatische Beenden von Sessions die durch
	// dieses Wake-Word getriggert wurden. Provider-agnostisch — gilt fuer
	// Gemini Live, OpenAI Realtime und Local-Cascaded gleichermassen. Wenn
	// der Zero-Value verwendet wird (SilenceCutoff=0 + len(ExitPhrases)=0),
	// faellt die Pipeline auf DefaultAutoEndConfig zurueck. Siehe
	// AutoEndPolicy + DefaultAutoEndConfig fuer die Framework-Baseline.
	AutoEnd AutoEndConfig
}

// DetectionEvent is emitted when a keyword fires.
type DetectionEvent struct {
	// Phrase is the configured display name (e.g. "Hey Quby").
	Phrase string

	// Keyword is the raw keyword string sherpa-onnx returned (e.g.
	// "HEY QUBY" or the @-suffix when the keywords file labelled it).
	Keyword string

	// Mode is the runtime mode the wake should trigger ("dictate" |
	// "assist" | "voice_agent"), copied from Config.DefaultMode at the
	// moment of detection so downstream consumers don't need to re-read
	// config.
	Mode string

	// Probability is informational only — sherpa-onnx KWS already applies
	// its threshold internally before reporting a keyword, so this is
	// always 1.0 from the pipeline. Kept on the surface for API stability
	// with the previous openWakeWord-based detector.
	Probability float32

	// At is the wall-clock time of the trigger.
	At time.Time
}

// Sink receives DetectionEvents from the pipeline. The desktop dispatcher
// implements this to convert events into synthetic hotkey events.
type Sink interface {
	Emit(DetectionEvent)
}

// SinkFunc adapts a plain function to the Sink interface.
type SinkFunc func(DetectionEvent)

// Emit calls f.
func (f SinkFunc) Emit(ev DetectionEvent) { f(ev) }
