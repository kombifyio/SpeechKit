package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
)

// settingsFormData holds parsed and validated form values from the settings page.
type settingsFormData struct {
	DictateEnabled             bool
	AssistEnabled              bool
	VoiceAgentEnabled          bool
	DictateHotkey              string
	AssistHotkey               string
	VoiceAgentHotkey           string
	DictateHotkeyBehavior      string
	AssistHotkeyBehavior       string
	VoiceAgentHotkeyBehavior   string
	VoiceAgentCloseBehavior    string
	VoiceAgentRefinementPrompt string
	VoiceAgentSessionSummary   bool
	AutoStartOnLaunch          bool
	AgentHotkey                string
	AgentMode                  string
	ActiveMode                 string
	AudioDeviceID              string
	HFModel                    string
	OverlayEnabled             bool
	Visualizer                 string
	Design                     string
	AssistOverlayMode          string
	VoiceAgentOverlayMode      string
	OverlayPosition            string
	OverlayMovable             bool
	OverlayFreeX               int
	OverlayFreeY               int
	OverlayMonitorPositions    map[string]config.OverlayFreePosition
	StoreBackend               string
	StoreSQLitePath            string
	StorePostgresDSN           string
	StoreSaveAudio             bool
	StoreAudioRetention        int
	StoreMaxAudioStorage       int
	ModelDownloadDir           string
	VocabularyDictionary       string
	Language                   string
	DictatePrimaryProfileID    string
	DictateFallbackProfileID   string
	AssistPrimaryProfileID     string
	AssistFallbackProfileID    string
	VoicePrimaryProfileID      string
	VoiceFallbackProfileID     string
}

// parseSettingsForm extracts and validates all settings form values.
// Returns the parsed data and an empty string on success, or an error message.
func parseSettingsForm(req *http.Request, cfg *config.Config) (settingsFormData, string) {
	var f settingsFormData

	parseModeSettingsForm(req, cfg, &f)
	parseVoiceAgentSettingsForm(req, cfg, &f)
	if errMsg := validateModeSettingsForm(req, cfg, &f); errMsg != "" {
		return f, errMsg
	}
	if errMsg := parseAudioAndProviderSettingsForm(req, cfg, &f); errMsg != "" {
		return f, errMsg
	}
	if errMsg := parseOverlaySettingsForm(req, cfg, &f); errMsg != "" {
		return f, errMsg
	}
	if errMsg := parseStoreSettingsForm(req, cfg, &f); errMsg != "" {
		return f, errMsg
	}
	parseContentSettingsForm(req, cfg, &f)
	parseModelSelectionSettingsForm(req, cfg, &f)
	if errMsg := validateModelSelectionSettingsForm(cfg, &f); errMsg != "" {
		return f, errMsg
	}

	return f, ""
}

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
		config.NormalizeHotkeyBehavior(cfg.General.DictateHotkeyBehavior, config.HotkeyBehaviorPushToTalk),
	)
	f.AssistHotkeyBehavior = config.NormalizeHotkeyBehavior(
		req.FormValue("assist_hotkey_behavior"),
		config.NormalizeHotkeyBehavior(cfg.General.AssistHotkeyBehavior, config.HotkeyBehaviorPushToTalk),
	)
	f.VoiceAgentHotkeyBehavior = config.NormalizeHotkeyBehavior(
		req.FormValue("voice_agent_hotkey_behavior"),
		config.NormalizeHotkeyBehavior(cfg.General.VoiceAgentHotkeyBehavior, config.HotkeyBehaviorPushToTalk),
	)
}

