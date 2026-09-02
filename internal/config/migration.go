package config

import (
	"errors"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/hostconfig"
)

func rejectRemovedConfigAliases(meta toml.MetaData) error {
	if meta.IsDefined("voice_agent", "instruction") {
		return errors.New("config [voice_agent].instruction was removed; use [voice_agent].framework_prompt")
	}
	return nil
}

// backfillLegacyModeHotkeys folds the removed [general] hotkey keys into the
// per-mode fields. The shared semantics live in hostconfig.Normalize so the
// desktop app and library embedders read the same file identically; only the
// write-back of the legacy fields themselves stays here, since the public
// Config no longer carries them.
func backfillLegacyModeHotkeys(meta toml.MetaData, cfg *Config) {
	if cfg == nil {
		return
	}
	legacyAgentMode := strings.TrimSpace(cfg.General.AgentMode)
	if legacyAgentMode != "voice_agent" {
		legacyAgentMode = "assist"
	}

	applySharedNormalization(meta, cfg)

	if cfg.General.AgentHotkey == "" {
		cfg.General.AgentHotkey = cfg.LegacyAgentHotkey()
	}
	if cfg.General.AgentMode == "" {
		cfg.General.AgentMode = legacyAgentMode
	}
	cfg.General.HotkeyMode = NormalizeHotkeyBehavior(cfg.General.HotkeyMode, cfg.General.DictateHotkeyBehavior)
}

// applySharedNormalization runs hostconfig.Normalize over the tables the
// public loader owns and copies the result back into the internal Config.
func applySharedNormalization(meta toml.MetaData, cfg *Config) {
	shared := sharedView(cfg)
	legacy := hostconfig.LegacyGeneral{
		Hotkey:      cfg.General.Hotkey,
		AgentHotkey: cfg.General.AgentHotkey,
		AgentMode:   cfg.General.AgentMode,
		HotkeyMode:  cfg.General.HotkeyMode,
	}
	dictateBefore := cfg.General.DictateHotkey

	hostconfig.Normalize(&shared, meta.IsDefined, legacy)
	applySharedView(cfg, shared)

	// The swapped-default migration is the only path that rewrites a set
	// dictate hotkey, so observing that rewrite identifies it exactly.
	swapped := sameCombo(dictateBefore, hostconfig.DefaultAssistHotkey) &&
		sameCombo(cfg.General.DictateHotkey, hostconfig.DefaultDictateHotkey)
	if swapped {
		// The public loader moved the pre-v0.30 swapped triple onto the
		// current defaults; keep the legacy mirrors pointing at the same keys.
		if blankOrCombo(cfg.General.Hotkey, "win+alt") {
			cfg.General.Hotkey = cfg.General.DictateHotkey
		}
		if blankOrCombo(cfg.General.AgentHotkey, "ctrl+win") &&
			(cfg.General.AgentMode == "" || cfg.General.AgentMode == "assist") {
			cfg.General.AgentHotkey = cfg.General.AssistHotkey
		}
	}
}

