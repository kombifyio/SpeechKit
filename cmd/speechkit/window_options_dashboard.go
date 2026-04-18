package main

import (
	"context"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func newDashboardWindowOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:            "SpeechKit Dashboard",
		Width:            1240,
		Height:           860,
		MinWidth:         900,
		MinHeight:        620,
		Frameless:        true,
		Hidden:           true,
		BackgroundType:   application.BackgroundTypeTranslucent,
		BackgroundColour: application.NewRGBA(16, 20, 28, 240),
		URL:              "/dashboard.html",
		Windows: application.WindowsWindow{
			Theme:        application.Dark,
			BackdropType: application.Mica,
			DisableIcon:  true,
		},
	}
}

func maybeAutoStartVoiceAgentOnLaunch(ctx context.Context, cfg *config.Config, controller *desktopInputController) {
	if cfg == nil || controller == nil {
		return
	}
	if !cfg.General.VoiceAgentEnabled || strings.TrimSpace(cfg.General.VoiceAgentHotkey) == "" {
		return
	}
	if !cfg.VoiceAgent.Enabled || !cfg.General.AutoStartOnLaunch {
		return
	}
	controller.activateVoiceAgent(ctx)
}
