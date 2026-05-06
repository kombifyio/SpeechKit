package main

import (
	"net/http"

	"github.com/kombifyio/SpeechKit/internal/config"
)

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
