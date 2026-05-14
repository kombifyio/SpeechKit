package main

import (
	"context"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/tray"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// windowCloseReason classifies why a WindowClosing hook fired so the
// log line tells us whether the close was a user-initiated app quit
// (shouldHideWindowOnClose returns false) versus the normal "X" button
// case where we hide-instead-of-close to keep the tray app running.
func windowCloseReason(state *appState) string {
	if state == nil {
		return "no_state"
	}
	if state.isShuttingDown() {
		return "shutdown"
	}
	return "hide"
}

type desktopWindowRuntimeOptions struct {
	App             *application.App
	Ctx             context.Context
	ConfigPath      string
	Config          *config.Config
	State           *appState
	ShowDashboard   func(string)
	InputController func() *desktopInputController
}

func configureDesktopWindowsAndTray(opts desktopWindowRuntimeOptions) {
	opts.State.wailsApp = opts.App

	pillAnchorWindow := opts.App.Window.NewWithOptions(newPillAnchorWindowOptions())
	pillPanelWindow := opts.App.Window.NewWithOptions(newPillPanelWindowOptions())
	dotAnchorWindow := opts.App.Window.NewWithOptions(newDotAnchorWindowOptions())
	radialMenuWindow := opts.App.Window.NewWithOptions(newRadialMenuWindowOptions())
	assistBubbleWindow := opts.App.Window.NewWithOptions(newAssistBubbleWindowOptions())

	opts.State.pillAnchor = pillAnchorWindow
	opts.State.pillPanel = pillPanelWindow
	opts.State.dotAnchor = dotAnchorWindow
	opts.State.radialMenu = radialMenuWindow
	opts.State.assistBubble = assistBubbleWindow

	prompterWin := opts.App.Window.NewWithOptions(newPrompterWindowOptions())
	prompterWin.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		defer recoverHook("prompter_window_closing")
		reason := windowCloseReason(opts.State)
		slog.Info("desktop.window.closing", "window", "prompter", "reason", reason)
		if !opts.State.shouldHideWindowOnClose() {
			return
		}
		event.Cancel()
		if inputController := opts.InputController(); inputController != nil {
			inputController.closeVoiceAgentPrompter(opts.Ctx)
			return
		}
		prompterWin.Hide()
	})
	opts.State.prompterWindow = prompterWin

	registerOverlayMovePersistence(opts.ConfigPath, opts.Config, opts.State, pillPanelWindow)

	dashboardWin := createDashboardWindow(opts.App, opts.State)
	opts.State.dashboard = dashboardWin
	opts.State.settings = dashboardWin

	appTray := tray.New(opts.App, func() {
		slog.Info("desktop.tray.quit_requested")
		opts.State.addLog("Quit requested from tray", "info")
		opts.State.beginShutdown()
		opts.App.Quit()
	}, func() {
		slog.Info("desktop.tray.dashboard_requested")
		opts.State.addLog("Dashboard requested from tray", "info")
		if opts.State.engine != nil {
			_ = opts.State.engine.Commands().Dispatch(context.Background(), speechkit.Command{
				Type: speechkit.CommandShowDashboard,
				Metadata: map[string]string{
					"source": "tray",
				},
			})
			return
		}
		opts.ShowDashboard("tray")
	})
	appTray.OnFeedback = func() {
		_ = exec.Command("explorer", "https://github.com/kombifyio/SpeechKit/issues").Start() //nolint:noctx // fire-and-forget browser open; no caller context available
	}
	opts.State.appTray = appTray

	opts.App.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(event *application.ApplicationEvent) {
		defer recoverHook("application_started")
		opts.State.markAppStarted()
		opts.State.positionOverlay()
		opts.State.setState("idle", "")
		maybeAutoStartVoiceAgentOnLaunch(opts.Ctx, opts.Config, opts.InputController())
	})

	opts.App.Event.On("voiceagent:start", func(_ *application.CustomEvent) {
		defer recoverHook("voiceagent_start")
		if inputController := opts.InputController(); inputController != nil {
			inputController.activateVoiceAgent(opts.Ctx)
		}
	})
	opts.App.Event.On("voiceagent:stop", func(_ *application.CustomEvent) {
		defer recoverHook("voiceagent_stop")
		if inputController := opts.InputController(); inputController != nil {
			inputController.deactivateVoiceAgentWithReason(opts.Ctx, true, "stop control")
		}
	})
	opts.App.Event.On("voiceagent:close", func(_ *application.CustomEvent) {
		defer recoverHook("voiceagent_close")
		if inputController := opts.InputController(); inputController != nil {
			inputController.closeVoiceAgentPrompter(opts.Ctx)
		}
	})
}

func createDashboardWindow(wailsApp *application.App, state *appState) *application.WebviewWindow {
	win := wailsApp.Window.NewWithOptions(newDashboardWindowOptions())
	win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		defer recoverHook("dashboard_window_closing")
		reason := windowCloseReason(state)
		slog.Info("desktop.window.closing", "window", "dashboard", "reason", reason)
		if !state.shouldHideWindowOnClose() {
			return
		}
		event.Cancel()
		win.Hide()
	})
	return win
}

func registerOverlayMovePersistence(cfgPath string, cfg *config.Config, state *appState, pillPanelWindow *application.WebviewWindow) {
	var overlayMoveSaveMu sync.Mutex
	var overlayMoveSaveTimer *time.Timer
	scheduleOverlayMoveSave := func() {
		overlayMoveSaveMu.Lock()
		defer overlayMoveSaveMu.Unlock()

		if overlayMoveSaveTimer != nil {
			overlayMoveSaveTimer.Stop()
		}

		overlayMoveSaveTimer = time.AfterFunc(250*time.Millisecond, func() {
			centerX, centerY, monitorPositions := state.overlayFreeCenterState()

			overlayMoveSaveMu.Lock()
			defer overlayMoveSaveMu.Unlock()

			cfg.UI.OverlayFreeX = centerX
			cfg.UI.OverlayFreeY = centerY
			cfg.UI.OverlayMonitorPositions = monitorPositions
			if !cfg.UI.OverlayMovable {
				return
			}
			if err := config.Save(cfgPath, cfg); err != nil {
				slog.Warn("save free overlay position", "err", err)
			}
		})
	}

	pillPanelWindow.OnWindowEvent(events.Common.WindowDidMove, func(_ *application.WindowEvent) {
		defer recoverHook("pill_panel_window_did_move")
		if !pillPanelWindow.IsVisible() {
			return
		}
		x, y := pillPanelWindow.Position()
		if !state.updateOverlayFreeCenterFromPanel(x, y) {
			return
		}
		scheduleOverlayMoveSave()
	})
}
