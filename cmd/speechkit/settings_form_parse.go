package main

import (
	"net/http"

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
	VoiceAgentProfileID        string
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

	// Wake-word controls. WakewordEnabled is the master toggle; the rest
	// mirror WakewordConfig fields and apply only when Enabled.
	WakewordEnabled              bool
	WakewordBackend              string
	WakewordPhraseID             string
	WakewordDefaultMode          string
	WakewordThreshold            float64
	WakewordMinConsecutiveFrames int
	WakewordCooldownMs           int
	WakewordDebugMode            bool

	// GoogleRegion is the [providers.google] region setting.
	// Surfaces in the Voice Agent settings form as a compliance control.
	GoogleRegion string
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
	parseWakewordSettingsForm(req, cfg, &f)

	return f, ""
}
