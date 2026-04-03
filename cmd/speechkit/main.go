package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/firebase/genkit/go/core"

	appassets "github.com/kombifyio/SpeechKit/assets"
	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/hotkey"
	_ "github.com/kombifyio/SpeechKit/internal/kombify"
	"github.com/kombifyio/SpeechKit/internal/models"
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
	mu                    sync.Mutex
	overlay               overlayWindow
	pillAnchor            overlayWindow
	pillPanel             overlayWindow
	dotAnchor             overlayWindow
	radialMenu            overlayWindow
	dashboard             settingsWindow
	settings              settingsWindow
	appTray               trayStateSetter
	screenLocator         overlayScreenLocator
	logEntries            []logEntry
	transcriptions        int
	providers             []string
	hotkey                string
	dictateHotkey         string
	agentHotkey           string
	currentState          string
	overlayText           string
	overlayLevel          float64
	overlayPhase          string
	overlayVisualizer     string
	overlayDesign         string
	overlayEnabled        bool
	overlayPosition       string
	quickNoteMode         bool
	quickCaptureMode      bool
	quickCaptureAutoStart bool  // when true, next PTT event auto-starts + auto-stops recording
	quickCaptureNoteID    int64 // the specific note ID this capture session writes to
	lastTranscriptionText string
	activeMode            string
	audioDeviceID         string
	activeProfiles        map[string]string
	hkManager             hotkeyReconfigurer
	audioSession          audioDeviceReconfigurer
	engine                *speechkit.Runtime
	sttRouter             *router.Router
	genkitRT              *appai.Runtime
	summarizeFlow         *core.Flow[flows.SummarizeInput, string, struct{}]
	agentFlow             *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}]
	assistFlow            *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}]
	assistPipeline        *assist.Pipeline
	assistBubble          overlayWindow
	ttsRouter             *tts.Router
	audioPlayer           *audio.Player
	voiceAgentSession     *voiceagent.Session
	wailsApp              *application.App
	captureWin            *application.WebviewWindow
	doneResetDelay        time.Duration
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

