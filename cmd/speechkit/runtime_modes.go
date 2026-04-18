package main

import (
	"fmt"
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

var allowedModeHotkeyBases = []string{"win+alt", "ctrl+win", "ctrl+shift"}

var allowedModeHotkeySuffixes = map[string]struct{}{
	"":      {},
	"d":     {},
	"j":     {},
	"k":     {},
	"v":     {},
	"space": {},
}

type parsedModeHotkey struct {
	Base   string
	Suffix string
	Raw    string
}

func orderedRuntimeModes() []string {
	return []string{modeDictate, modeAssist, modeVoiceAgent}
}

func normalizeAgentMode(mode string) string {
	if strings.TrimSpace(mode) == modeVoiceAgent {
		return modeVoiceAgent
	}
	return modeAssist
}

func normalizeRuntimeMode(mode, legacyAgentMode string) string {
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

func sanitizeActiveModeForBindings(mode, legacyAgentMode string, dictateEnabled, assistEnabled, voiceAgentEnabled bool, dictateHotkey, assistHotkey, voiceAgentHotkey string) string {
	normalized := normalizeRuntimeMode(mode, legacyAgentMode)
	if normalized == modeNone {
		return modeNone
	}
	if _, ok := configuredModeBindings(dictateEnabled, assistEnabled, voiceAgentEnabled, dictateHotkey, assistHotkey, voiceAgentHotkey)[normalized]; !ok {
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

func configuredModeBindings(dictateEnabled, assistEnabled, voiceAgentEnabled bool, dictateHotkey, assistHotkey, voiceAgentHotkey string) map[string]string {
	bindings := make(map[string]string, 3)
	if modeBindingAvailable(dictateEnabled, dictateHotkey) {
		trimmed := strings.TrimSpace(dictateHotkey)
		bindings[modeDictate] = trimmed
	}
	if modeBindingAvailable(assistEnabled, assistHotkey) {
		trimmed := strings.TrimSpace(assistHotkey)
		bindings[modeAssist] = trimmed
	}
	if modeBindingAvailable(voiceAgentEnabled, voiceAgentHotkey) {
		trimmed := strings.TrimSpace(voiceAgentHotkey)
		bindings[modeVoiceAgent] = trimmed
	}
	return bindings
}

func configuredModeCombos(dictateEnabled, assistEnabled, voiceAgentEnabled bool, dictateHotkey, assistHotkey, voiceAgentHotkey string) map[string][]uint32 {
	bindings := configuredModeBindings(dictateEnabled, assistEnabled, voiceAgentEnabled, dictateHotkey, assistHotkey, voiceAgentHotkey)
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

func validateDistinctModeHotkeys(_, _, _ bool, dictateHotkey, assistHotkey, voiceAgentHotkey string) bool {
	seen := map[string]string{}
	for mode, binding := range map[string]string{
		modeDictate:    strings.TrimSpace(dictateHotkey),
		modeAssist:     strings.TrimSpace(assistHotkey),
		modeVoiceAgent: strings.TrimSpace(voiceAgentHotkey),
	} {
		if strings.TrimSpace(binding) == "" {
			continue
		}
		parsed, err := parseModeHotkey(binding)
		if err != nil {
			return false
		}
		if existingMode, ok := seen[parsed.Base]; ok && existingMode != mode {
			return false
		}
		seen[parsed.Base] = mode
	}
	return true
}

func normalizeConfigModes(cfg *config.Config) {
	if cfg == nil {
		return
	}
	cfg.General.AgentMode = normalizeAgentMode(cfg.General.AgentMode)
	cfg.General.ActiveMode = normalizeRuntimeMode(cfg.General.ActiveMode, cfg.General.AgentMode)
	cfg.General.DictateHotkeyBehavior = config.NormalizeHotkeyBehavior(cfg.General.DictateHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	cfg.General.AssistHotkeyBehavior = config.NormalizeHotkeyBehavior(cfg.General.AssistHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	cfg.General.VoiceAgentHotkeyBehavior = config.NormalizeHotkeyBehavior(cfg.General.VoiceAgentHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	cfg.General.HotkeyMode = config.NormalizeHotkeyBehavior(cfg.General.HotkeyMode, cfg.General.DictateHotkeyBehavior)
	cfg.General.AgentHotkey = legacyAgentHotkeyFromModeBindings(cfg.General.AssistHotkey, cfg.General.VoiceAgentHotkey, cfg.General.AgentMode)
}

func modeBindingAvailable(enabled bool, binding string) bool {
	return enabled && strings.TrimSpace(binding) != ""
}

func normalizeModeHotkeyBinding(binding string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(binding), " ", ""))
}

func parseModeHotkey(binding string) (parsedModeHotkey, error) {
	normalized := normalizeModeHotkeyBinding(binding)
	if normalized == "" {
		return parsedModeHotkey{}, nil
	}

	for _, base := range allowedModeHotkeyBases {
		if normalized == base {
			return parsedModeHotkey{Base: base, Raw: normalized}, nil
		}
		prefix := base + "+"
		if strings.HasPrefix(normalized, prefix) {
			suffix := strings.TrimPrefix(normalized, prefix)
			if _, ok := allowedModeHotkeySuffixes[suffix]; !ok {
				return parsedModeHotkey{}, fmt.Errorf("unsupported suffix")
			}
			return parsedModeHotkey{Base: base, Suffix: suffix, Raw: normalized}, nil
		}
	}

	return parsedModeHotkey{}, fmt.Errorf("unsupported base")
}
