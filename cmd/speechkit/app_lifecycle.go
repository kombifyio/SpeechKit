package main

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/downloads"
)

func runDesktopApp(closeLogFile func()) {
	var cleanup desktopCleanupStack
	defer cleanup.Close()

	tracker := newStartupTracker()
	tracker.stage("entered", "pid", os.Getpid())

	// Belt-and-suspenders for Wails v3 alpha SingleInstance: claim a
	// session-scoped mutex before any global resource (Windows hotkeys,
	// WASAPI capture) is acquired. Wails' own SingleInstance check fires
	// inside app.Run() which is the LAST stage of bootstrap — by then a
	// racing second instance has already registered the global hotkey
	// and is reacting to the same key press, producing the duplicate
	// text injection symptom in SK-004.5.10.
	release, isFirst, mutexErr := tryAcquireSingleton()
	if mutexErr != nil {
		slog.Warn("desktop.singleton.acquire_error",
			"err", mutexErr,
			"action", "continuing_startup",
			"note", "Wails SingleInstanceOptions remains as fallback")
	}
	if !isFirst {
		slog.Info("desktop.singleton.duplicate_instance",
			"pid", os.Getpid(),
			"action", "exiting")
		closeLogFile()
		os.Exit(0) //nolint:gocritic // closeLogFile() called explicitly above before exit
	}
	if release != nil {
		cleanup.Add(release)
	}
	tracker.stage("singleton_acquired")

	cfgPath, cfg, installState, err := loadDesktopStartupConfig()
	if err != nil {
		slog.Error("config load failed", "err", err)
		closeLogFile()
		os.Exit(1) //nolint:gocritic // exitAfterDefer: closeLogFile() called explicitly above before exit
	}
	tracker.stage("config_loaded", "path", cfgPath, "install_mode", string(installState.Mode))

	state := newInitialAppState(cfg)
	tracker.stage("state_init")

	r := initDesktopRouterRuntime(cfg, state)
	tracker.stage("router_runtime")

	audioRuntime, err := initDesktopAudioRuntime(cfg, state, &cleanup)
	if err != nil {
		slog.Error("audio init failed", "err", err)
		os.Exit(1)
	}
	tracker.stage("audio_runtime")

	ctx, cancel := initDesktopAppContext(state, &cleanup)
	services := initDesktopRuntimeServices(ctx, cfg, state, &cleanup)
	tracker.stage("runtime_services")
	prepareDesktopVoiceAgentSession(cfg, state)
	hkManager := newDesktopModeHotkeyManager(cfg, state)
	feedbackStore := openDesktopFeedbackStore(cfg, state, &cleanup)
	showDashboard := newDesktopDashboardPresenter(state)
	var inputController *desktopInputController

	app := newDesktopWailsApp(desktopWailsAppOptions{
		ConfigPath:    cfgPath,
		Config:        cfg,
		State:         state,
		STTRouter:     r,
		FeedbackStore: feedbackStore,
		InstallState:  installState,
		Capturer:      audioRuntime.Capturer,
		Cancel:        cancel,
		ShowDashboard: showDashboard,
	})
	tracker.stage("wails_app_created")

	configureDesktopWindowsAndTray(desktopWindowRuntimeOptions{
		App:        app,
		Ctx:        ctx,
		ConfigPath: cfgPath,
		Config:     cfg,
		State:      state,
		ShowDashboard: func(source string) {
			showDashboard(source)
		},
		InputController: func() *desktopInputController {
			return inputController
		},
	})
	tracker.stage("windows_configured")

	startDesktopProviderReadiness(ctx, state, r)
	if err := startDesktopHotkeys(ctx, hkManager, &cleanup); err != nil {
		slog.Error("hotkey start failed", "err", err)
		os.Exit(1)
	}
	tracker.stage("hotkeys_started")

	logDesktopStartupReady(cfg, state, r)
	startDesktopOverlaySyncLoop(ctx, state)
	inputController, err = startDesktopInputRuntime(ctx, cfg, state, r, feedbackStore, audioRuntime, services, hkManager, installState, showDashboard, &cleanup)
	if err != nil {
		slog.Error("desktop mode runtime init failed", "err", err)
		os.Exit(1)
	}
	tracker.stage("input_runtime_ready")

	scheduleFirstRunOnboarding(state, installState, showDashboard)
	tracker.stage("app_run_begin")

	if err := app.Run(); err != nil {
		slog.Error("app run failed", "err", err)
		os.Exit(1)
	}
	tracker.stage("app_run_returned")
	cancel()
}

