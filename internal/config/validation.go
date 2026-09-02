package config

import (
	"errors"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/hotkeycombo"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/hostconfig"
)

// DefaultMeetingScreenshotHotkey is the shipped global shortcut for the Meeting
// Mode screenshot quick action. It fires while a meeting is live to capture the
// monitor under the cursor immediately.
const DefaultMeetingScreenshotHotkey = "ctrl+alt+s"

// DisabledMeetingScreenshotHotkey is the persisted sentinel for an explicitly
// disabled shortcut. It must stay distinct from an unset value, which receives
// the default shortcut on first load.
const DisabledMeetingScreenshotHotkey = "none"

// NormalizeMeetingScreenshotHotkey canonicalizes the Meeting screenshot
// shortcut combo. Empty falls back to the default; disable aliases normalize to
// the persisted "none" sentinel; an unparseable combo falls back to the default
// rather than silently disabling the feature.
func NormalizeMeetingScreenshotHotkey(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "":
		return DefaultMeetingScreenshotHotkey
	case "none", "off", "disabled":
		return DisabledMeetingScreenshotHotkey
	}
	if _, err := hotkeycombo.ParseStrict(v); err != nil {
		return DefaultMeetingScreenshotHotkey
	}
	return v
}

// ErrMeetingScreenshotHotkeyConflict reports that the requested screenshot
// shortcut collides with a mode hotkey. It is returned (not swallowed) so the
// settings layer can roll back and surface the conflict.
var ErrMeetingScreenshotHotkeyConflict = errors.New("meeting screenshot hotkey conflicts with an existing hotkey")

// meetingHotkeyBinding pairs a human-facing name with a configured combo string
// for conflict reporting.
type meetingHotkeyBinding struct {
	name  string
	combo string
}

// MeetingScreenshotHotkeyConflict returns the name of the existing hotkey the
// Meeting screenshot shortcut collides with, or "" when there is no conflict.
// Comparison is on normalized VK combos, so "ctrl+alt+s" and "Alt+Ctrl+S" are
// recognized as the same chord regardless of token order. A disabled shortcut
// never conflicts.
func MeetingScreenshotHotkeyConflict(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	screenshot := NormalizeMeetingScreenshotHotkey(cfg.Meeting.ScreenshotHotkey)
	if screenshot == DisabledMeetingScreenshotHotkey {
		return ""
	}
	target, err := hotkeycombo.ParseStrict(screenshot)
	if err != nil {
		return ""
	}
	for _, binding := range []meetingHotkeyBinding{
		{"dictate", cfg.General.DictateHotkey},
		{"assist", cfg.General.AssistHotkey},
		{"voice_agent", cfg.General.VoiceAgentHotkey},
		{"agent", cfg.General.AgentHotkey},
		{"hotkey", cfg.General.Hotkey},
	} {
		if strings.TrimSpace(binding.combo) == "" {
			continue
		}
		other, err := hotkeycombo.ParseStrict(binding.combo)
		if err != nil {
			continue
		}
		if hotkeyComboEqual(target, other) {
			return binding.name
		}
	}
	return ""
}

// ApplyMeetingScreenshotHotkey sets the Meeting screenshot shortcut
// transactionally: it normalizes next, and only commits when the result does
// not conflict with a mode hotkey. On conflict it leaves cfg unchanged and
// returns ErrMeetingScreenshotHotkeyConflict, so a failed settings change rolls
// back cleanly rather than leaving two hotkeys fighting over the same chord.
func ApplyMeetingScreenshotHotkey(cfg *Config, next string) (string, error) {
	if cfg == nil {
		return "", errors.New("config: nil config")
	}
	previous := cfg.Meeting.ScreenshotHotkey
	normalized := NormalizeMeetingScreenshotHotkey(next)
	cfg.Meeting.ScreenshotHotkey = normalized
	if conflict := MeetingScreenshotHotkeyConflict(cfg); conflict != "" {
		cfg.Meeting.ScreenshotHotkey = previous // roll back
		return previous, ErrMeetingScreenshotHotkeyConflict
	}
	return normalized, nil
}

// hotkeyComboEqual reports whether two normalized VK combos are the same set of
// keys, independent of order.
func hotkeyComboEqual(a, b []uint32) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	seen := make(map[uint32]int, len(a))
	for _, k := range a {
		seen[k]++
	}
	for _, k := range b {
		if seen[k] == 0 {
			return false
		}
		seen[k]--
	}
	return true
}

func NormalizeHotkeyBehavior(value, fallback string) string {
	return hostconfig.NormalizeHotkeyBehavior(value, fallback)
}

