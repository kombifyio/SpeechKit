package main

import (
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func parseModeSettingsForm(req *http.Request, cfg *config.Config, f *settingsFormData) {
	hasDictateHotkey := postFormIncludes(req, "dictate_hotkey")
	hasAssistHotkey := postFormIncludes(req, "assist_hotkey")
	hasVoiceAgentHotkey := postFormIncludes(req, "voice_agent_hotkey")

	f.DictateHotkey = resolveDictateHotkey(req, cfg, hasDictateHotkey)
	f.DictateEnabled = boolFormValue(req, "dictate_enabled", cfg.General.DictateEnabled)
	f.AssistEnabled = boolFormValue(req, "assist_enabled", cfg.General.AssistEnabled)
	f.VoiceAgentEnabled = boolFormValue(req, "voice_agent_enabled", cfg.General.VoiceAgentEnabled)
	f.AssistHotkey = trimmedFormValue(req, "assist_hotkey")
	f.VoiceAgentHotkey = trimmedFormValue(req, "voice_agent_hotkey")
	f.AgentMode = normalizeAgentMode(valueOrDefault(trimmedFormValue(req, "agent_mode"), cfg.General.AgentMode))
	f.AgentHotkey = trimmedFormValue(req, "agent_hotkey")

	applyLegacyAgentHotkey(req, cfg, f, hasAssistHotkey, hasVoiceAgentHotkey)
	disableModesWithoutHotkeys(f)
	parseModeHotkeyBehaviors(req, cfg, f)
}

func resolveDictateHotkey(req *http.Request, cfg *config.Config, hasDictateHotkey bool) string {
	if hasDictateHotkey {
		return trimmedFormValue(req, "dictate_hotkey")
	}
	if hotkey := trimmedFormValue(req, "hotkey"); hotkey != "" {
		return hotkey
	}
	if hotkey := strings.TrimSpace(cfg.General.DictateHotkey); hotkey != "" {
		return hotkey
	}
	return strings.TrimSpace(cfg.General.Hotkey)
}

func applyLegacyAgentHotkey(req *http.Request, cfg *config.Config, f *settingsFormData, hasAssistHotkey, hasVoiceAgentHotkey bool) {
	legacyAgentHotkeyPosted := !hasAssistHotkey && !hasVoiceAgentHotkey && postFormIncludes(req, "agent_hotkey")
	if legacyAgentHotkeyPosted {
		f.AssistHotkey = ""
		f.VoiceAgentHotkey = ""
		if f.AgentMode == modeVoiceAgent {
			f.VoiceAgentHotkey = f.AgentHotkey
			return
		}
		f.AssistHotkey = f.AgentHotkey
		return
	}
	if !hasAssistHotkey && f.AssistHotkey == "" {
		f.AssistHotkey = strings.TrimSpace(cfg.General.AssistHotkey)
	}
	if !hasVoiceAgentHotkey && f.VoiceAgentHotkey == "" {
		f.VoiceAgentHotkey = strings.TrimSpace(cfg.General.VoiceAgentHotkey)
	}
}

func disableModesWithoutHotkeys(f *settingsFormData) {
	if strings.TrimSpace(f.DictateHotkey) == "" {
		f.DictateEnabled = false
	}
	if strings.TrimSpace(f.AssistHotkey) == "" {
		f.AssistEnabled = false
	}
	if strings.TrimSpace(f.VoiceAgentHotkey) == "" {
		f.VoiceAgentEnabled = false
	}
}

func parseModeHotkeyBehaviors(req *http.Request, cfg *config.Config, f *settingsFormData) {
	f.DictateHotkeyBehavior = config.NormalizeHotkeyBehavior(
		req.FormValue("dictate_hotkey_behavior"),
		config.NormalizeHotkeyBehavior(cfg.General.DictateHotkeyBehavior, config.HotkeyBehaviorHoldToTalk),
	)
	f.AssistHotkeyBehavior = config.NormalizeHotkeyBehavior(
		req.FormValue("assist_hotkey_behavior"),
		config.NormalizeHotkeyBehavior(cfg.General.AssistHotkeyBehavior, config.HotkeyBehaviorHoldToTalk),
	)
	f.VoiceAgentHotkeyBehavior = config.NormalizeHotkeyBehavior(
		req.FormValue("voice_agent_hotkey_behavior"),
		config.NormalizeHotkeyBehavior(cfg.General.VoiceAgentHotkeyBehavior, config.HotkeyBehaviorHoldToTalk),
	)
}

func validateModeSettingsForm(req *http.Request, cfg *config.Config, f *settingsFormData) string {
	for _, binding := range []string{f.DictateHotkey, f.AssistHotkey, f.VoiceAgentHotkey} {
		if strings.TrimSpace(binding) == "" {
			continue
		}
		if _, err := parseModeHotkey(binding); err != nil {
			return msgUnsupportedModeHotkey
		}
	}

	f.AgentHotkey = legacyAgentHotkeyFromModeBindings(f.AssistHotkey, f.VoiceAgentHotkey, f.AgentMode)
	activeMode := trimmedFormValue(req, "active_mode")
	if activeMode == "" && !postFormIncludes(req, "active_mode") {
		activeMode = cfg.General.ActiveMode
	}
	f.ActiveMode = sanitizeActiveModeForBindings(
		activeMode,
		f.AgentMode,
		f.DictateEnabled,
		f.AssistEnabled,
		f.VoiceAgentEnabled,
		f.DictateHotkey,
		f.AssistHotkey,
		f.VoiceAgentHotkey,
	)
	if validateDistinctModeHotkeys(
		f.DictateEnabled,
		f.AssistEnabled,
		f.VoiceAgentEnabled,
		f.DictateHotkey,
		f.AssistHotkey,
		f.VoiceAgentHotkey,
	) {
		return ""
	}
	return msgDuplicateHotkeys
}
