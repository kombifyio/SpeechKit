package main

import (
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/hotkey"
)

const (
	modeNone       = "none"
	modeDictate    = "dictate"
	modeAssist     = "assist"
	modeVoiceAgent = "voice_agent"
	modeAgent      = "agent"
)

func orderedRuntimeModes() []string {
	return []string{modeDictate, modeAssist, modeVoiceAgent}
}

func isRuntimeMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case modeNone, modeDictate, modeAssist, modeVoiceAgent:
		return true
	default:
		return false
	}
}

func normalizeAgentMode(mode string) string {
	if strings.TrimSpace(mode) == modeVoiceAgent {
		return modeVoiceAgent
	}
	return modeAssist
}

func normalizeRuntimeMode(mode string, legacyAgentMode string) string {
	trimmed := strings.TrimSpace(mode)
	switch trimmed {
	case modeDictate, modeAssist, modeVoiceAgent, modeNone:
		return trimmed
	case modeAgent:
		return normalizeAgentMode(legacyAgentMode)
	default:
		return modeNone
	}
}

func normalizeActiveMode(mode string, legacyAgentMode string) string {
	normalized := normalizeRuntimeMode(mode, legacyAgentMode)
	if normalized == modeNone {
		return modeDictate
	}
	return normalized
}

func deriveLegacyAgentModeFromBindings(assistHotkey, voiceAgentHotkey, activeMode, fallback string) string {
	if activeMode == modeVoiceAgent && strings.TrimSpace(voiceAgentHotkey) != "" {
		return modeVoiceAgent
	}
	if activeMode == modeAssist && strings.TrimSpace(assistHotkey) != "" {
		return modeAssist
	}
	if strings.TrimSpace(assistHotkey) != "" {
		return modeAssist
	}
	if strings.TrimSpace(voiceAgentHotkey) != "" {
		return modeVoiceAgent
	}
	return normalizeAgentMode(fallback)
}

func sanitizeActiveModeForBindings(mode, legacyAgentMode, dictateHotkey, assistHotkey, voiceAgentHotkey string) string {
	normalized := normalizeRuntimeMode(mode, legacyAgentMode)
	if normalized == modeNone {
		return modeNone
	}
	if _, ok := configuredModeBindings(dictateHotkey, assistHotkey, voiceAgentHotkey)[normalized]; !ok {
		return modeNone
	}
	return normalized
}

func activeModeHotkey(state runtimeState) string {
	switch state.activeMode {
	case modeAssist:
		return strings.TrimSpace(state.assistHotkey)
	case modeVoiceAgent:
		return strings.TrimSpace(state.voiceAgentHotkey)
	case modeNone:
		return ""
	default:
		return strings.TrimSpace(state.dictateHotkey)
	}
}

func legacyAgentHotkeyFromModeBindings(assistHotkey, voiceAgentHotkey, legacyAgentMode string) string {
	if normalizeAgentMode(legacyAgentMode) == modeVoiceAgent {
		return strings.TrimSpace(voiceAgentHotkey)
	}
	return strings.TrimSpace(assistHotkey)
}

func configuredModeBindings(dictateHotkey, assistHotkey, voiceAgentHotkey string) map[string]string {
	bindings := make(map[string]string, 3)
	if trimmed := strings.TrimSpace(dictateHotkey); trimmed != "" {
		bindings[modeDictate] = trimmed
	}
	if trimmed := strings.TrimSpace(assistHotkey); trimmed != "" {
		bindings[modeAssist] = trimmed
	}
	if trimmed := strings.TrimSpace(voiceAgentHotkey); trimmed != "" {
		bindings[modeVoiceAgent] = trimmed
	}
	return bindings
}

func configuredModeCombos(dictateHotkey, assistHotkey, voiceAgentHotkey string) map[string][]uint32 {
	bindings := configuredModeBindings(dictateHotkey, assistHotkey, voiceAgentHotkey)
	combos := make(map[string][]uint32, len(bindings))
	for mode, binding := range bindings {
		combos[mode] = parseConfiguredHotkeyCombo(binding)
	}
	return combos
}

func parseConfiguredHotkeyCombo(binding string) []uint32 {
	if strings.TrimSpace(binding) == "" {
		return nil
	}
	return hotkey.ParseCombo(binding)
}

func validateDistinctModeHotkeys(dictateHotkey, assistHotkey, voiceAgentHotkey string) bool {
	seen := map[string]string{}
	for mode, binding := range configuredModeBindings(dictateHotkey, assistHotkey, voiceAgentHotkey) {
		if existingMode, ok := seen[binding]; ok && existingMode != mode {
			return false
		}
		seen[binding] = mode
	}
	return true
}

func runtimeModeLabel(mode string) string {
	switch mode {
	case modeAssist:
		return "Assist"
	case modeVoiceAgent:
		return "Voice Agent"
	case modeNone:
		return "None"
	default:
		return "Dictate"
	}
}

func normalizeConfigModes(cfg *config.Config) {
	if cfg == nil {
		return
	}
	cfg.General.AgentMode = normalizeAgentMode(cfg.General.AgentMode)
	cfg.General.ActiveMode = normalizeRuntimeMode(cfg.General.ActiveMode, cfg.General.AgentMode)
	cfg.General.AgentHotkey = legacyAgentHotkeyFromModeBindings(cfg.General.AssistHotkey, cfg.General.VoiceAgentHotkey, cfg.General.AgentMode)
}
