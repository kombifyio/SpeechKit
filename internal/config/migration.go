package config

import (
	"strings"

	"github.com/BurntSushi/toml"
)

func backfillLegacyModeHotkeys(meta toml.MetaData, cfg *Config) {
	if cfg == nil {
		return
	}

	if strings.TrimSpace(cfg.General.DictateHotkey) == "" {
		cfg.General.DictateHotkey = strings.TrimSpace(cfg.General.Hotkey)
	}
	if strings.TrimSpace(cfg.General.DictateHotkey) == "" {
		cfg.General.DictateHotkey = "ctrl+win"
	}

	legacyAgentHotkey := strings.TrimSpace(cfg.General.AgentHotkey)
	legacyAgentMode := strings.TrimSpace(cfg.General.AgentMode)
	if legacyAgentMode != "voice_agent" {
		legacyAgentMode = "assist"
	}

	if !meta.IsDefined("general", "assist_hotkey") && strings.TrimSpace(cfg.General.AssistHotkey) == "" && legacyAgentMode == "assist" {
		cfg.General.AssistHotkey = legacyAgentHotkey
	}
	if !meta.IsDefined("general", "voice_agent_hotkey") && strings.TrimSpace(cfg.General.VoiceAgentHotkey) == "" && legacyAgentMode == "voice_agent" {
		cfg.General.VoiceAgentHotkey = legacyAgentHotkey
	}

	cfg.General.AssistHotkey = strings.TrimSpace(cfg.General.AssistHotkey)
	cfg.General.VoiceAgentHotkey = strings.TrimSpace(cfg.General.VoiceAgentHotkey)
	migrateOldBuiltInHotkeyDefaults(cfg)
	if cfg.General.AgentHotkey == "" {
		cfg.General.AgentHotkey = cfg.LegacyAgentHotkey()
	}
	if cfg.General.AgentMode == "" {
		cfg.General.AgentMode = legacyAgentMode
	}
	if !meta.IsDefined("general", "dictate_enabled") {
		cfg.General.DictateEnabled = strings.TrimSpace(cfg.General.DictateHotkey) != ""
	}
	if !meta.IsDefined("general", "assist_enabled") {
		cfg.General.AssistEnabled = strings.TrimSpace(cfg.General.AssistHotkey) != ""
	}
	if !meta.IsDefined("general", "voice_agent_enabled") {
		cfg.General.VoiceAgentEnabled = strings.TrimSpace(cfg.General.VoiceAgentHotkey) != ""
	}

	legacyHotkeyMode := NormalizeHotkeyBehavior(cfg.General.HotkeyMode, HotkeyBehaviorPushToTalk)
	legacyHotkeyModeDefined := meta.IsDefined("general", "hotkey_mode")
	if legacyHotkeyModeDefined && !meta.IsDefined("general", "dictate_hotkey_behavior") {
		cfg.General.DictateHotkeyBehavior = legacyHotkeyMode
	}
	if legacyHotkeyModeDefined && !meta.IsDefined("general", "assist_hotkey_behavior") {
		cfg.General.AssistHotkeyBehavior = legacyHotkeyMode
	}
	if legacyHotkeyModeDefined && !meta.IsDefined("general", "voice_agent_hotkey_behavior") {
		cfg.General.VoiceAgentHotkeyBehavior = legacyHotkeyMode
	}

	cfg.General.DictateHotkeyBehavior = NormalizeHotkeyBehavior(cfg.General.DictateHotkeyBehavior, HotkeyBehaviorPushToTalk)
	cfg.General.AssistHotkeyBehavior = NormalizeHotkeyBehavior(cfg.General.AssistHotkeyBehavior, HotkeyBehaviorPushToTalk)
	cfg.General.VoiceAgentHotkeyBehavior = NormalizeHotkeyBehavior(cfg.General.VoiceAgentHotkeyBehavior, HotkeyBehaviorPushToTalk)
	cfg.General.HotkeyMode = NormalizeHotkeyBehavior(cfg.General.HotkeyMode, cfg.General.DictateHotkeyBehavior)
}

func migrateOldBuiltInHotkeyDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	if !sameCombo(cfg.General.DictateHotkey, "win+alt") ||
		!sameCombo(cfg.General.AssistHotkey, "ctrl+win") ||
		!sameCombo(cfg.General.VoiceAgentHotkey, "ctrl+shift") {
		return
	}

	cfg.General.DictateHotkey = "ctrl+win"
	cfg.General.AssistHotkey = "win+alt"
	cfg.General.VoiceAgentHotkey = "ctrl+shift"

	if blankOrCombo(cfg.General.Hotkey, "win+alt") {
		cfg.General.Hotkey = cfg.General.DictateHotkey
	}
	if blankOrCombo(cfg.General.AgentHotkey, "ctrl+win") &&
		(cfg.General.AgentMode == "" || cfg.General.AgentMode == "assist") {
		cfg.General.AgentHotkey = cfg.General.AssistHotkey
	}
}

