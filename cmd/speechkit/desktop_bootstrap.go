package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	appassets "github.com/kombifyio/SpeechKit/assets"
	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/output"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/vad"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type desktopAudioRuntime struct {
	Capturer        audio.Session
	DictationVAD    vad.Detector
	SilenceAutoStop <-chan struct{}
}

type desktopRuntimeServices struct {
	Clipboard    *output.ClipboardHandler
	QuickActions *quickActionCoordinator
}

type desktopWailsAppOptions struct {
	ConfigPath    string
	Config        *config.Config
	State         *appState
	STTRouter     *router.Router
	FeedbackStore store.Store
	InstallState  *config.InstallState
	Capturer      audio.Session
	Cancel        context.CancelFunc
	ShowDashboard func(string)
}

func initDesktopRouterRuntime(cfg *config.Config, state *appState) *router.Router {
	r, providerLog := buildRouter(cfg)
	syncRuntimeProviders(context.Background(), state, r) //nolint:contextcheck // ctx not yet created at startup initialization.
	for _, msg := range providerLog {
		slog.Info(msg)
	}
	configureDesktopServerDelegates(cfg, state)
	state.sttRouter = r
	return r
}

func configureDesktopServerDelegates(cfg *config.Config, state *appState) {
	serverDel, err := buildServerDelegates(cfg)
	if err != nil {
		slog.Warn("server delegates init failed; running modes locally", "err", err)
		return
	}
	if serverDel == nil {
		return
	}

	state.mu.Lock()
	state.serverDelegates = serverDel
	state.mu.Unlock()
	slog.Info("server delegates active",
		"modes", strings.Join(desktopServerDelegateModes(serverDel), ","),
		"fallback_to_local", serverDel.shouldFallback())
}

func desktopServerDelegateModes(serverDel *serverDelegates) []string {
	var modes []string
	if serverDel.hasDictation() {
		modes = append(modes, "dictation")
	}
	if serverDel.hasAssist() {
		modes = append(modes, "assist")
	}
	if serverDel.hasVoiceAgent() {
		modes = append(modes, "voice_agent")
	}
	return modes
}

func initDesktopAudioRuntime(cfg *config.Config, state *appState, cleanup *desktopCleanupStack) (desktopAudioRuntime, error) {
	capturer, err := newReconfigurableAudioSession(desktopAudioConfig(cfg), audio.Open)
	if err != nil {
		return desktopAudioRuntime{}, err
	}
	state.audioSession = capturer
	if cleanup != nil {
		cleanup.Add(func() {
			if err := capturer.Close(); err != nil {
				slog.Warn("audio close", "err", err)
			}
		})
	}

	watchDesktopAudioEvents(capturer, state)

	dictationVAD := openDesktopDictationVAD(state, cleanup)
	silenceAutoStop := make(chan struct{}, 1)
	capturer.SetLevelHandler(desktopAudioLevelHandler(cfg, state, silenceAutoStop))

	return desktopAudioRuntime{
		Capturer:        capturer,
		DictationVAD:    dictationVAD,
		SilenceAutoStop: silenceAutoStop,
	}, nil
}

func desktopAudioConfig(cfg *config.Config) audio.Config {
	return audio.Config{
		Backend:     audio.Backend(cfg.Audio.Backend),
		DeviceID:    cfg.Audio.DeviceID,
		SampleRate:  cfg.Audio.SampleRate,
		Channels:    cfg.Audio.Channels,
		FrameSizeMs: cfg.Audio.FrameSizeMs,
		LatencyHint: cfg.Audio.LatencyHint,
	}
}

func watchDesktopAudioEvents(capturer audio.Session, state *appState) {
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
				// EventStarted, EventStopped: no log action needed.
			}
		}
	}()
}

func openDesktopDictationVAD(state *appState, cleanup *desktopCleanupStack) vad.Detector {
	dictationVAD, closeDictationVAD, err := newDictationVAD()
	if err != nil {
		slog.Warn("dictation VAD unavailable", "err", err)
		return nil
	}
	if closeDictationVAD != nil && cleanup != nil {
		cleanup.Add(closeDictationVAD)
	}
	if dictationVAD != nil {
		state.addLog("Dictation VAD ready", "info")
	}
	return dictationVAD
}

func desktopAudioLevelHandler(cfg *config.Config, state *appState, silenceAutoStop chan<- struct{}) func(float64) {
	const silenceThreshold = 0.01 // RMS below this is treated as silence.
	var silenceSince time.Time
	var heardSpeech bool
	fastModeDuration := desktopFastModeDuration(cfg)

	return func(level float64) {
		state.setLevel(level)

		state.mu.Lock()
		voiceSession := state.voiceAgentSession
		state.mu.Unlock()
		if voiceSession != nil {
			state.updatePrompterActivity("user", voiceAgentUserActivityLevel(voiceSession.CurrentState(), level, state.currentVoiceAgentEchoGuard()))
		}

		if !state.quickCaptureModeActive() {
			silenceSince = time.Time{}
			heardSpeech = false
			return
		}

		if level < silenceThreshold {
			if !heardSpeech {
				silenceSince = time.Time{}
				return
			}
			if silenceSince.IsZero() {
				silenceSince = time.Now()
			} else if time.Since(silenceSince) >= fastModeDuration {
				select {
				case silenceAutoStop <- struct{}{}:
				default:
				}
				silenceSince = time.Time{}
				heardSpeech = false
			}
			return
		}
		heardSpeech = true
		silenceSince = time.Time{}
	}
}

