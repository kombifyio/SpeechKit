package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/kombifyio/SpeechKit/internal/auditlog"
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
	// and is reacting to the same key press, producing duplicate text
	// injection into the focused application.
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
	// configureLoggingLimits runs after initAppLogging on purpose: logging
	// infrastructure must be up before loadDesktopStartupConfig (which can
	// emit log lines during config error reporting). The var defaults in
	// logging.go are aligned with the config defaults so the small window
	// before this call does not cause spurious rotation.
	configureLoggingLimits(cfg.Logging.MaxFileSizeMB, cfg.Logging.MaxFiles)
	configureUpdateChannel(cfg.Update.Enabled, cfg.Update.ManifestURL, cfg.Update.CheckIntervalHours, cfg.Update.SignaturePinThumbprint)
	if exePath, err := os.Executable(); err == nil {
		if err := auditlog.ConfigureFromOptions(auditlog.ConfigOptions{
			Enabled:         cfg.Audit.Enabled,
			Dir:             filepath.Join(filepath.Dir(exePath), "logs"),
			RetentionDays:   cfg.Audit.RetentionDays,
			EventLogEnabled: cfg.Audit.EventLogEnabled,
			OTLPEndpoint:    cfg.Audit.OTLPEndpoint,
			OTLPCertFile:    cfg.Audit.OTLPCertFile,
			OTLPKeyFile:     cfg.Audit.OTLPKeyFile,
			OTLPCAFile:      cfg.Audit.OTLPCAFile,
		}); err != nil {
			slog.Warn("auditlog: configure failed", "err", err)
		}
	} else {
		slog.Warn("auditlog: could not resolve executable path", "err", err)
	}
	// Emit a policy.applied audit event so compliance auditors can verify
	// which registry source contributed to the effective config and how many
	// values were locked. We emit even when KeysFound == 0 so the audit trail
	// is consistent across deploys (auditors can distinguish "no registry
	// policy present" from "policy data missing from audit log").
	if policy := config.LastPolicy(); policy.Origin != "none" || policy.KeysFound > 0 {
		if err := auditlog.AppendEvent(context.Background(), auditlog.Record{
			Event: auditlog.EventPolicyApplied,
			Resource: map[string]any{
				"source":            policy.Origin,
				"keys_locked_count": policy.KeysFound,
			},
		}); err != nil {
			slog.Warn("auditlog: failed to write policy.applied event", "err", err)
		}
	}
	if *noTelemetry {
		// Mutate cfg to reflect the runtime state — not functionally required
		// (configureUpdateChannel mutated package vars already), but keeps cfg
		// self-consistent for diagnostic dumps and future readers.
		cfg.Telemetry.UpdateCheck = false
		cfg.Update.Enabled = false
		disableTelemetry()
		slog.Info("telemetry disabled by --no-telemetry CLI flag")
	}
	tracker.stage("config_loaded", "path", cfgPath, "install_mode", string(installState.Mode))

	state := newInitialAppState(cfg)
	// First-run UX: hide the overlay until the user finishes the
	// onboarding wizard. Without this, the bubble pops up over the
	// setup window which is confusing. The setup-completion handler
	// (/app/complete-setup) re-applies cfg.UI.OverlayEnabled. Already-
	// configured users keep their current overlay preference because
	// SetupDone is true on launch for them.
	if installState != nil && !installState.SetupDone {
		state.setOverlayEnabled(false)
	}
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

	// Wake-word listener (opt-in via cfg.Wakeword.Enabled). Never fatal —
	// missing models or audio errors degrade gracefully to "no wake-word
	// today" with a warning in the log and the in-app status feed.
	startDesktopWakeword(ctx, cfg, state, hkManager, &cleanup)
	tracker.stage("wakeword_evaluated")

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
	showSettingsWindowWithFocus(window, true)
}

func showSettingsWindowWithFocus(window settingsWindow, focus bool) {
	defer recoverHook("settings_window_show")

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
	if focus {
		window.Focus()
	}
	slog.Info("desktop.window.shown", "window", "settings", "was_visible", wasVisible, "focus", focus)
}

// isAppStarted reports whether the Wails application event loop has entered
// its main thread. Until this returns true, any code path that calls
// application.InvokeSync (overlay windows, dashboard show, screen-aware
// positioning) will dereference a nil internal pointer and panic. Callers
// that may run before app.Run() — most notably the desktop input
// controller goroutine — should gate UI-touching work on this.
//
// When no Wails app is wired (test harnesses, ad-hoc tools that never call
// application.New), there is no main-thread dispatcher to wait for —
// InvokeSync would never be dialled in the first place — so we treat the
// app as "started" and let callers proceed. The flag is only meaningful
// when a Wails app exists and we are racing against its
// OnApplicationStarted callback.
func (s *appState) isAppStarted() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wailsApp == nil {
		return true
	}
	return s.appStarted
}

func (s *appState) markAppStarted() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.appStarted = true
	s.mu.Unlock()
}

func (s *appState) dashboardReadyForAutoOpen() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dashboard != nil && s.appStarted
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
		showSettingsWindowWithFocus(window, source != "first-run-setup")
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
// window and application-started event before asking Wails to show it.
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
			if state.dashboardReadyForAutoOpen() {
				slog.Info("first-run onboarding: opening dashboard")
				showDashboard("first-run-setup")
				return
			}
		}
		slog.Warn("first-run onboarding: dashboard window never became ready within timeout — user must open it manually from tray")
	}()
}