func NormalizeDictationProcessingMode(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case DictationProcessingModeFinalFull:
		return DictationProcessingModeFinalFull
	case DictationProcessingModeSegmentBatch:
		return DictationProcessingModeSegmentBatch
	case DictationProcessingModeProviderStream:
		return DictationProcessingModeProviderStream
	case DictationProcessingModeAuto:
		return DictationProcessingModeAuto
	}
	// Empty or unrecognised: honour the caller's fallback (this is how the
	// blank-config default flows through). Backstop to final_full only when
	// the fallback is itself empty or invalid, so a slow-but-safe mode is the
	// worst case rather than an undefined one.
	switch strings.ToLower(strings.TrimSpace(fallback)) {
	case DictationProcessingModeFinalFull:
		return DictationProcessingModeFinalFull
	case DictationProcessingModeSegmentBatch:
		return DictationProcessingModeSegmentBatch
	case DictationProcessingModeProviderStream:
		return DictationProcessingModeProviderStream
	case DictationProcessingModeAuto:
		return DictationProcessingModeAuto
	default:
		return DictationProcessingModeFinalFull
	}
}

func NormalizeDictationLiveCommit(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case DictationLiveCommitImmediate:
		return DictationLiveCommitImmediate
	case DictationLiveCommitPhrase:
		return DictationLiveCommitPhrase
	case DictationLiveCommitPassage:
		return DictationLiveCommitPassage
	}
	switch strings.ToLower(strings.TrimSpace(fallback)) {
	case DictationLiveCommitImmediate:
		return DictationLiveCommitImmediate
	case DictationLiveCommitPhrase:
		return DictationLiveCommitPhrase
	case DictationLiveCommitPassage:
		return DictationLiveCommitPassage
	default:
		return DictationLiveCommitPassage
	}
}

func NormalizeAudioInputSource(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AudioInputSourceMicrophone, "":
		return AudioInputSourceMicrophone
	case AudioInputSourceSystemLoopback, "system", "loopback":
		return AudioInputSourceSystemLoopback
	case AudioInputSourceMicAndSystem, "microphone_and_system", "mic+system", "microphone+system":
		return AudioInputSourceMicAndSystem
	default:
		if strings.TrimSpace(fallback) == "" {
			return AudioInputSourceMicrophone
		}
		if strings.EqualFold(strings.TrimSpace(fallback), value) {
			return AudioInputSourceMicrophone
		}
		return NormalizeAudioInputSource(fallback, AudioInputSourceMicrophone)
	}
}

func NormalizeVoiceAgentCloseBehavior(value, fallback string) string {
	return hostconfig.NormalizeVoiceAgentCloseBehavior(value, fallback)
}

// NormalizeVoiceAgentBargeIn coerces config/UI values to the supported
// Voice Agent barge-in modes. Unknown values fall back to the given fallback,
// then to "auto" (headset-detected full duplex).
func NormalizeVoiceAgentBargeIn(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case VoiceAgentBargeInAuto:
		return VoiceAgentBargeInAuto
	case VoiceAgentBargeInAlways:
		return VoiceAgentBargeInAlways
	case VoiceAgentBargeInNever:
		return VoiceAgentBargeInNever
	default:
		if strings.TrimSpace(fallback) == "" {
			return VoiceAgentBargeInAuto
		}
		if strings.EqualFold(strings.TrimSpace(fallback), value) {
			return VoiceAgentBargeInAuto
		}
		return NormalizeVoiceAgentBargeIn(fallback, VoiceAgentBargeInAuto)
	}
}

// NormalizeVoiceAgentWarmLinger clamps the hold-to-talk resume window
// (seconds) to the supported range. 0 disables the warm linger; values above
// 120 s are capped so a warm cloud connection cannot idle unboundedly.
// Negative values fall back to the given fallback (itself clamped).
func NormalizeVoiceAgentWarmLinger(value, fallback int) int {
	if value < 0 {
		if fallback < 0 {
			return 0
		}
		value = fallback
	}
	if value > 120 {
		return 120
	}
	return value
}

// NormalizeVoiceAgentPauseTolerance clamps the client-side pause tolerance
// (milliseconds of silence filtered from the mic stream before the provider
// may endpoint the turn) to [0, 3000]. 0 disables the filter. Negative values
// fall back to the given fallback (itself clamped).
func NormalizeVoiceAgentPauseTolerance(value, fallback int) int {
	if value < 0 {
		if fallback < 0 {
			return 0
		}
		value = fallback
	}
	if value > 3000 {
		return 3000
	}
	return value
}

// NormalizeWakewordDefaultMode coerces an arbitrary mode string to one of
// the supported wake-word target modes. Unknown values fall back to
// WakewordDefaultModeVoiceAgent (the most common consumer use case).
func NormalizeWakewordDefaultMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case WakewordDefaultModeDictate:
		return WakewordDefaultModeDictate
	case WakewordDefaultModeAssist:
		return WakewordDefaultModeAssist
	case WakewordDefaultModeVoiceAgent, "":
		return WakewordDefaultModeVoiceAgent
	default:
		return WakewordDefaultModeVoiceAgent
	}
}

