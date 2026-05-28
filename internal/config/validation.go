package config

import "strings"

func NormalizeHotkeyBehavior(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case HotkeyBehaviorHoldToTalk, legacyHotkeyBehaviorPushToTalk:
		// Legacy push_to_talk inputs are normalized to the canonical
		// hold_to_talk value so subsequent writes use the new name.
		return HotkeyBehaviorHoldToTalk
	case HotkeyBehaviorToggle:
		return HotkeyBehaviorToggle
	default:
		if strings.TrimSpace(fallback) == "" {
			return HotkeyBehaviorHoldToTalk
		}
		if strings.EqualFold(strings.TrimSpace(fallback), value) {
			return HotkeyBehaviorHoldToTalk
		}
		return NormalizeHotkeyBehavior(fallback, HotkeyBehaviorHoldToTalk)
	}
}

func NormalizeVoiceAgentCloseBehavior(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case VoiceAgentCloseBehaviorContinue:
		return VoiceAgentCloseBehaviorContinue
	case VoiceAgentCloseBehaviorNewChat:
		return VoiceAgentCloseBehaviorNewChat
	default:
		if strings.TrimSpace(fallback) == "" {
			return VoiceAgentCloseBehaviorContinue
		}
		if strings.EqualFold(strings.TrimSpace(fallback), value) {
			return VoiceAgentCloseBehaviorContinue
		}
		return NormalizeVoiceAgentCloseBehavior(fallback, VoiceAgentCloseBehaviorContinue)
	}
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
// Empty resolves to LiveKit/openWakeWord because the bundled per-phrase
// ONNX models (hey_quby/hey_mira/hey_kombify/hey_jarvis/hey_computer) are
// purpose-trained for those exact phrases and significantly more reliable
// than the generic Gigaspeech sherpa-onnx KWS for the curated catalog.
// Existing installs that pinned "sherpa_kws" keep it; only fresh configs
// and unset fields land on openWakeWord.
func NormalizeWakewordBackend(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case WakewordBackendSherpaKWS:
		return WakewordBackendSherpaKWS
	case WakewordBackendLiveKitOpenWakeWord, "", "livekit", "openwakeword", "livekit_openwakeword_onnx":
		return WakewordBackendLiveKitOpenWakeWord
	case WakewordBackendSTTPhrase, "stt", "phrase_match", "stt_phrase_match":
		return WakewordBackendSTTPhrase
	default:
		return WakewordBackendLiveKitOpenWakeWord
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
