package runtime

import "strings"

const (
	ModeNone       = "none"
	ModeDictate    = "dictate"
	ModeAssist     = "assist"
	ModeVoiceAgent = "voice_agent"
	ModeAgent      = "agent"
)

func OrderedModes() []string {
	return []string{ModeDictate, ModeAssist, ModeVoiceAgent}
}

func NormalizeAgentMode(mode string) string {
	if strings.TrimSpace(mode) == ModeVoiceAgent {
		return ModeVoiceAgent
	}
	return ModeAssist
}

func NormalizeMode(mode, legacyAgentMode string) string {
	trimmed := strings.TrimSpace(mode)
	switch trimmed {
	case ModeDictate, ModeAssist, ModeVoiceAgent, ModeNone:
		return trimmed
	case ModeAgent:
		return NormalizeAgentMode(legacyAgentMode)
	default:
		return ModeNone
	}
}

func DeriveLegacyAgentMode(assistHotkey, voiceAgentHotkey, activeMode, fallback string) string {
	if activeMode == ModeVoiceAgent && strings.TrimSpace(voiceAgentHotkey) != "" {
		return ModeVoiceAgent
	}
	if activeMode == ModeAssist && strings.TrimSpace(assistHotkey) != "" {
		return ModeAssist
	}
	if strings.TrimSpace(assistHotkey) != "" {
		return ModeAssist
	}
	if strings.TrimSpace(voiceAgentHotkey) != "" {
		return ModeVoiceAgent
	}
	return NormalizeAgentMode(fallback)
}

func LegacyAgentHotkey(assistHotkey, voiceAgentHotkey, legacyAgentMode string) string {
	if NormalizeAgentMode(legacyAgentMode) == ModeVoiceAgent {
		return strings.TrimSpace(voiceAgentHotkey)
	}
	return strings.TrimSpace(assistHotkey)
}
