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

// NormalizeWakewordThreshold clamps the threshold to a sane range. Values
// outside (0, 1] are coerced to the published sweet-spot of 0.68.
func NormalizeWakewordThreshold(value float64) float64 {
	if value <= 0 || value > 1 {
		return 0.68
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
