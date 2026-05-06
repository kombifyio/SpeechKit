package main

import (
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
)

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
