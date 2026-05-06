package main

import (
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	desktopsettings "github.com/kombifyio/SpeechKit/internal/desktop/settings"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

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
}

func normalizeVoiceAgentPrompt(input string) string {
	return desktopsettings.NormalizeMultiline(input)
}
