package main

import (
	"context"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/store"
)

func startDesktopInputRuntime(
	ctx context.Context,
	cfg *config.Config,
	state *appState,
	r *router.Router,
	feedbackStore store.Store,
	audioRuntime desktopAudioRuntime,
	services desktopRuntimeServices,
	hkManager *modeHotkeyManager,
	installState *config.InstallState,
	showDashboard func(string),
	cleanup *desktopCleanupStack,
) (*desktopInputController, error) {
	return startDesktopModeRuntime(desktopModeRuntimeOptions{
		Ctx:             ctx,
		Config:          cfg,
		State:           state,
		STTRouter:       r,
		FeedbackStore:   feedbackStore,
		Capturer:        audioRuntime.Capturer,
		DictationVAD:    audioRuntime.DictationVAD,
		Clipboard:       services.Clipboard,
		QuickActions:    services.QuickActions,
		HotkeyEvents:    hkManager.Events(),
		SilenceAutoStop: audioRuntime.SilenceAutoStop,
		InstallState:    installState,
		ShowDashboard:   showDashboard,
		Cleanup:         cleanup,
	})
}
