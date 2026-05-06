package config

import "strings"

func NormalizeHotkeyBehavior(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case HotkeyBehaviorPushToTalk:
		return HotkeyBehaviorPushToTalk
	case HotkeyBehaviorToggle:
		return HotkeyBehaviorToggle
	default:
		if strings.TrimSpace(fallback) == "" {
			return HotkeyBehaviorPushToTalk
		}
		if strings.EqualFold(strings.TrimSpace(fallback), value) {
			return HotkeyBehaviorPushToTalk
		}
		return NormalizeHotkeyBehavior(fallback, HotkeyBehaviorPushToTalk)
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