// NormalizeHandsFreeTargetMode coerces arbitrary config/UI values to the
// supported hands-free target modes. Unknown values fall back to Voice Agent,
// the primary fully hands-free companion experience.
func NormalizeHandsFreeTargetMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case HandsFreeTargetAssist:
		return HandsFreeTargetAssist
	case HandsFreeTargetVoiceAgent, "voice-agent", "voiceagent", "":
		return HandsFreeTargetVoiceAgent
	case HandsFreeTargetDictationUIAssisted, "dictation-ui-assisted", "dictation", "dictate", "transcribe":
		return HandsFreeTargetDictationUIAssisted
	default:
		return HandsFreeTargetVoiceAgent
	}
}

func HandsFreeTargetToWakewordDefaultMode(target string) string {
	switch NormalizeHandsFreeTargetMode(target) {
	case HandsFreeTargetAssist:
		return WakewordDefaultModeAssist
	case HandsFreeTargetDictationUIAssisted:
		return WakewordDefaultModeDictate
	default:
		return WakewordDefaultModeVoiceAgent
	}
}

func WakewordDefaultModeToHandsFreeTarget(mode string) string {
	switch NormalizeWakewordDefaultMode(mode) {
	case WakewordDefaultModeAssist:
		return HandsFreeTargetAssist
	case WakewordDefaultModeDictate:
		return HandsFreeTargetDictationUIAssisted
	default:
		return HandsFreeTargetVoiceAgent
	}
}

// NormalizeWakewordBackend coerces arbitrary config/UI values to the small
// set of detector backend IDs the desktop app understands.
//
// Empty/unset (and any unrecognized value) resolves to sherpa_kws — the only
// backend whose KWS model and keywords file are unconditionally staged into
// the bundle (scripts/build.ps1 + scripts/prepare-wakeword-model.ps1) and the
// only backend the Box companion runs. This keeps a fresh or backend-less
// config deterministic and self-consistent with the struct default in
// defaults.go, instead of silently landing on openWakeWord, whose per-phrase
// ONNX models can be absent on dev/partial builds and then disable wake-word
// with no obvious cause. Existing installs that explicitly pinned another
// backend keep it.
func NormalizeWakewordBackend(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case WakewordBackendLiveKitOpenWakeWord, "livekit", "openwakeword", "livekit_openwakeword_onnx":
		return WakewordBackendLiveKitOpenWakeWord
	case WakewordBackendSTTPhrase, "stt", "phrase_match", "stt_phrase_match":
		return WakewordBackendSTTPhrase
	case WakewordBackendSherpaKWS, "":
		return WakewordBackendSherpaKWS
	default:
		return WakewordBackendSherpaKWS
	}
}

// NormalizeWakewordThreshold clamps the threshold to a sane range. Values
// outside (0, 1] are coerced to 0.5 — the Wyoming/openWakeWord canonical
// default. Sherpa-onnx KWS uses a separate per-backend default (0.25) via
// effectiveWakewordThreshold in the desktop adapter.
func NormalizeWakewordThreshold(value float64) float64 {
	if value <= 0 || value > 1 {
		return 0.5
	}
	return value
}

// Voice Assistant appearance vocabulary (speechkit.voice_ui.v1): the visual
// variant and the semantic brand mark of the speechkit-voice-assistant
// element. The same ids are used by the Device settings UI, the server
// [server.assistant_ui] block, and the Android appearance setting.
const (
	AssistantVariantAura     = "aura"
	AssistantVariantWaveform = "waveform"

	AssistantMarkRosette = "rosette"
	AssistantMarkK       = "k"
	AssistantMarkNone    = "none"
)

// NormalizeAssistantVariant coerces unknown values to the default Aura orb.
func NormalizeAssistantVariant(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AssistantVariantWaveform:
		return AssistantVariantWaveform
	default:
		return AssistantVariantAura
	}
}

// NormalizeAssistantMark coerces unknown values to the standard rosette mark.
func NormalizeAssistantMark(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AssistantMarkK:
		return AssistantMarkK
	case AssistantMarkNone:
		return AssistantMarkNone
	default:
		return AssistantMarkRosette
	}
}

func NormalizeOverlayFeedbackMode(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case OverlayFeedbackModeBigProductivity:
		return OverlayFeedbackModeBigProductivity
	case OverlayFeedbackModeSmallFeedback:
		return OverlayFeedbackModeSmallFeedback
	default:
		if strings.TrimSpace(fallback) == "" {
			return OverlayFeedbackModeSmallFeedback
		}
		if strings.EqualFold(strings.TrimSpace(fallback), value) {
			return OverlayFeedbackModeSmallFeedback
		}
		return NormalizeOverlayFeedbackMode(fallback, OverlayFeedbackModeSmallFeedback)
	}
}
