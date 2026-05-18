package main

import (
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	desktopsettings "github.com/kombifyio/SpeechKit/internal/desktop/settings"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

// googleRegionAllowed is the set of valid [providers.google] region values
// offered in the settings UI.
var googleRegionAllowed = map[string]bool{
	"europe-west3":    true, // Frankfurt — default; DSGVO-friendly
	"europe-west4":    true, // Eemshaven — alternative EU
	"us-central1":     true, // Iowa — US customers
	"asia-southeast1": true, // Singapore — APAC customers
}

func normalizeGoogleRegion(region, fallback string) string {
	region = strings.TrimSpace(region)
	if googleRegionAllowed[region] {
		return region
	}
	return fallback
}

func parseVoiceAgentSettingsForm(req *http.Request, cfg *config.Config, f *settingsFormData) {
	f.VoiceAgentCloseBehavior = config.NormalizeVoiceAgentCloseBehavior(
		req.FormValue("voice_agent_close_behavior"),
		config.NormalizeVoiceAgentCloseBehavior(cfg.VoiceAgent.CloseBehavior, config.VoiceAgentCloseBehaviorContinue),
	)
	f.VoiceAgentProfileID = voiceagentprofile.NormalizeID(trimmedFormValue(req, "voice_agent_profile_id"))
	if !postFormIncludes(req, "voice_agent_profile_id") {
		f.VoiceAgentProfileID = voiceagentprofile.NormalizeID(cfg.VoiceAgent.AgentProfileID)
	}
	f.VoiceAgentRefinementPrompt = normalizeVoiceAgentPrompt(req.FormValue("voice_agent_refinement_prompt"))
	if !postFormIncludes(req, "voice_agent_refinement_prompt") {
		f.VoiceAgentRefinementPrompt = strings.TrimSpace(cfg.VoiceAgent.RefinementPrompt)
	}
	f.VoiceAgentSessionSummary = boolFormValue(req, "voice_agent_session_summary", cfg.VoiceAgent.EnableSessionSummary)
	f.AutoStartOnLaunch = boolFormValue(req, "auto_start_on_launch", cfg.General.AutoStartOnLaunch)
	if raw := trimmedFormValue(req, "voice_agent_auto_start"); trimmedFormValue(req, "auto_start_on_launch") == "" && raw != "" {
		f.AutoStartOnLaunch = raw == "1"
	}

	// Google Cloud region for Gemini Live (BYOK compliance control).
	// Defaults to the currently configured value; only updated when the form
	// field is present and contains a recognised region.
	currentRegion := cfg.Providers.Google.Region
	if currentRegion == "" {
		currentRegion = "europe-west3"
	}
	if postFormIncludes(req, "providers.google.region") {
		f.GoogleRegion = normalizeGoogleRegion(req.FormValue("providers.google.region"), currentRegion)
	} else {
		f.GoogleRegion = currentRegion
	}
}

func normalizeVoiceAgentPrompt(input string) string {
	return desktopsettings.NormalizeMultiline(input)
}