func sameCombo(value, combo string) bool {
	return strings.EqualFold(strings.TrimSpace(value), combo)
}

func blankOrCombo(value, combo string) bool {
	return strings.TrimSpace(value) == "" || sameCombo(value, combo)
}

func backfillStartupBehavior(meta toml.MetaData, cfg *Config) {
	if cfg == nil {
		return
	}

	switch {
	case meta.IsDefined("general", "auto_start_on_launch"):
		cfg.VoiceAgent.AutoStartOnLaunch = cfg.General.AutoStartOnLaunch
	case meta.IsDefined("voice_agent", "auto_start_on_launch"):
		cfg.General.AutoStartOnLaunch = cfg.VoiceAgent.AutoStartOnLaunch
	default:
		cfg.VoiceAgent.AutoStartOnLaunch = cfg.General.AutoStartOnLaunch
	}
}

func backfillVoiceAgentPromptLayers(meta toml.MetaData, cfg *Config) {
	if cfg == nil {
		return
	}

	frameworkPrompt := strings.TrimSpace(cfg.VoiceAgent.FrameworkPrompt)
	legacyInstruction := strings.TrimSpace(cfg.VoiceAgent.Instruction)

	if frameworkPrompt == "" && !meta.IsDefined("voice_agent", "framework_prompt") {
		frameworkPrompt = legacyInstruction
	}
	if legacyInstruction == "" && frameworkPrompt != "" {
		legacyInstruction = frameworkPrompt
	}

	cfg.VoiceAgent.FrameworkPrompt = frameworkPrompt
	cfg.VoiceAgent.RefinementPrompt = strings.TrimSpace(cfg.VoiceAgent.RefinementPrompt)
	cfg.VoiceAgent.Instruction = legacyInstruction
}

func backfillVoiceAgentSessionSummary(meta toml.MetaData, cfg *Config) {
	if cfg == nil {
		return
	}
	if !meta.IsDefined("voice_agent", "enable_session_summary") {
		cfg.VoiceAgent.EnableSessionSummary = true
	}
}

func backfillLegacyAssistModels(meta toml.MetaData, cfg *Config) {
	if cfg == nil {
		return
	}

	backfillLegacyAssistField(!meta.IsDefined("huggingface", "assist_model"), meta.IsDefined("huggingface", "agent_model"), &cfg.HuggingFace.AssistModel, cfg.HuggingFace.AgentModel)
	backfillLegacyAssistField(!meta.IsDefined("providers", "openai", "assist_model"), meta.IsDefined("providers", "openai", "agent_model"), &cfg.Providers.OpenAI.AssistModel, cfg.Providers.OpenAI.AgentModel)
	backfillLegacyAssistField(!meta.IsDefined("providers", "groq", "assist_model"), meta.IsDefined("providers", "groq", "agent_model"), &cfg.Providers.Groq.AssistModel, cfg.Providers.Groq.AgentModel)
	backfillLegacyAssistField(!meta.IsDefined("providers", "google", "assist_model"), meta.IsDefined("providers", "google", "agent_model"), &cfg.Providers.Google.AssistModel, cfg.Providers.Google.AgentModel)
	backfillLegacyAssistField(!meta.IsDefined("providers", "ollama", "assist_model"), meta.IsDefined("providers", "ollama", "agent_model"), &cfg.Providers.Ollama.AssistModel, cfg.Providers.Ollama.AgentModel)
	backfillLegacyAssistField(!meta.IsDefined("providers", "openrouter", "assist_model"), meta.IsDefined("providers", "openrouter", "agent_model"), &cfg.Providers.OpenRouter.AssistModel, cfg.Providers.OpenRouter.AgentModel)
	backfillLegacyAssistField(!meta.IsDefined("local_llm", "assist_model"), meta.IsDefined("local_llm", "agent_model"), &cfg.LocalLLM.AssistModel, cfg.LocalLLM.AgentModel)
}

func backfillLegacyAssistField(assistMissing, legacyAgentDefined bool, assistValue *string, legacyAgentValue string) {
	if !assistMissing || !legacyAgentDefined || assistValue == nil {
		return
	}
	if legacyAgentValue = strings.TrimSpace(legacyAgentValue); legacyAgentValue != "" {
		*assistValue = legacyAgentValue
	}
}