func sharedView(cfg *Config) hostconfig.Config {
	return hostconfig.Config{
		General: hostconfig.General{
			DictateEnabled:           cfg.General.DictateEnabled,
			AssistEnabled:            cfg.General.AssistEnabled,
			VoiceAgentEnabled:        cfg.General.VoiceAgentEnabled,
			DictateHotkey:            cfg.General.DictateHotkey,
			AssistHotkey:             cfg.General.AssistHotkey,
			VoiceAgentHotkey:         cfg.General.VoiceAgentHotkey,
			DictateHotkeyBehavior:    cfg.General.DictateHotkeyBehavior,
			AssistHotkeyBehavior:     cfg.General.AssistHotkeyBehavior,
			VoiceAgentHotkeyBehavior: cfg.General.VoiceAgentHotkeyBehavior,
		},
		ModelSelection: hostconfig.ModelSelection{
			Dictate:    sharedModeSelection(cfg.ModelSelection.Dictate),
			Assist:     sharedModeSelection(cfg.ModelSelection.Assist),
			VoiceAgent: sharedModeSelection(cfg.ModelSelection.VoiceAgent),
		},
		Vocabulary: hostconfig.Vocabulary{Dictionary: cfg.Vocabulary.Dictionary},
		TTS:        hostconfig.TTS{Enabled: cfg.TTS.Enabled},
		VoiceAgent: hostconfig.VoiceAgent{
			EnableSessionSummary: cfg.VoiceAgent.EnableSessionSummary,
			PipelineFallback:     cfg.VoiceAgent.PipelineFallback,
			CloseBehavior:        cfg.VoiceAgent.CloseBehavior,
			AgentProfileID:       cfg.VoiceAgent.AgentProfileID,
			AgentSequenceID:      cfg.VoiceAgent.AgentSequenceID,
		},
		ServerConnection: hostconfig.ServerConnection{
			Enabled:              cfg.ServerConnection.Enabled,
			URL:                  cfg.ServerConnection.URL,
			BearerTokenEnv:       cfg.ServerConnection.BearerTokenEnv,
			AuthMode:             cfg.ServerConnection.AuthMode,
			BetaInstallIDEnv:     cfg.ServerConnection.BetaInstallIDEnv,
			BetaInstallSecretEnv: cfg.ServerConnection.BetaInstallSecretEnv,
			FallbackToLocal:      cfg.ServerConnection.FallbackToLocal,
			RequestTimeoutSec:    cfg.ServerConnection.RequestTimeoutSec,
		},
	}
}

func sharedModeSelection(sel ModeModelSelection) hostconfig.ModeSelection {
	return hostconfig.ModeSelection{
		PrimaryProfileID:  sel.PrimaryProfileID,
		FallbackProfileID: sel.FallbackProfileID,
		ModeSource:        sel.ModeSource,
	}
}

func applySharedView(cfg *Config, shared hostconfig.Config) {
	cfg.General.DictateEnabled = shared.General.DictateEnabled
	cfg.General.AssistEnabled = shared.General.AssistEnabled
	cfg.General.VoiceAgentEnabled = shared.General.VoiceAgentEnabled
	cfg.General.DictateHotkey = shared.General.DictateHotkey
	cfg.General.AssistHotkey = shared.General.AssistHotkey
	cfg.General.VoiceAgentHotkey = shared.General.VoiceAgentHotkey
	cfg.General.DictateHotkeyBehavior = shared.General.DictateHotkeyBehavior
	cfg.General.AssistHotkeyBehavior = shared.General.AssistHotkeyBehavior
	cfg.General.VoiceAgentHotkeyBehavior = shared.General.VoiceAgentHotkeyBehavior

	cfg.ModelSelection.Dictate.ModeSource = shared.ModelSelection.Dictate.ModeSource
	cfg.ModelSelection.Assist.ModeSource = shared.ModelSelection.Assist.ModeSource
	cfg.ModelSelection.VoiceAgent.ModeSource = shared.ModelSelection.VoiceAgent.ModeSource

	cfg.VoiceAgent.EnableSessionSummary = shared.VoiceAgent.EnableSessionSummary
	cfg.VoiceAgent.CloseBehavior = shared.VoiceAgent.CloseBehavior
	cfg.VoiceAgent.AgentSequenceID = shared.VoiceAgent.AgentSequenceID
	// AgentProfileID is intentionally not copied back: the desktop app resolves
	// it against its behaviour catalog (voiceagentprofile.NormalizeID) later in
	// Load, which the public loader deliberately does not do.

	cfg.ServerConnection.AuthMode = shared.ServerConnection.AuthMode
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

	// v0.48 splits the old ambiguous "auto-start" preference into two
	// explicit controls: Windows login start and dashboard auto-open after the
	// app process starts. If a user had enabled the old setting and has not yet
	// written the new field, carry that intent into the real login-start flag.
	if !meta.IsDefined("general", "start_at_login") {
		cfg.General.StartAtLogin = cfg.General.AutoStartOnLaunch
	}
}

func backfillVoiceAgentPromptLayers(cfg *Config) {
	if cfg == nil {
		return
	}

	cfg.VoiceAgent.FrameworkPrompt = strings.TrimSpace(cfg.VoiceAgent.FrameworkPrompt)
	cfg.VoiceAgent.RefinementPrompt = strings.TrimSpace(cfg.VoiceAgent.RefinementPrompt)
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