func desktopFastModeDuration(cfg *config.Config) time.Duration {
	fastModeDuration := time.Duration(cfg.General.FastModeSilenceMs) * time.Millisecond
	if fastModeDuration <= 0 {
		return 1500 * time.Millisecond
	}
	return fastModeDuration
}

func initDesktopAppContext(state *appState, cleanup *desktopCleanupStack) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	if cleanup != nil {
		cleanup.Add(cancel)
		cleanup.Add(func() { stopConfiguredLocalLLMRuntime(state) })
	}
	return ctx, cancel
}

func initDesktopRuntimeServices(ctx context.Context, cfg *config.Config, state *appState, cleanup *desktopCleanupStack) desktopRuntimeServices {
	initDesktopAIRuntime(ctx, cfg, state)
	ttsRouter := initDesktopTTSRuntime(ctx, cfg, state, cleanup)
	clipHandler, quickActions, shortcutResolver := initDesktopQuickActions(cfg, state)
	initDesktopAssistRuntime(cfg, state, ttsRouter, shortcutResolver, quickActions)
	return desktopRuntimeServices{
		Clipboard:    clipHandler,
		QuickActions: quickActions,
	}
}

func prepareDesktopVoiceAgentSession(cfg *config.Config, state *appState) {
	if !cfg.VoiceAgent.Enabled {
		return
	}
	state.voiceAgentSession = prepareVoiceAgentSession(state, cfg)
	slog.Info("voice agent session prepared")
}

func newDesktopModeHotkeyManager(cfg *config.Config, state *appState) *modeHotkeyManager {
	hkManager := newModeHotkeyManager(configuredModeCombos(
		cfg.General.DictateEnabled,
		cfg.General.AssistEnabled,
		cfg.General.VoiceAgentEnabled,
		cfg.General.DictateHotkey,
		cfg.General.AssistHotkey,
		cfg.General.VoiceAgentHotkey,
	))
	state.hkManager = hkManager
	return hkManager
}

func newDesktopDashboardPresenter(state *appState) func(string) {
	return func(source string) {
		slog.Info("dashboard requested", "source", source)
		state.showDashboardWindow(source)
	}
}

func newDesktopWailsApp(opts desktopWailsAppOptions) *application.App {
	return application.New(application.Options{
		Name: "kombify SpeechKit",
		Icon: appassets.SpeechKitICO(),
		Windows: application.WindowsOptions{
			EnabledFeatures: []string{"msWebView2EnableDraggableRegions"},
		},
		Assets: application.AssetOptions{
			Handler: assetHandler(opts.Config, opts.ConfigPath, opts.State, opts.STTRouter, opts.FeedbackStore, opts.InstallState),
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
			opts.State.beginShutdown()
			return true
		},
		OnShutdown: func() {
			opts.State.beginShutdown()
			if opts.Cancel != nil {
				opts.Cancel()
			}
			opts.State.stopVoiceAgentAudioSender()
			opts.State.stopVoiceAgentStream()
			if opts.Capturer != nil {
				opts.Capturer.SetPCMHandler(nil)
			}
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.kombify.speechkit",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				handleSecondDesktopInstance(opts.State, opts.ShowDashboard, data)
			},
		},
	})
}

func handleSecondDesktopInstance(state *appState, showDashboard func(string), data application.SecondInstanceData) {
	slog.Info("second instance launch blocked", "cwd", data.WorkingDir, "args", data.Args)
	if state.engine != nil {
		_ = state.engine.Commands().Dispatch(context.Background(), speechkit.Command{
			Type: speechkit.CommandShowDashboard,
			Metadata: map[string]string{
				"source": "second-instance",
			},
		})
		return
	}
	showDashboard("second-instance")
}

func startDesktopProviderReadiness(ctx context.Context, state *appState, r *router.Router) {
	for _, msg := range validateCloudProviders(ctx, r) {
		state.addLog(msg, "info")
	}
	syncRuntimeProviders(ctx, state, r)

	if localProvider, ok := r.Local().(localProviderStarter); ok {
		startLocalProviderAsync(ctx, state, r, localProvider)
	}
}

func startDesktopHotkeys(ctx context.Context, hkManager *modeHotkeyManager, cleanup *desktopCleanupStack) error {
	if err := hkManager.Start(ctx); err != nil {
		return err
	}
	if cleanup != nil {
		cleanup.Add(hkManager.Stop)
	}
	return nil
}

func logDesktopStartupReady(cfg *config.Config, state *appState, r *router.Router) {
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
			return
		}
		if hint := missingProviderHint(cfg); hint != "" {
			state.addLog(hint, "error")
		}
		state.addLog("No STT provider ready", "error")
		return
	}
	state.addLog("Ready", "success")
}

func startDesktopOverlaySyncLoop(ctx context.Context, state *appState) {
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
}
