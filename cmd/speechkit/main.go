package main

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/firebase/genkit/go/core"

	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/downloads"
	_ "github.com/kombifyio/SpeechKit/internal/kombify"
	"github.com/kombifyio/SpeechKit/internal/router"
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
	quickNoteAutoStop        bool  // enables silence auto-stop for editor-armed quick-note recording
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
	localLLMRuntime          localLLMRuntimeStarter
	voiceAgentSession        *voiceagent.Session
	voiceAgentDialogTurns    []voiceAgentDialogTurn
	voiceAgentSessionStarted time.Time
	voiceAgentSummaryDone    bool
	voiceAgentStore          store.VoiceAgentSessionStore
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

	// serverDelegates holds optional per-mode adapters that delegate to a
	// remote SpeechKit Server-Target. Nil when every mode runs locally
	// (the pre-0.26 default). See server_delegates.go.
	serverDelegates *serverDelegates
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

	runDesktopApp(closeLogFile)
}

func runDesktopApp(closeLogFile func()) {
	var cleanup desktopCleanupStack
	defer cleanup.Close()

	cfgPath, cfg, installState, err := loadDesktopStartupConfig()
	if err != nil {
		slog.Error("config load failed", "err", err)
		closeLogFile()
		os.Exit(1) //nolint:gocritic // exitAfterDefer: closeLogFile() called explicitly above before exit
	}

	state := newInitialAppState(cfg)

	r := initDesktopRouterRuntime(cfg, state)
	audioRuntime, err := initDesktopAudioRuntime(cfg, state, &cleanup)
	if err != nil {
		slog.Error("audio init failed", "err", err)
		os.Exit(1)
	}

	ctx, cancel := initDesktopAppContext(state, &cleanup)
	services := initDesktopRuntimeServices(ctx, cfg, state, &cleanup)
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

	startDesktopProviderReadiness(ctx, state, r)
	if err := startDesktopHotkeys(ctx, hkManager, &cleanup); err != nil {
		slog.Error("hotkey start failed", "err", err)
		os.Exit(1)
	}

	logDesktopStartupReady(cfg, state, r)
	startDesktopOverlaySyncLoop(ctx, state)
	inputController, err = startDesktopInputRuntime(ctx, cfg, state, r, feedbackStore, audioRuntime, services, hkManager, installState, showDashboard, &cleanup)
	if err != nil {
		slog.Error("desktop mode runtime init failed", "err", err)
		os.Exit(1)
	}

	if err := app.Run(); err != nil {
		slog.Error("app run failed", "err", err)
		os.Exit(1)
	}
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

// buildRouter, buildGenkitConfig, buildTTSRouter, validateCloudProviders,
// missingProviderHint, executableDir, defaultLocalModelPath, escapeJS, and
// runtimeConfigPath are in app_init.go.
