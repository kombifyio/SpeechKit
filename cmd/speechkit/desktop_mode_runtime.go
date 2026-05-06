package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/hotkey"
	"github.com/kombifyio/SpeechKit/internal/output"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/vad"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type desktopModeRuntimeOptions struct {
	Ctx             context.Context
	Config          *config.Config
	State           *appState
	STTRouter       *router.Router
	FeedbackStore   store.Store
	Capturer        audio.Session
	DictationVAD    vad.Detector
	Clipboard       *output.ClipboardHandler
	QuickActions    *quickActionCoordinator
	HotkeyEvents    <-chan hotkey.Event
	SilenceAutoStop <-chan struct{}
	InstallState    *config.InstallState
	ShowDashboard   func(string)
	Cleanup         *desktopCleanupStack
}

func startDesktopModeRuntime(opts desktopModeRuntimeOptions) (*desktopInputController, error) {
	transcriptionWorker, err := speechkit.NewTranscriptionWorker(speechkit.TranscriptionWorkerConfig{
		Timeout:   30 * time.Second,
		QueueSize: 4,
		Runner: speechkit.NewTranscriptionRunner(
			routerTranscriber{
				router:          newRuntimeDictationTranscriber(opts.STTRouter, opts.State),
				state:           opts.State,
				dictionaryStore: userDictionaryStoreFromFeedbackStore(opts.FeedbackStore),
			},
			speechkitStoreAdapter{store: opts.FeedbackStore},
		).WithObserver(speechkitCommitObserver{state: opts.State}),
		Output: desktopTranscriptOutput{
			cfg:         opts.Config,
			state:       opts.State,
			handler:     opts.Clipboard,
			interceptor: opts.QuickActions,
			playbackCtx: opts.Ctx,
			activeMode: func() string {
				opts.State.mu.Lock()
				defer opts.State.mu.Unlock()
				return opts.State.activeMode
			},
			onAssistText: func(text string) {
				trimmed := strings.TrimSpace(text)
				opts.State.addLog(
					fmt.Sprintf(
						"Assist response ready (%d chars, %d words)",
						utf8.RuneCountInString(trimmed),
						len(strings.Fields(trimmed)),
					),
					"info",
				)
				opts.State.showAssistBubble(text)
			},
		},
		Observer: opts.State,
	})
	if err != nil {
		return nil, fmt.Errorf("transcription worker init: %w", err)
	}
	transcriptionWorker.Start(opts.Ctx)
	if opts.Cleanup != nil {
		opts.Cleanup.Add(func() {
			transcriptionWorker.Close()
			transcriptionWorker.Wait()
		})
	}

	quickNoteService := desktopQuickNoteService{
		cfg:           opts.Config,
		state:         opts.State,
		feedbackStore: opts.FeedbackStore,
		host:          wailsQuickNoteHost{state: opts.State},
	}
	recordingController := speechkit.NewRecordingController(
		opts.Capturer,
		transcriptionWorker,
		opts.State,
		func() speechkit.SegmentCollector {
			return newDictationCaptureSession(opts.DictationVAD, opts.Config)
		},
	)

	var inputController *desktopInputController
	opts.State.engine = newSpeechKitRuntime(opts.State, speechkit.Hooks{
		HandleCommand: desktopCommandHandler{
			cfg:                 opts.Config,
			state:               opts.State,
			recordingController: recordingController,
			feedbackStore:       opts.FeedbackStore,
			showDashboard:       opts.ShowDashboard,
			quickNotes:          quickNoteService,
			actions:             opts.QuickActions,
			startVoiceAgent: func(ctx context.Context) error {
				if inputController == nil {
					return fmt.Errorf("voice agent runtime not configured")
				}
				inputController.activateVoiceAgent(ctx)
				return nil
			},
			stopVoiceAgent: func(ctx context.Context) error {
				if inputController == nil {
					return fmt.Errorf("voice agent runtime not configured")
				}
				inputController.deactivateVoiceAgentWithReason(ctx, true, "API control")
				return nil
			},
			voiceAgentFallback: func() bool {
				if inputController == nil {
					return true
				}
				return inputController.shouldUseVoiceAgentPipelineFallback()
			},
		}.Handle,
	})
	if opts.Cleanup != nil {
		opts.Cleanup.Add(func() {
			opts.State.engine.Close()
		})
	}
	opts.State.syncSpeechKitSnapshot()

	inputController = &desktopInputController{
		commands:          opts.State.engine.Commands(),
		recording:         recordingController,
		state:             opts.State,
		hotkeyEvents:      opts.HotkeyEvents,
		silenceAutoStop:   opts.SilenceAutoStop,
		autoStartInterval: 200 * time.Millisecond,
		voiceAgentSession: opts.State.voiceAgentSession,
		voiceAgentConfig:  &opts.Config.VoiceAgent,
		cfg:               opts.Config,
		installState:      opts.InstallState,
		sttRouter:         opts.STTRouter,
		audioCapturer:     opts.Capturer,
	}
	go inputController.Run(opts.Ctx)
	slog.Info("desktop mode runtime ready")

	return inputController, nil
}