func (s *appState) setState(state, text string) {
	s.mu.Lock()
	s.currentState = state
	s.overlayText = text
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

	switch state {
	case "done":
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

	log.Println(msg)
}

func main() {
	_, closeLogFile := initAppLogging()
	defer closeLogFile()

	cfgPath := runtimeConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if migrated, err := secrets.MigrateInstallTokenBootstrap(); err != nil {
		log.Printf("WARN: migrate install hugging face token: %v", err)
	} else if migrated {
		log.Printf("Install token: migrated Hugging Face bootstrap token into secure storage")
	}
	// Load install state (local vs cloud mode)
	installState, err := config.LoadInstallState()
	if err != nil {
		log.Printf("WARN: install state: %v", err)
		installState = &config.InstallState{Mode: config.InstallModeLocal}
	}
	if installState.Mode == "" {
		// First run or not yet set -- default to local
		installState.Mode = config.InstallModeLocal
		installState.SetupDone = false
		_ = config.SaveInstallState(installState)
		log.Println("Install mode: local (default, first run — setup wizard pending)")
	} else {
		log.Printf("Install mode: %s", installState.Mode)
	}
	if config.ApplyLocalInstallDefaults(cfg, installState) {
		if err := config.Save(cfgPath, cfg); err != nil {
			log.Printf("WARN: save local install defaults: %v", err)
		} else {
			log.Printf("Local install defaults: enabled bundled local runtime")
		}
	}

	if config.ApplyManagedIntegrationDefaults(cfg) {
		log.Printf("Managed integration: Hugging Face enabled by explicit opt-in with resolved credentials")
	}

	state := &appState{
		hotkey:            cfg.General.DictateHotkey,
		dictateHotkey:     cfg.General.DictateHotkey,
		agentHotkey:       cfg.General.AgentHotkey,
		activeMode:        cfg.General.ActiveMode,
		audioDeviceID:     cfg.Audio.DeviceID,
		activeProfiles:    defaultActiveProfiles(models.DefaultCatalog()),
		providers:         []string{},
		overlayEnabled:    cfg.UI.OverlayEnabled,
		overlayPosition:   cfg.UI.OverlayPosition,
		overlayVisualizer: cfg.UI.Visualizer,
		overlayDesign:     cfg.UI.Design,
		screenLocator:     newActiveWindowScreenLocator(),
	}

	// Build router and track provider status
	r, providerLog := buildRouter(cfg)
	state.providers = r.AvailableProviders()
	state.syncSpeechKitSnapshot()
	for _, msg := range providerLog {
		log.Println(msg)
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
		log.Fatalf("audio init failed: %v", err)
	}
	state.audioSession = capturer
	defer func() {
		if err := capturer.Close(); err != nil {
			log.Printf("WARN: audio close: %v", err)
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
			}
		}
	}()

	dictationVAD, closeDictationVAD, err := newDictationVAD()
	if err != nil {
		log.Printf("WARN: dictation VAD unavailable: %v", err)
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
		log.Printf("WARN: genkit init: %v", err)
	} else {
		state.genkitRT = genkitRT

		// Define flows.
		state.summarizeFlow = flows.DefineSummarizeFlow(genkitRT.G, genkitRT.UtilityModels())
		state.agentFlow = flows.DefineAgentFlow(genkitRT.G, genkitRT.AgentModels())
		state.assistFlow = flows.DefineAssistFlow(genkitRT.G, genkitRT.UtilityModels())

		log.Printf("Genkit: %d utility models, %d agent models", len(genkitRT.UtilityModels()), len(genkitRT.AgentModels()))
	}

	// TTS initialization for Assist Mode.
	ttsRouter := buildTTSRouter(cfg)
	state.ttsRouter = ttsRouter
	if ttsRouter != nil {
		healthResults := ttsRouter.HealthCheck(ctx)
		for name, err := range healthResults {
			if err != nil {
				log.Printf("TTS: %s unavailable: %v", name, err)
			} else {
				log.Printf("TTS: %s ready", name)
			}
		}
	}

	// Audio player for TTS output.
	audioPlayer, err := audio.NewPlayer()
	if err != nil {
		log.Printf("WARN: audio player init: %v", err)
	} else {
		state.audioPlayer = audioPlayer
		defer audioPlayer.Close()
		log.Println("Audio player: ready (24kHz mono)")
	}

	// Assist Pipeline: STT → Codeword → LLM → TTS → Result{Text, Audio}
	if state.assistFlow != nil {
		state.assistPipeline = assist.NewPipeline(
			state.assistFlow,
			ttsRouter,
			cfg.TTS.Enabled,
		)
		log.Println("Assist pipeline: ready")
	}

	// Voice Agent session (pre-created, started on demand via hotkey).
	if cfg.VoiceAgent.Enabled && cfg.General.AgentMode == "voice_agent" {
		geminiProvider := voiceagent.NewGeminiLive()
		state.voiceAgentSession = voiceagent.NewSession(geminiProvider, voiceagent.Callbacks{
			OnStateChange: func(vaState voiceagent.State) {
				state.addLog(fmt.Sprintf("Voice Agent: %s", vaState), "info")
			},
			OnAudio: func(audioData []byte) {
				if state.audioPlayer != nil {
					go func() {
						if err := state.audioPlayer.PlayPCM(context.Background(), audioData, 24000); err != nil {
							log.Printf("Voice Agent playback error: %v", err)
						}
					}()
				}
			},
			OnText: func(text string) {
				state.showAssistBubble(text)
			},
			OnError: func(err error) {
				state.addLog(fmt.Sprintf("Voice Agent error: %v", err), "error")
			},
		})
		log.Println("Voice Agent: session prepared (start via agent hotkey)")
	}

	// Clipboard output
	clipHandler := output.NewClipboardHandler()
	quickActions := newQuickActionCoordinator(state, clipHandler)
	if state.genkitRT != nil && len(state.genkitRT.UtilityModels()) > 0 {
		quickActions.summarizer.Summarizer = &textactions.FlowSummarizer{Flow: state.summarizeFlow}
	}

	// Hotkeys for Dictate and Agent mode
	hkManager := newDualHotkeyManager(
		hotkey.ParseCombo(cfg.General.DictateHotkey),
		hotkey.ParseCombo(cfg.General.AgentHotkey),
		func() string {
			state.mu.Lock()
			defer state.mu.Unlock()
			return state.activeMode
		},
	)
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
		log.Printf("WARN: store: %v", err)
		feedbackStore = nil
	} else {
		defer feedbackStore.Close()
		count, _ := feedbackStore.TranscriptionCount(context.Background())
		state.transcriptions = count
		state.syncSpeechKitSnapshot()
		log.Printf("Store: %d records (backend: %s)", count, cfg.Store.Backend)
	}

	var dashboardWin *application.WebviewWindow
	createDashboardWindow := func(wailsApp *application.App) *application.WebviewWindow {
		win := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
			Title:            "kombify SpeechKit",
			Width:            1200,
			Height:           820,
			MinWidth:         900,
			MinHeight:        650,
			InitialPosition:  application.WindowCentered,
			Hidden:           true,
			URL:              "/dashboard.html",
			BackgroundColour: application.NewRGBA(11, 15, 20, 255),
			Windows: application.WindowsWindow{
				Theme: application.Dark,
			},
		})
		win.OnWindowEvent(events.Common.WindowClosing, func(event *application.WindowEvent) {
			event.Cancel()
			win.Hide()
		})
		return win
	}
	showDashboard := func(source string) {
		log.Printf("Dashboard requested via %s", source)
		application.InvokeSync(func() {
			// Re-create window if it was destroyed
			if dashboardWin == nil {
				return
			}
			showSettingsWindow(dashboardWin)
		})
	}

	// Wails app
	app := application.New(application.Options{
		Name: "kombify SpeechKit",
		Icon: appassets.SpeechKitICO(),
		Assets: application.AssetOptions{
			Handler: assetHandler(cfg, cfgPath, state, r, feedbackStore, installState),
		},
		PanicHandler: func(details *application.PanicDetails) {
			log.Printf("PANIC: %v\n%s", details.Error, details.StackTrace)
		},
		WarningHandler: func(message string) {
			log.Printf("WAILS WARN: %s", message)
		},
		ErrorHandler: func(err error) {
			log.Printf("WAILS ERROR: %v", err)
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.kombify.speechkit",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				log.Printf("Second instance launch blocked: cwd=%s args=%v", data.WorkingDir, data.Args)
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
	state.pillAnchor = app.Window.NewWithOptions(newPillAnchorWindowOptions())
	state.pillPanel = app.Window.NewWithOptions(newPillPanelWindowOptions())
	state.dotAnchor = app.Window.NewWithOptions(newDotAnchorWindowOptions())
	state.radialMenu = app.Window.NewWithOptions(newRadialMenuWindowOptions())
	state.assistBubble = app.Window.NewWithOptions(newAssistBubbleWindowOptions())

	// Dashboard: main product window (Dashboard/Settings/Logs tabs)
	dashboardWin = createDashboardWindow(app)
	state.dashboard = dashboardWin
	state.settings = dashboardWin

	// System tray
	appTray := tray.New(app, func() {
		state.addLog("Quit requested from tray", "info")
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
		_ = exec.Command("explorer", "https://github.com/kombifyio/SpeechKit/issues").Start()
	}
	state.appTray = appTray

	// On app start: make overlay click-through and position it via the first sync tick.
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(event *application.ApplicationEvent) {
		state.positionOverlay()
		state.setState("idle", "")
	})

	for _, msg := range validateCloudProviders(ctx, r) {
		state.addLog(msg, "info")
	}
	state.providers = r.AvailableProviders()
	state.syncSpeechKitSnapshot()

	if localProvider, ok := r.Local().(*stt.LocalProvider); ok {
		go func() {
			state.addLog("Starting local STT...", "info")
			if err := localProvider.StartServer(ctx); err != nil {
				state.addLog(fmt.Sprintf("Local STT unavailable: %v", err), "warn")
				return
			}
			state.addLog("Local STT ready", "success")
		}()
	}

	if err := hkManager.Start(ctx); err != nil {
		log.Fatalf("hotkey: %v", err)
	}
	defer hkManager.Stop()

	state.setActiveMode(cfg.General.ActiveMode)
	state.addLog(fmt.Sprintf("Dictate hotkey: %s", cfg.General.DictateHotkey), "info")
	state.addLog(fmt.Sprintf("Agent hotkey: %s", cfg.General.AgentHotkey), "info")
	state.addLog(fmt.Sprintf("Providers: %s", strings.Join(state.providers, ", ")), "info")
	if len(state.providers) == 0 {
		if hint := missingProviderHint(cfg); hint != "" {
			state.addLog(hint, "error")
		}
		state.addLog("No STT provider ready", "error")
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
			routerTranscriber{router: r},
			speechkitStoreAdapter{store: feedbackStore},
		).WithObserver(speechkitCommitObserver{state: state}),
		Output: desktopTranscriptOutput{
			handler:        clipHandler,
			interceptor:    quickActions,
			agentFlow:      state.agentFlow,
			assistPipeline: state.assistPipeline,
			audioPlayer:    state.audioPlayer,
			activeMode: func() string {
				state.mu.Lock()
				defer state.mu.Unlock()
				return state.activeMode
			},
			agentMode: func() string {
				return cfg.General.AgentMode
			},
			onAssistText: func(text string) {
				state.addLog(fmt.Sprintf("Assist: %s", text), "info")
				state.showAssistBubble(text)
			},
		},
		Observer: state,
	})
	if err != nil {
		log.Fatalf("transcription worker: %v", err)
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
	go func() {
		desktopInputController{
			commands:          state.engine.Commands(),
			recording:         recordingController,
			state:             state,
			hotkeyEvents:      hkManager.Events(),
			silenceAutoStop:   silenceAutoStop,
			autoStartInterval: 200 * time.Millisecond,
			voiceAgentSession: state.voiceAgentSession,
			voiceAgentConfig:  &cfg.VoiceAgent,
			cfg:               cfg,
		}.Run(ctx)
	}()

	if err := app.Run(); err != nil {
		log.Fatalf("app: %v", err)
	}
	cancel()
}

func buildRouter(cfg *config.Config) (*router.Router, []string) {
	var msgs []string
	r := &router.Router{
		Strategy:             router.Strategy(cfg.Routing.Strategy),
		PreferLocalUnderSecs: cfg.Routing.PreferLocalUnderSeconds,
		ParallelCloud:        cfg.Routing.ParallelCloud,
		ReplaceOnBetter:      cfg.Routing.ReplaceOnBetter,
	}

	if cfg.HuggingFace.Enabled {
		hfToken, tokenStatus, err := config.ResolveHuggingFaceToken(cfg)
		if err != nil || hfToken == "" {
			tokenEnv := config.HuggingFaceTokenEnvName(cfg)
			if tokenEnv == "" {
				tokenEnv = "HF_TOKEN"
			}
			msgs = append(msgs, fmt.Sprintf("WARN: %s not found in host secret store, env or Doppler", tokenEnv))
		} else {
			r.SetHuggingFace(newHuggingFaceProvider(cfg.HuggingFace.Model, hfToken))
			msgs = append(msgs, fmt.Sprintf("HuggingFace: %s (source: %s)", cfg.HuggingFace.Model, tokenStatus.ActiveSource))
		}
	}

	if cfg.VPS.Enabled && cfg.VPS.URL != "" {
		apiKey := config.ResolveSecret(cfg.VPS.APIKeyEnv)
		r.SetVPS(stt.NewVPSProvider(cfg.VPS.URL, apiKey))
		msgs = append(msgs, fmt.Sprintf("VPS: %s", cfg.VPS.URL))
	}

	if cfg.Local.Enabled {
		modelPath := cfg.Local.ModelPath
		if modelPath == "" {
			modelPath = defaultLocalModelPath(executableDir(), os.Getenv("LOCALAPPDATA"), cfg.Local.Model)
		}
		r.SetLocal(stt.NewLocalProvider(cfg.Local.Port, modelPath, cfg.Local.GPU))
		msgs = append(msgs, fmt.Sprintf("Local: %s (not started)", cfg.Local.Model))
	}

	// Additional STT cloud providers
	if cfg.Providers.Groq.Enabled {
		apiKey := config.ResolveSecret(cfg.Providers.Groq.APIKeyEnv)
		if apiKey != "" {
			r.AddCloud(stt.NewGroqSTTProvider(apiKey))
			msgs = append(msgs, "STT: Groq provider registered")
		}
	}

	if cfg.Providers.OpenAI.Enabled {
		apiKey := config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv)
		if apiKey != "" {
			r.AddCloud(stt.NewOpenAISTTProvider(apiKey))
			msgs = append(msgs, "STT: OpenAI provider registered")
		}
	}

	if cfg.Providers.Google.Enabled {
		apiKey := config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)
		if apiKey != "" {
			r.AddCloud(stt.NewGoogleSTTProvider(apiKey, cfg.Providers.Google.STTModel))
			msgs = append(msgs, "STT: Google provider registered")
		}
	}

	return r, msgs
}

