package main

import (
	"github.com/kombifyio/SpeechKit/cmd/speechkit/internal/profiles"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

func buildNextConfig(form settingsFormData, cfg *config.Config) config.Config {
	hfAvailableInBuild := config.ManagedHuggingFaceAvailableInBuild()
	nextCfg := *cfg
	nextCfg.General.Hotkey = form.DictateHotkey // keep legacy field in sync
	nextCfg.General.DictateHotkey = form.DictateHotkey
	nextCfg.General.AssistHotkey = form.AssistHotkey
	nextCfg.General.VoiceAgentHotkey = form.VoiceAgentHotkey
	nextCfg.General.DictateHotkeyBehavior = config.NormalizeHotkeyBehavior(form.DictateHotkeyBehavior, config.HotkeyBehaviorHoldToTalk)
	nextCfg.General.AssistHotkeyBehavior = config.NormalizeHotkeyBehavior(form.AssistHotkeyBehavior, config.HotkeyBehaviorHoldToTalk)
	nextCfg.General.VoiceAgentHotkeyBehavior = config.NormalizeHotkeyBehavior(form.VoiceAgentHotkeyBehavior, config.HotkeyBehaviorHoldToTalk)
	nextCfg.General.DictateEnabled = form.DictateEnabled
	nextCfg.General.AssistEnabled = form.AssistEnabled
	nextCfg.General.VoiceAgentEnabled = form.VoiceAgentEnabled
	nextCfg.General.ActiveMode = sanitizeActiveModeForBindings(
		form.ActiveMode,
		form.AgentMode,
		form.DictateEnabled,
		form.AssistEnabled,
		form.VoiceAgentEnabled,
		form.DictateHotkey,
		form.AssistHotkey,
		form.VoiceAgentHotkey,
	)
	nextCfg.General.AgentMode = deriveLegacyAgentModeFromBindings(form.AssistHotkey, form.VoiceAgentHotkey, nextCfg.General.ActiveMode, form.AgentMode)
	nextCfg.General.AgentHotkey = legacyAgentHotkeyFromModeBindings(form.AssistHotkey, form.VoiceAgentHotkey, nextCfg.General.AgentMode)
	nextCfg.General.HotkeyMode = nextCfg.General.DictateHotkeyBehavior
	nextCfg.ModelSelection.Dictate = buildNextModeSelection(form.DictatePrimaryProfileID, form.DictateFallbackProfileID, cfg.ModelSelection.Dictate)
	nextCfg.ModelSelection.Assist = buildNextModeSelection(form.AssistPrimaryProfileID, form.AssistFallbackProfileID, cfg.ModelSelection.Assist)
	nextCfg.ModelSelection.VoiceAgent = buildNextModeSelection(form.VoicePrimaryProfileID, form.VoiceFallbackProfileID, cfg.ModelSelection.VoiceAgent)
	nextCfg.VoiceAgent.RefinementPrompt = form.VoiceAgentRefinementPrompt
	nextCfg.VoiceAgent.AgentProfileID = voiceagentprofile.NormalizeID(form.VoiceAgentProfileID)
	nextCfg.VoiceAgent.EnableSessionSummary = form.VoiceAgentSessionSummary
	nextCfg.VoiceAgent.CloseBehavior = config.NormalizeVoiceAgentCloseBehavior(
		form.VoiceAgentCloseBehavior,
		config.NormalizeVoiceAgentCloseBehavior(cfg.VoiceAgent.CloseBehavior, config.VoiceAgentCloseBehaviorContinue),
	)
	nextCfg.General.AutoStartOnLaunch = form.AutoStartOnLaunch
	nextCfg.VoiceAgent.AutoStartOnLaunch = form.AutoStartOnLaunch
	nextCfg.Audio.DeviceID = form.AudioDeviceID
	nextCfg.HuggingFace.Enabled = cfg.HuggingFace.Enabled && hfAvailableInBuild
	nextCfg.HuggingFace.Model = form.HFModel
	nextCfg.UI.OverlayEnabled = form.OverlayEnabled
	nextCfg.UI.OverlayPosition = form.OverlayPosition
	nextCfg.UI.OverlayMovable = form.OverlayMovable
	nextCfg.UI.OverlayFreeX = form.OverlayFreeX
	nextCfg.UI.OverlayFreeY = form.OverlayFreeY
	nextCfg.UI.OverlayMonitorPositions = cloneOverlayMonitorPositions(form.OverlayMonitorPositions)
	if !form.OverlayMovable {
		nextCfg.UI.OverlayFreeX = 0
		nextCfg.UI.OverlayFreeY = 0
		nextCfg.UI.OverlayMonitorPositions = map[string]config.OverlayFreePosition{}
	}
	nextCfg.UI.Visualizer = form.Visualizer
	nextCfg.UI.Design = form.Design
	nextCfg.UI.AssistOverlayMode = form.AssistOverlayMode
	nextCfg.UI.VoiceAgentOverlayMode = form.VoiceAgentOverlayMode
	nextCfg.Store.Backend = form.StoreBackend
	nextCfg.Store.SQLitePath = form.StoreSQLitePath
	nextCfg.Store.PostgresDSN = form.StorePostgresDSN
	nextCfg.Store.SaveAudio = form.StoreSaveAudio
	nextCfg.Store.AudioRetentionDays = form.StoreAudioRetention
	nextCfg.Store.MaxAudioStorageMB = form.StoreMaxAudioStorage
	nextCfg.Feedback.SaveAudio = form.StoreSaveAudio
	nextCfg.Feedback.AudioRetentionDays = form.StoreAudioRetention
	if form.StoreBackend == "sqlite" {
		nextCfg.Feedback.DBPath = form.StoreSQLitePath
	}
	nextCfg.General.ModelDownloadDir = form.ModelDownloadDir
	nextCfg.Vocabulary.Dictionary = form.VocabularyDictionary
	nextCfg.General.Language = form.Language

	// Google Cloud region (Gemini Live BYOK compliance control).
	if form.GoogleRegion != "" {
		nextCfg.Providers.Google.Region = form.GoogleRegion
	}

	// Wake-word: preserve catalog-driven defaults for anything the form
	// doesn't supply (e.g. ModelPath stays empty so the resolver looks
	// up the catalog entry on next runtime start).
	nextCfg.Wakeword.Enabled = form.WakewordEnabled
	nextCfg.Wakeword.PhraseID = form.WakewordPhraseID
	nextCfg.Wakeword.DefaultMode = config.NormalizeWakewordDefaultMode(form.WakewordDefaultMode)
	nextCfg.Wakeword.Threshold = form.WakewordThreshold
	if form.WakewordMinConsecutiveFrames > 0 {
		nextCfg.Wakeword.MinConsecutiveFrames = form.WakewordMinConsecutiveFrames
	}
	if form.WakewordCooldownMs > 0 {
		nextCfg.Wakeword.CooldownMs = form.WakewordCooldownMs
	}

	return nextCfg
}

func buildNextModeSelection(primaryProfileID, fallbackProfileID string, current config.ModeModelSelection) config.ModeModelSelection {
	next := profiles.NormalizeModeSelection(config.ModeModelSelection{
		PrimaryProfileID:  primaryProfileID,
		FallbackProfileID: fallbackProfileID,
		ModeSource:        current.ResolvedModeSource(),
	})
	return next
}