func parseVoiceAgentSettingsForm(req *http.Request, cfg *config.Config, f *settingsFormData) {
	f.VoiceAgentCloseBehavior = config.NormalizeVoiceAgentCloseBehavior(
		req.FormValue("voice_agent_close_behavior"),
		config.NormalizeVoiceAgentCloseBehavior(cfg.VoiceAgent.CloseBehavior, config.VoiceAgentCloseBehaviorContinue),
	)
	f.VoiceAgentRefinementPrompt = normalizeVoiceAgentPrompt(req.FormValue("voice_agent_refinement_prompt"))
	if !postFormIncludes(req, "voice_agent_refinement_prompt") {
		f.VoiceAgentRefinementPrompt = strings.TrimSpace(cfg.VoiceAgent.RefinementPrompt)
	}
	f.VoiceAgentSessionSummary = boolFormValue(req, "voice_agent_session_summary", cfg.VoiceAgent.EnableSessionSummary)
	f.AutoStartOnLaunch = boolFormValue(req, "auto_start_on_launch", cfg.General.AutoStartOnLaunch)
	if raw := trimmedFormValue(req, "voice_agent_auto_start"); trimmedFormValue(req, "auto_start_on_launch") == "" && raw != "" {
		f.AutoStartOnLaunch = raw == "1"
	}
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

func parseAudioAndProviderSettingsForm(req *http.Request, cfg *config.Config, f *settingsFormData) string {
	f.AudioDeviceID = firstNonEmpty(
		trimmedFormValue(req, "audio_device_id"),
		trimmedFormValue(req, "selected_audio_device_id"),
		cfg.Audio.DeviceID,
	)
	f.HFModel = valueOrDefault(trimmedFormValue(req, "hf_model"), cfg.HuggingFace.Model)
	if config.ManagedHuggingFaceAvailableInBuild() && !isSupportedHFModel(f.HFModel) {
		return msgUnsupportedModel
	}
	return ""
}

func parseOverlaySettingsForm(req *http.Request, cfg *config.Config, f *settingsFormData) string {
	f.OverlayEnabled = req.FormValue("overlay_enabled") == "1"
	f.Visualizer = valueOrDefault(trimmedFormValue(req, "overlay_visualizer"), cfg.UI.Visualizer)
	if !isSupportedOverlayVisualizer(f.Visualizer) {
		return msgUnsupportedVis
	}
	f.Design = valueOrDefault(trimmedFormValue(req, "overlay_design"), cfg.UI.Design)
	if !isSupportedOverlayDesign(f.Design) {
		return msgUnsupportedDesign
	}
	f.AssistOverlayMode = config.NormalizeOverlayFeedbackMode(
		trimmedFormValue(req, "assist_overlay_mode"),
		config.NormalizeOverlayFeedbackMode(cfg.UI.AssistOverlayMode, config.OverlayFeedbackModeSmallFeedback),
	)
	f.VoiceAgentOverlayMode = config.NormalizeOverlayFeedbackMode(
		trimmedFormValue(req, "voice_agent_overlay_mode"),
		config.NormalizeOverlayFeedbackMode(cfg.UI.VoiceAgentOverlayMode, config.OverlayFeedbackModeSmallFeedback),
	)
	f.OverlayPosition = valueOrDefault(trimmedFormValue(req, "overlay_position"), cfg.UI.OverlayPosition)
	if !isSupportedOverlayPosition(f.OverlayPosition) {
		return msgUnsupportedPos
	}
	f.OverlayMovable = boolFormValue(req, "overlay_movable", cfg.UI.OverlayMovable)
	f.OverlayFreeX = intFormValue(req, "overlay_free_x", cfg.UI.OverlayFreeX)
	f.OverlayFreeY = intFormValue(req, "overlay_free_y", cfg.UI.OverlayFreeY)
	f.OverlayMonitorPositions = cloneOverlayMonitorPositions(cfg.UI.OverlayMonitorPositions)
	return ""
}

func parseStoreSettingsForm(req *http.Request, cfg *config.Config, f *settingsFormData) string {
	f.StoreSaveAudio = boolFormValue(req, "store_save_audio", cfg.Store.SaveAudio)
	f.StoreBackend = firstNonEmpty(trimmedFormValue(req, "store_backend"), cfg.Store.Backend, "sqlite")
	switch f.StoreBackend {
	case "sqlite", "postgres":
	default:
		return msgUnsupportedStore
	}
	f.StoreSQLitePath = valueOrDefault(trimmedFormValue(req, "store_sqlite_path"), cfg.Store.SQLitePath)
	f.StorePostgresDSN = valueOrDefault(trimmedFormValue(req, "store_postgres_dsn"), cfg.Store.PostgresDSN)
	if f.StoreBackend == "postgres" && f.StorePostgresDSN == "" {
		return msgPostgresDSNReq
	}
	f.StoreAudioRetention = nonNegativeIntFormValue(req, "store_audio_retention_days", cfg.Store.AudioRetentionDays)
	f.StoreMaxAudioStorage = nonNegativeIntFormValue(req, "store_max_audio_storage_mb", cfg.Store.MaxAudioStorageMB)
	return ""
}

func parseContentSettingsForm(req *http.Request, cfg *config.Config, f *settingsFormData) {
	f.ModelDownloadDir = normalizeModelDownloadDir(req.FormValue("model_download_dir"))
	if !postFormIncludes(req, "model_download_dir") {
		f.ModelDownloadDir = strings.TrimSpace(cfg.General.ModelDownloadDir)
	}
	f.VocabularyDictionary = normalizeVocabularyDictionary(req.FormValue("vocabulary_dictionary"))
	if !postFormIncludes(req, "vocabulary_dictionary") {
		f.VocabularyDictionary = cfg.Vocabulary.Dictionary
	}
	f.Language = firstNonEmpty(trimmedFormValue(req, "language"), cfg.General.Language, "de")
}

func parseModelSelectionSettingsForm(req *http.Request, cfg *config.Config, f *settingsFormData) {
	f.DictatePrimaryProfileID = valueOrDefault(
		trimmedFormValue(req, "dictate_primary_profile_id"),
		strings.TrimSpace(cfg.ModelSelection.Dictate.PrimaryProfileID),
	)
	f.DictateFallbackProfileID = valueOrDefault(
		trimmedFormValue(req, "dictate_fallback_profile_id"),
		strings.TrimSpace(cfg.ModelSelection.Dictate.FallbackProfileID),
	)
	f.AssistPrimaryProfileID = valueOrDefault(
		trimmedFormValue(req, "assist_primary_profile_id"),
		strings.TrimSpace(cfg.ModelSelection.Assist.PrimaryProfileID),
	)
	f.AssistFallbackProfileID = valueOrDefault(
		trimmedFormValue(req, "assist_fallback_profile_id"),
		strings.TrimSpace(cfg.ModelSelection.Assist.FallbackProfileID),
	)
	f.VoicePrimaryProfileID = valueOrDefault(
		trimmedFormValue(req, "voice_primary_profile_id"),
		strings.TrimSpace(cfg.ModelSelection.VoiceAgent.PrimaryProfileID),
	)
	f.VoiceFallbackProfileID = valueOrDefault(
		trimmedFormValue(req, "voice_fallback_profile_id"),
		strings.TrimSpace(cfg.ModelSelection.VoiceAgent.FallbackProfileID),
	)
}

func validateModelSelectionSettingsForm(cfg *config.Config, f *settingsFormData) string {
	catalog := filteredModelCatalog()
	selections := []struct {
		mode      string
		selection config.ModeModelSelection
	}{
		{mode: modeDictate, selection: config.ModeModelSelection{PrimaryProfileID: f.DictatePrimaryProfileID, FallbackProfileID: f.DictateFallbackProfileID}},
		{mode: modeAssist, selection: config.ModeModelSelection{PrimaryProfileID: f.AssistPrimaryProfileID, FallbackProfileID: f.AssistFallbackProfileID}},
		{mode: modeVoiceAgent, selection: config.ModeModelSelection{PrimaryProfileID: f.VoicePrimaryProfileID, FallbackProfileID: f.VoiceFallbackProfileID}},
	}
	for _, candidate := range selections {
		if err := validateModeSelection(cfg, catalog, candidate.mode, candidate.selection); err != nil {
			return err.Error()
		}
	}
	return ""
}

// buildNextConfig creates a new Config from the parsed form values.
func buildNextConfig(form settingsFormData, cfg *config.Config) config.Config {
	hfAvailableInBuild := config.ManagedHuggingFaceAvailableInBuild()
	nextCfg := *cfg
	nextCfg.General.Hotkey = form.DictateHotkey // keep legacy field in sync
	nextCfg.General.DictateHotkey = form.DictateHotkey
	nextCfg.General.AssistHotkey = form.AssistHotkey
	nextCfg.General.VoiceAgentHotkey = form.VoiceAgentHotkey
	nextCfg.General.DictateHotkeyBehavior = config.NormalizeHotkeyBehavior(form.DictateHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	nextCfg.General.AssistHotkeyBehavior = config.NormalizeHotkeyBehavior(form.AssistHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	nextCfg.General.VoiceAgentHotkeyBehavior = config.NormalizeHotkeyBehavior(form.VoiceAgentHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
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
	nextCfg.ModelSelection.Dictate = normalizeModeSelection(config.ModeModelSelection{
		PrimaryProfileID:  form.DictatePrimaryProfileID,
		FallbackProfileID: form.DictateFallbackProfileID,
	})
	nextCfg.ModelSelection.Assist = normalizeModeSelection(config.ModeModelSelection{
		PrimaryProfileID:  form.AssistPrimaryProfileID,
		FallbackProfileID: form.AssistFallbackProfileID,
	})
	nextCfg.ModelSelection.VoiceAgent = normalizeModeSelection(config.ModeModelSelection{
		PrimaryProfileID:  form.VoicePrimaryProfileID,
		FallbackProfileID: form.VoiceFallbackProfileID,
	})
	nextCfg.VoiceAgent.RefinementPrompt = form.VoiceAgentRefinementPrompt
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
	return nextCfg
}

func boolFormValue(req *http.Request, key string, fallback bool) bool {
	raw := trimmedFormValue(req, key)
	if raw == "" {
		return fallback
	}
	return raw == "1"
}

func intFormValue(req *http.Request, key string, fallback int) int {
	raw := trimmedFormValue(req, key)
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func nonNegativeIntFormValue(req *http.Request, key string, fallback int) int {
	parsed := intFormValue(req, key, fallback)
	if parsed < 0 {
		return fallback
	}
	return parsed
}

func trimmedFormValue(req *http.Request, key string) string {
	return strings.TrimSpace(req.FormValue(key))
}

func postFormIncludes(req *http.Request, key string) bool {
	if req == nil || req.PostForm == nil {
		return false
	}
	_, ok := req.PostForm[key]
	return ok
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func valueOrDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func normalizeVocabularyDictionary(input string) string {
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.TrimSpace(normalized)
}

func normalizeModelDownloadDir(input string) string {
	return strings.TrimSpace(input)
}

func normalizeVoiceAgentPrompt(input string) string {
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.TrimSpace(normalized)
}