func executableDir() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return ""
	}
	return filepath.Dir(exe)
}

func defaultLocalModelPath(exeDir string, localAppData string, modelName string) string {
	if exeDir != "" && modelName != "" {
		bundlePath := filepath.Join(exeDir, "models", modelName)
		if _, err := os.Stat(bundlePath); err == nil {
			return bundlePath
		}
	}
	if localAppData != "" && modelName != "" {
		return filepath.Join(localAppData, "SpeechKit", "models", modelName)
	}
	if exeDir != "" && modelName != "" {
		return filepath.Join(exeDir, "models", modelName)
	}
	return ""
}

func buildGenkitConfig(cfg *config.Config) appai.Config {
	var aiCfg appai.Config

	if cfg.Providers.Google.Enabled {
		aiCfg.GoogleAPIKey = config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)
		aiCfg.GoogleUtilityModel = cfg.Providers.Google.UtilityModel
		aiCfg.GoogleAgentModel = cfg.Providers.Google.AgentModel
	}

	if cfg.Providers.OpenAI.Enabled {
		aiCfg.OpenAIAPIKey = config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv)
		aiCfg.OpenAIUtilityModel = cfg.Providers.OpenAI.UtilityModel
		aiCfg.OpenAIAgentModel = cfg.Providers.OpenAI.AgentModel
	}

	if cfg.Providers.Groq.Enabled {
		aiCfg.GroqAPIKey = config.ResolveSecret(cfg.Providers.Groq.APIKeyEnv)
		aiCfg.GroqUtilityModel = cfg.Providers.Groq.UtilityModel
		aiCfg.GroqAgentModel = cfg.Providers.Groq.AgentModel
	}

	if cfg.HuggingFace.Enabled {
		token, _, _ := config.ResolveHuggingFaceToken(cfg)
		aiCfg.HuggingFaceToken = token
		aiCfg.HFUtilityModel = "Qwen/Qwen3.5-9B"
		aiCfg.HFAgentModel = "Qwen/Qwen3.5-32B"
	}

	if cfg.Providers.Ollama.Enabled {
		aiCfg.OllamaBaseURL = cfg.Providers.Ollama.BaseURL
		aiCfg.OllamaUtilityModel = cfg.Providers.Ollama.UtilityModel
		aiCfg.OllamaAgentModel = cfg.Providers.Ollama.AgentModel
	}

	return aiCfg
}

