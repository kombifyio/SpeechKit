package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/firebase/genkit/go/core"

	appassets "github.com/kombifyio/SpeechKit/assets"
	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/downloads"
	_ "github.com/kombifyio/SpeechKit/internal/kombify"
	"github.com/kombifyio/SpeechKit/internal/output"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/secrets"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/textactions"
	"github.com/kombifyio/SpeechKit/internal/tray"
	"github.com/kombifyio/SpeechKit/internal/tts"
	"github.com/kombifyio/SpeechKit/internal/voiceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

var newHuggingFaceProvider = func(model, token string) stt.STTProvider {
	return stt.NewHuggingFaceProvider(model, token)
}

type logEntry struct {
	Message   string `json:"message"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
}

// appState holds shared state for UI updates.
type appState struct {
	mu                       sync.Mutex
	controlPlaneToken        string
	overlay                  overlayWindow
	pillAnchor               overlayWindow
	pillPanel                overlayWindow
	dotAnchor                overlayWindow
	radialMenu               overlayWindow
	dashboard                settingsWindow
	settings                 settingsWindow
	appTray                  trayStateSetter
	screenLocator            overlayScreenLocator
	logEntries               []logEntry
	transcriptions           int
	providers                []string
	hotkey                   string
	dictateHotkey            string
	assistHotkey             string
	voiceAgentHotkey         string
	dictateHotkeyBehavior    string
	assistHotkeyBehavior     string
	voiceAgentHotkeyBehavior string
	dictateEnabled           bool
	assistEnabled            bool
	voiceAgentEnabled        bool
	agentHotkey              string
	currentState             string
	overlayText              string
	overlayFeedbackRole      string
	overlayFeedbackText      string
	overlayFeedbackDone      bool
	overlayLevel             float64
	overlayPhase             string
	overlayVisualizer        string
	overlayDesign            string
	assistOverlayMode        string
	voiceAgentOverlayMode    string
	overlayEnabled           bool
	overlayPosition          string
	overlayMovable           bool
	overlayFreeX             int
	overlayFreeY             int
	overlayMonitorKey        string
	overlayMonitorCenters    map[string]config.OverlayFreePosition
	quickNoteMode            bool
	quickCaptureMode         bool
	quickCaptureAutoStart    bool  // when true, next PTT event auto-starts + auto-stops recording
	quickCaptureNoteID       int64 // the specific note ID this capture session writes to
	lastTranscriptionText    string
	vocabularyDictionary     string
	activeMode               string
	prompterMode             string
	audioDeviceID            string
	audioOutputDeviceID      string
	activeProfiles           map[string]string
	hkManager                hotkeyReconfigurer
	audioSession             audioDeviceReconfigurer
	engine                   *speechkit.Runtime
	sttRouter                *router.Router
	genkitRT                 *appai.Runtime
	summarizeFlow            *core.Flow[flows.SummarizeInput, string, struct{}]
	agentFlow                *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}]
	assistFlow               *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}]
	assistExecutor           assist.ToolExecutor
	assistPipeline           *assist.Pipeline
	assistBubble             overlayWindow
	prompterWindow           overlayWindow
	ttsRouter                *tts.Router
	audioPlayer              *audio.Player
	voiceAgentSession        *voiceagent.Session
	voiceAgentDialogTurns    []voiceAgentDialogTurn
	voiceAgentSummaryTool    textactions.SummaryTool
	streamPlayer             *audio.StreamPlayer
	voiceAgentAudioSender    *voiceAgentAudioSender
	voiceAgentEchoGuard      *voiceAgentEchoGuard
	wailsApp                 *application.App
	captureWin               *application.WebviewWindow
	doneResetDelay           time.Duration
	downloads                *downloads.Manager
	appUpdates               *appUpdateManager
	shuttingDown             bool
}

func showSettingsWindow(window settingsWindow) {
	if window == nil {
		return
	}

	window.Restore()
	window.UnMinimise()
	if !window.IsVisible() {
		window.Show()
	}
	window.Focus()
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

func (s *appState) setState(state, text string) {
	s.mu.Lock()
	s.currentState = state
	s.overlayText = text
	if state == "recording" || state == "idle" {
		s.overlayFeedbackRole = ""
		s.overlayFeedbackText = ""
		s.overlayFeedbackDone = true
	}
	if state != "recording" {
		s.overlayLevel = 0
	}
	s.overlayPhase = overlayPhase(state, normalizeOverlayLevel(s.overlayLevel))
	appTray := s.appTray
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()

	s.publishSpeechKitEvent(speechkitStateEvent(state, text))

	s.showActiveOverlayWindow()
	if appTray != nil {
		appTray.SetState(tray.State(state))
	}

	if state == "done" {
		go s.resetIdleAfter("done", s.doneResetDelayValue())
	}
}

func (s *appState) resetIdleAfter(expected string, delay time.Duration) {
	time.Sleep(delay)

	s.mu.Lock()
	current := s.currentState
	s.mu.Unlock()

	if current == expected {
		s.setState("idle", "")
	}
}

func (s *appState) addLog(msg, logType string) {
	entry := logEntry{
		Message:   msg,
		Type:      logType,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	s.mu.Lock()
	s.logEntries = append(s.logEntries, entry)
	if len(s.logEntries) > 200 {
		s.logEntries = s.logEntries[len(s.logEntries)-200:]
	}
	s.mu.Unlock()

	if event, ok := speechkitLogEvent(msg, logType); ok {
		s.publishSpeechKitEvent(event)
	}

	slog.Info(msg)
}

func main() {
	_, closeLogFile := initAppLogging()
	defer closeLogFile()

	cfgPath := runtimeConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("config load failed", "err", err)
		closeLogFile()
		os.Exit(1) //nolint:gocritic // exitAfterDefer: closeLogFile() called explicitly above before exit
	}
	normalizeConfigModes(cfg)
	applySelectedVoiceAgentProfile(cfg, filteredModelCatalog())
	if migrated, err := secrets.MigrateInstallTokenBootstrap(); err != nil {
		slog.Warn("migrate install hugging face token", "err", err)
	} else if migrated {
		slog.Info("install token migrated Hugging Face bootstrap token into secure storage")
	}
	// Load install state (local vs cloud mode)
	installState, err := config.LoadInstallState()
	if err != nil {
		slog.Warn("install state load failed", "err", err)
		installState = &config.InstallState{Mode: config.InstallModeLocal}
	}
	if installState.Mode == "" {
		// First run or not yet set -- default to local
		installState.Mode = config.InstallModeLocal
		installState.SetupDone = false
		if err := config.SaveInstallState(installState); err != nil {
			slog.Warn("save install state", "err", err)
		}
		slog.Info("install mode: local (default, first run — setup wizard pending)")
	} else {
		slog.Info("install mode", "mode", installState.Mode)
	}
	if config.ApplyLocalInstallDefaults(cfg, installState) {
		if err := config.Save(cfgPath, cfg); err != nil {
			slog.Warn("save local install defaults", "err", err)
		} else {
			slog.Info("local install defaults: onboarding will download a local model before enabling whisper.cpp")
		}
	}

	if config.ApplyManagedIntegrationDefaults(cfg) {
		slog.Info("managed integration: Hugging Face enabled by explicit opt-in with resolved credentials")
	}

	state := &appState{
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

	// Build router and track provider status
	r, providerLog := buildRouter(cfg)
	syncRuntimeProviders(context.Background(), state, r) //nolint:contextcheck // ctx not yet created at startup initialization
	for _, msg := range providerLog {
		slog.Info(msg)
	}

	// Audio capture
	audioCfg := audio.Config{
		Backend:     audio.Backend(cfg.Audio.Backend),
		DeviceID:    cfg.Audio.DeviceID,
		SampleRate:  cfg.Audio.SampleRate,
		Channels:    cfg.Audio.Channels,
		FrameSizeMs: cfg.Audio.FrameSizeMs,
		LatencyHint: cfg.Audio.LatencyHint,
	}
	capturer, err := newReconfigurableAudioSession(audioCfg, audio.Open)
	if err != nil {
		slog.Error("audio init failed", "err", err)
		os.Exit(1)
	}
	state.audioSession = capturer
	defer func() {
		if err := capturer.Close(); err != nil {
			slog.Warn("audio close", "err", err)
		}
	}()

	go func() {
		for event := range capturer.Events() {
			switch event.Type {
			case audio.EventError:
				state.addLog(fmt.Sprintf("Audio backend error: %v", event.Err), "error")
			case audio.EventWarning:
				msg := event.Message
				if msg == "" && event.Err != nil {
					msg = event.Err.Error()
				}
				if msg != "" {
					state.addLog(fmt.Sprintf("Audio backend warning: %s", msg), "warn")
				}
			default:
				// EventStarted, EventStopped — no log action needed.
			}
		}
	}()

	dictationVAD, closeDictationVAD, err := newDictationVAD()
	if err != nil {
		slog.Warn("dictation VAD unavailable", "err", err)
	} else {
		if closeDictationVAD != nil {
			defer closeDictationVAD()
		}
		if dictationVAD != nil {
			state.addLog("Dictation VAD ready", "info")
		}
	}

	// Silence detection for Quick Capture auto-stop
	silenceAutoStop := make(chan struct{}, 1)
	silenceThreshold := 0.01 // RMS below this = silence
	var silenceSince time.Time
	fastModeDuration := time.Duration(cfg.General.FastModeSilenceMs) * time.Millisecond
	if fastModeDuration <= 0 {
		fastModeDuration = 1500 * time.Millisecond
	}

	capturer.SetLevelHandler(func(level float64) {
		state.setLevel(level)

		state.mu.Lock()
		voiceSession := state.voiceAgentSession
		state.mu.Unlock()
		if voiceSession != nil {
			state.updatePrompterActivity("user", voiceAgentUserActivityLevel(voiceSession.CurrentState(), level, state.currentVoiceAgentEchoGuard()))
		}

		// Only do silence detection when Quick Capture is active
		if !state.quickCaptureModeActive() {
			silenceSince = time.Time{}
			return
		}

		if level < silenceThreshold {
			if silenceSince.IsZero() {
				silenceSince = time.Now()
			} else if time.Since(silenceSince) >= fastModeDuration {
				// Silence exceeded threshold -- trigger auto-stop
				select {
				case silenceAutoStop <- struct{}{}:
				default:
				}
				silenceSince = time.Time{}
			}
		} else {
			silenceSince = time.Time{} // reset on speech
		}
	})

	state.sttRouter = r

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Genkit AI initialization: replaces the old LLM registries.
	genkitRT, err := appai.Init(ctx, buildGenkitConfig(cfg))
	if err != nil {
		slog.Warn("genkit init", "err", err)
		state.addLog("AI providers unavailable — Assist and Voice Agent disabled", "warn")
	} else {
		state.genkitRT = genkitRT

		// Define flows.
		state.summarizeFlow = flows.DefineSummarizeFlow(genkitRT.G, genkitRT.UtilityModels())
		state.agentFlow = flows.DefineAgentFlow(genkitRT.G, genkitRT.AgentModels())
		if len(genkitRT.AssistModels()) > 0 {
			state.assistFlow = flows.DefineAssistFlow(genkitRT.G, genkitRT.AssistModels())
		}

		slog.Info("genkit initialized",
			"utility_models", len(genkitRT.UtilityModels()),
			"assist_models", len(genkitRT.AssistModels()),
			"agent_models", len(genkitRT.AgentModels()),
		)
	}

	// TTS initialization for Assist Mode.
	ttsRouter := buildTTSRouter(cfg)
	state.ttsRouter = ttsRouter
	if ttsRouter != nil {
		healthResults := ttsRouter.HealthCheck(ctx)
		for name, err := range healthResults {
			if err != nil {
				slog.Warn("TTS provider unavailable", "provider", name, "err", err)
			} else {
				slog.Info("TTS provider ready", "provider", name)
			}
		}
	}

	// Audio player for TTS output.
	audioPlayer, err := audio.NewPlayer()
	if err != nil {
		slog.Warn("audio player init", "err", err)
		state.addLog("TTS audio player unavailable — voice output disabled", "warn")
	} else {
		state.audioPlayer = audioPlayer
		defer audioPlayer.Close()
		slog.Info("audio player ready (24kHz mono)")
	}

	// Clipboard output and desktop quick actions.
	clipHandler := output.NewClipboardHandler()
	shortcutResolver := buildShortcutResolver(cfg)
	quickActions := newQuickActionCoordinator(state, clipHandler, shortcutResolver)
	quickActions.summarizer.Summarizer = textactions.SummarizerFunc(func(ctx context.Context, input textactions.Input) (string, error) {
		state.mu.Lock()
		flow := state.summarizeFlow
		state.mu.Unlock()
		return (&textactions.FlowSummarizer{Flow: flow}).Summarize(ctx, input)
	})
	state.assistExecutor = newAssistToolExecutor(quickActions)

	// Assist Pipeline: STT → Codeword → LLM → TTS → Result{Text, Audio}
	if state.assistFlow != nil || state.assistExecutor != nil {
		state.assistPipeline = assist.NewPipeline(
			state.assistFlow,
			state.assistExecutor,
			ttsRouter,
			cfg.TTS.Enabled,
			assist.WithRouter(assist.NewRouter(assist.WithResolver(shortcutResolver))),
		)
		slog.Info("assist pipeline ready")
	}

	// Voice Agent session (pre-created, started on demand via hotkey).
	if cfg.VoiceAgent.Enabled {
		state.voiceAgentSession = prepareVoiceAgentSession(state, cfg)
		slog.Info("voice agent session prepared")
	}

	// Hotkeys for Dictate, Assist and Voice Agent.
	hkManager := newModeHotkeyManager(configuredModeCombos(
		cfg.General.DictateEnabled,
		cfg.General.AssistEnabled,
		cfg.General.VoiceAgentEnabled,
		cfg.General.DictateHotkey,
		cfg.General.AssistHotkey,
		cfg.General.VoiceAgentHotkey,
	))
	state.hkManager = hkManager

	// Store (interface-based, backend selected via config)
	var feedbackStore store.Store
	feedbackStore, err = store.New(store.StoreConfig{
		Backend:                 cfg.Store.Backend,
		SQLitePath:              cfg.Store.SQLitePath,
		SaveAudio:               cfg.Store.SaveAudio,
		AudioRetentionDays:      cfg.Store.AudioRetentionDays,
		MaxAudioStorageMB:       cfg.Store.MaxAudioStorageMB,
		PostgresDSN:             cfg.Store.PostgresDSN,
		TranscriptionModelHints: configuredTranscriptionModelHints(cfg),
	})
	if err != nil {
		slog.Warn("store init", "err", err)
		feedbackStore = nil
	} else {
		defer func() { _ = feedbackStore.Close() }()
		count, _ := feedbackStore.TranscriptionCount(context.Background())
		state.transcriptions = count
		state.syncSpeechKitSnapshot()
		slog.Info("store ready", "records", count, "backend", cfg.Store.Backend)
	}

	var dashboardWin *application.WebviewWindow
	createDashboardWindow := func(wailsApp *application.App) *application.WebviewWindow {
		win := wailsApp.Window.NewWithOptions(newDashboardWindowOptions())
		win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
			if !state.shouldHideWindowOnClose() {
				return
			}
			event.Cancel()
			win.Hide()
		})
		return win
	}
	showDashboard := func(source string) {
		slog.Info("dashboard requested", "source", source)
		state.showDashboardWindow(source)
	}

	// Wails app
	app := application.New(application.Options{
		Name: "kombify SpeechKit",
		Icon: appassets.SpeechKitICO(),
		Windows: application.WindowsOptions{
			EnabledFeatures: []string{"msWebView2EnableDraggableRegions"},
		},
		Assets: application.AssetOptions{
			Handler: assetHandler(cfg, cfgPath, state, r, feedbackStore, installState),
		},
		PanicHandler: func(details *application.PanicDetails) {
			slog.Error("wails panic", "err", details.Error, "stack", details.StackTrace)
		},
		WarningHandler: func(message string) {
			slog.Warn("wails warning", "message", message)
		},
		ErrorHandler: func(err error) {
			slog.Error("wails error", "err", err)
		},
		ShouldQuit: func() bool {
			state.beginShutdown()
			return true
		},
		OnShutdown: func() {
			state.beginShutdown()
			cancel()
			state.stopVoiceAgentAudioSender()
			state.stopVoiceAgentStream()
			if capturer != nil {
				capturer.SetPCMHandler(nil)
			}
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.kombify.speechkit",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				slog.Info("second instance launch blocked", "cwd", data.WorkingDir, "args", data.Args)
				if state.engine != nil {
					_ = state.engine.Commands().Dispatch(context.Background(), speechkit.Command{
						Type: speechkit.CommandShowDashboard,
						Metadata: map[string]string{
							"source": "second-instance",
						},
					})
				} else {
					showDashboard("second-instance")
				}
			},
		},
	})

	state.wailsApp = app

	// Overlay windows
	pillAnchorWindow := app.Window.NewWithOptions(newPillAnchorWindowOptions())
	pillPanelWindow := app.Window.NewWithOptions(newPillPanelWindowOptions())
	dotAnchorWindow := app.Window.NewWithOptions(newDotAnchorWindowOptions())
	radialMenuWindow := app.Window.NewWithOptions(newRadialMenuWindowOptions())
	assistBubbleWindow := app.Window.NewWithOptions(newAssistBubbleWindowOptions())

	state.pillAnchor = pillAnchorWindow
	state.pillPanel = pillPanelWindow
	state.dotAnchor = dotAnchorWindow
	state.radialMenu = radialMenuWindow
	state.assistBubble = assistBubbleWindow

	var inputController *desktopInputController
	prompterWin := app.Window.NewWithOptions(newPrompterWindowOptions())
	prompterWin.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if !state.shouldHideWindowOnClose() {
			return
		}
		event.Cancel()
		if inputController != nil {
			inputController.closeVoiceAgentPrompter(ctx)
			return
		}
		prompterWin.Hide()
	})
	state.prompterWindow = prompterWin

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
		if !pillPanelWindow.IsVisible() {
			return
		}
		x, y := pillPanelWindow.Position()
		if !state.updateOverlayFreeCenterFromPanel(x, y) {
			return
		}
		scheduleOverlayMoveSave()
	})

	// Dashboard: main product window (Dashboard/Settings/Logs tabs)
	dashboardWin = createDashboardWindow(app)
	state.dashboard = dashboardWin
	state.settings = dashboardWin

	// System tray
	appTray := tray.New(app, func() {
		state.addLog("Quit requested from tray", "info")
		state.beginShutdown()
		app.Quit()
	}, func() {
		state.addLog("Dashboard requested from tray", "info")
		if state.engine != nil {
			_ = state.engine.Commands().Dispatch(context.Background(), speechkit.Command{
				Type: speechkit.CommandShowDashboard,
				Metadata: map[string]string{
					"source": "tray",
				},
			})
			return
		}
		showDashboard("tray")
	})
	appTray.OnFeedback = func() {
		_ = exec.Command("explorer", "https://github.com/kombifyio/SpeechKit/issues").Start() //nolint:noctx // fire-and-forget browser open; no caller context available
	}
	state.appTray = appTray

	// On app start: make overlay click-through and position it via the first sync tick.
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(event *application.ApplicationEvent) {
		state.positionOverlay()
		state.setState("idle", "")
		maybeAutoStartVoiceAgentOnLaunch(ctx, cfg, inputController)
	})

	// Listen for prompter controls.
	app.Event.On("voiceagent:start", func(_ *application.CustomEvent) {
		if inputController == nil {
			return
		}
		inputController.activateVoiceAgent(ctx)
	})
	app.Event.On("voiceagent:stop", func(_ *application.CustomEvent) {
		if inputController == nil {
			return
		}
		inputController.deactivateVoiceAgentWithReason(ctx, true, "stop control")
	})
	app.Event.On("voiceagent:close", func(_ *application.CustomEvent) {
		if inputController == nil {
			return
		}
		inputController.closeVoiceAgentPrompter(ctx)
	})

	for _, msg := range validateCloudProviders(ctx, r) {
		state.addLog(msg, "info")
	}
	syncRuntimeProviders(ctx, state, r)

	if localProvider, ok := r.Local().(localProviderStarter); ok {
		startLocalProviderAsync(ctx, state, r, localProvider)
	}

	if err := hkManager.Start(ctx); err != nil {
		slog.Error("hotkey start failed", "err", err)
		os.Exit(1)
	}
	defer hkManager.Stop()

	state.setActiveMode(cfg.General.ActiveMode)
	state.addLog(fmt.Sprintf("Dictate hotkey: %s", cfg.General.DictateHotkey), "info")
	if cfg.General.AssistHotkey != "" {
		state.addLog(fmt.Sprintf("Assist hotkey: %s", cfg.General.AssistHotkey), "info")
	}
	if cfg.General.VoiceAgentHotkey != "" {
		state.addLog(fmt.Sprintf("Voice Agent hotkey: %s", cfg.General.VoiceAgentHotkey), "info")
	}
	state.addLog(fmt.Sprintf("Providers: %s", strings.Join(state.providers, ", ")), "info")
	if len(state.providers) == 0 {
		if r.Local() != nil {
			state.addLog("Waiting for local STT startup...", "info")
		} else {
			if hint := missingProviderHint(cfg); hint != "" {
				state.addLog(hint, "error")
			}
			state.addLog("No STT provider ready", "error")
		}
	} else {
		state.addLog("Ready", "success")
	}

	go func() {
		ticker := time.NewTicker(900 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				state.syncOverlayToActiveScreen()
			case <-ctx.Done():
				return
			}
		}
	}()

	transcriptionWorker, err := speechkit.NewTranscriptionWorker(speechkit.TranscriptionWorkerConfig{
		Timeout:   30 * time.Second,
		QueueSize: 4,
		Runner: speechkit.NewTranscriptionRunner(
			routerTranscriber{
				router:          r,
				state:           state,
				dictionaryStore: userDictionaryStoreFromFeedbackStore(feedbackStore),
			},
			speechkitStoreAdapter{store: feedbackStore},
		).WithObserver(speechkitCommitObserver{state: state}),
		Output: desktopTranscriptOutput{
			cfg:         cfg,
			state:       state,
			handler:     clipHandler,
			interceptor: quickActions,
			playbackCtx: ctx,
			activeMode: func() string {
				state.mu.Lock()
				defer state.mu.Unlock()
				return state.activeMode
			},
			onAssistText: func(text string) {
				trimmed := strings.TrimSpace(text)
				state.addLog(
					fmt.Sprintf(
						"Assist response ready (%d chars, %d words)",
						utf8.RuneCountInString(trimmed),
						len(strings.Fields(trimmed)),
					),
					"info",
				)
				state.showAssistBubble(text)
			},
		},
		Observer: state,
	})
	if err != nil {
		slog.Error("transcription worker init failed", "err", err)
		os.Exit(1)
	}
	transcriptionWorker.Start(ctx)
	defer func() {
		transcriptionWorker.Close()
		transcriptionWorker.Wait()
	}()
	quickNoteService := desktopQuickNoteService{
		cfg:           cfg,
		state:         state,
		feedbackStore: feedbackStore,
		host:          wailsQuickNoteHost{state: state},
	}
	recordingController := speechkit.NewRecordingController(
		capturer,
		transcriptionWorker,
		state,
		func() speechkit.SegmentCollector {
			if dictationVAD == nil {
				return nil
			}
			pauseThreshold := 700 * time.Millisecond
			if cfg.General.AutoStopSilenceMs > 0 {
				pauseThreshold = time.Duration(cfg.General.AutoStopSilenceMs) * time.Millisecond
			}
			return speechkit.NewDictationSegmenter(dictationVAD, pauseThreshold)
		},
	)
	state.engine = newSpeechKitRuntime(state, speechkit.Hooks{
		HandleCommand: desktopCommandHandler{
			cfg:                 cfg,
			state:               state,
			recordingController: recordingController,
			feedbackStore:       feedbackStore,
			showDashboard:       showDashboard,
			quickNotes:          quickNoteService,
			actions:             quickActions,
		}.Handle,
	})
	defer state.engine.Close()
	state.syncSpeechKitSnapshot()

	// Unified event loop: handles PTT, Quick Capture auto-record, and silence auto-stop
	inputController = &desktopInputController{
		commands:          state.engine.Commands(),
		recording:         recordingController,
		state:             state,
		hotkeyEvents:      hkManager.Events(),
		silenceAutoStop:   silenceAutoStop,
		autoStartInterval: 200 * time.Millisecond,
		voiceAgentSession: state.voiceAgentSession,
		voiceAgentConfig:  &cfg.VoiceAgent,
		cfg:               cfg,
		installState:      installState,
		sttRouter:         r,
		audioCapturer:     capturer,
	}
	go inputController.Run(ctx)

	if err := app.Run(); err != nil {
		slog.Error("app run failed", "err", err)
		os.Exit(1)
	}
	cancel()
}

// buildRouter, buildGenkitConfig, buildTTSRouter, validateCloudProviders,
// missingProviderHint, executableDir, defaultLocalModelPath, escapeJS, and
// runtimeConfigPath are in app_init.go.