func newInitialAppState(cfg *config.Config) *appState {
	return &appState{
		controlPlaneToken:        newControlPlaneToken(),
		hotkey:                   cfg.General.DictateHotkey,
		dictateHotkey:            cfg.General.DictateHotkey,
		assistHotkey:             cfg.General.AssistHotkey,
		voiceAgentHotkey:         cfg.General.VoiceAgentHotkey,
		dictateHotkeyBehavior:    cfg.General.DictateHotkeyBehavior,
		assistHotkeyBehavior:     cfg.General.AssistHotkeyBehavior,
		voiceAgentHotkeyBehavior: cfg.General.VoiceAgentHotkeyBehavior,
		dictateEnabled:           cfg.General.DictateEnabled,
		assistEnabled:            cfg.General.AssistEnabled,
		voiceAgentEnabled:        cfg.General.VoiceAgentEnabled,
		agentHotkey:              cfg.General.AgentHotkey,
		activeMode:               cfg.General.ActiveMode,
		prompterMode:             modeAssist,
		audioDeviceID:            cfg.Audio.DeviceID,
		audioOutputDeviceID:      cfg.Audio.OutputDeviceID,
		activeProfiles:           activeProfilesFromConfig(cfg, filteredModelCatalog()),
		providers:                []string{},
		overlayEnabled:           cfg.UI.OverlayEnabled,
		overlayPosition:          cfg.UI.OverlayPosition,
		overlayMovable:           cfg.UI.OverlayMovable,
		overlayFreeX:             cfg.UI.OverlayFreeX,
		overlayFreeY:             cfg.UI.OverlayFreeY,
		overlayMonitorCenters:    cloneOverlayMonitorPositions(cfg.UI.OverlayMonitorPositions),
		overlayVisualizer:        cfg.UI.Visualizer,
		overlayDesign:            cfg.UI.Design,
		assistOverlayMode:        config.NormalizeOverlayFeedbackMode(cfg.UI.AssistOverlayMode, config.OverlayFeedbackModeSmallFeedback),
		voiceAgentOverlayMode:    config.NormalizeOverlayFeedbackMode(cfg.UI.VoiceAgentOverlayMode, config.OverlayFeedbackModeSmallFeedback),
		vocabularyDictionary:     cfg.Vocabulary.Dictionary,
		screenLocator:            newActiveWindowScreenLocator(),
		downloads:                downloads.NewManager(),
		appUpdates:               newAppUpdateManager(),
	}
}

func showSettingsWindow(window settingsWindow) {
	if window == nil {
		slog.Warn("desktop.window.show_skipped", "window", "settings", "reason", "nil")
		return
	}

	wasVisible := window.IsVisible()
	window.Restore()
	window.UnMinimise()
	if !wasVisible {
		window.Show()
	}
	window.Focus()
	slog.Info("desktop.window.shown", "window", "settings", "was_visible", wasVisible)
}

func dashboardRefreshScript(source string) string {
	return fmt.Sprintf(
		`window.dispatchEvent(new CustomEvent("speechkit:dashboard-show",{detail:{source:%s}}));`,
		strconv.Quote(source),
	)
}

func (s *appState) showDashboardWindow(source string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	window := s.dashboard
	app := s.wailsApp
	s.mu.Unlock()

	show := func() {
		if window == nil {
			return
		}
		showSettingsWindow(window)
		window.ExecJS(dashboardRefreshScript(source))
	}

	if app != nil {
		application.InvokeSync(show)
		return
	}
	show()
}

func (s *appState) beginShutdown() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.shuttingDown = true
	s.mu.Unlock()
}

func (s *appState) isShuttingDown() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shuttingDown
}

func (s *appState) shouldHideWindowOnClose() bool {
	return !s.isShuttingDown()
}

// scheduleFirstRunOnboarding pops the dashboard window automatically
// when the user is on a fresh local install that has not yet completed
// the onboarding wizard. Without this, the desktop client launches
// silently into the tray and the user can trigger global hotkeys
// without knowing SpeechKit is listening.
//
// The presenter is dispatched in a goroutine because the dashboard
// window is created during app.Run()'s event loop, which has not yet
// started when we are called. We poll briefly (under 4 s) for the
// window to be wired up and then ask Wails to show it.
func scheduleFirstRunOnboarding(state *appState, installState *config.InstallState, showDashboard func(string)) {
	if installState == nil || installState.Mode != config.InstallModeLocal || installState.SetupDone {
		return
	}
	if state == nil || showDashboard == nil {
		return
	}
	go func() {
		const attempts = 20
		const interval = 200 * time.Millisecond
		for i := 0; i < attempts; i++ {
			time.Sleep(interval)
			state.mu.Lock()
			ready := state.dashboard != nil
			state.mu.Unlock()
			if ready {
				slog.Info("first-run onboarding: opening dashboard")
				showDashboard("first-run-setup")
				return
			}
		}
		slog.Warn("first-run onboarding: dashboard window never became ready within timeout — user must open it manually from tray")
	}()
}