func validateCloudProviders(ctx context.Context, r *router.Router) []string {
	var msgs []string

	if vps := r.VPS(); vps != nil {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := vps.Health(checkCtx)
		cancel()
		if err != nil {
			msgs = append(msgs, fmt.Sprintf("VPS unavailable: %v", err))
		} else {
			msgs = append(msgs, "VPS ready")
		}
	}

	if hf := r.HuggingFace(); hf != nil {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := hf.Health(checkCtx)
		cancel()
		if err != nil {
			msgs = append(msgs, fmt.Sprintf("HuggingFace unavailable: %v", err))
		} else {
			msgs = append(msgs, "HuggingFace ready")
		}
	}

	return msgs
}

// escapeJS returns s as a safe JavaScript string literal (without surrounding quotes).
// Uses json.Marshal to handle all special characters including backticks, null bytes,
// and unicode line/paragraph separators (U+2028, U+2029).
func escapeJS(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	// json.Marshal returns "quoted string" -- strip the surrounding quotes
	return string(b[1 : len(b)-1])
}

func missingProviderHint(cfg *config.Config) string {
	if cfg.Routing.Strategy == "cloud-only" && !cfg.HuggingFace.Enabled && !cfg.VPS.Enabled {
		return "Cloud-only routing is active, but no cloud provider is enabled. Enable Hugging Face Inference or configure VPS."
	}

	if cfg.HuggingFace.Enabled && cfg.VPS.Enabled {
		return ""
	}

	if cfg.HuggingFace.Enabled {
		token, _, err := config.ResolveHuggingFaceToken(cfg)
		if err == nil && token != "" {
			return ""
		}
		tokenEnv := config.HuggingFaceTokenEnvName(cfg)
		if tokenEnv == "" {
			tokenEnv = "HF_TOKEN"
		}
		return fmt.Sprintf("Hugging Face Inference is enabled, but no token could be resolved from settings, install bootstrap, %s, env or Doppler.", tokenEnv)
	}
	if cfg.VPS.Enabled && cfg.VPS.URL == "" {
		return "VPS provider is enabled, but no VPS URL is configured."
	}

	return ""
}

func runtimeConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(filepath.Dir(exe), "config.toml")
}

func buildTTSRouter(cfg *config.Config) *tts.Router {
	if !cfg.TTS.Enabled {
		return nil
	}

	var providers []tts.Provider

	if cfg.TTS.OpenAI.Enabled {
		apiKey := config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv)
		if apiKey != "" {
			model := cfg.TTS.OpenAI.Model
			if model == "" {
				model = cfg.Providers.OpenAI.TTSModel
			}
			voice := cfg.TTS.OpenAI.Voice
			if voice == "" {
				voice = cfg.Providers.OpenAI.TTSVoice
			}
			providers = append(providers, tts.NewOpenAI(tts.OpenAIOpts{
				APIKey: apiKey,
				Model:  model,
				Voice:  voice,
			}))
		}
	}

	if cfg.TTS.Google.Enabled {
		apiKey := config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)
		if apiKey != "" {
			providers = append(providers, tts.NewGoogle(tts.GoogleOpts{
				APIKey: apiKey,
				Voice:  cfg.TTS.Google.Voice,
			}))
		}
	}

	if cfg.TTS.HuggingFace.Enabled {
		token := config.ResolveSecret(cfg.HuggingFace.TokenEnv)
		if token != "" {
			model := cfg.TTS.HuggingFace.Model
			providers = append(providers, tts.NewHuggingFace(tts.HuggingFaceOpts{
				Token: token,
				Model: model,
			}))
		}
	}

	if len(providers) == 0 {
		return nil
	}

	return tts.NewRouter(tts.Strategy(cfg.TTS.Strategy), providers...)
}
