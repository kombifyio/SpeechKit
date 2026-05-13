package main

import (
	"context"
	"log/slog"

	"github.com/kombifyio/SpeechKit/cmd/speechkit/internal/transcription"
	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/output"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/textactions"
	"github.com/kombifyio/SpeechKit/internal/tts"
)

func initDesktopAIRuntime(ctx context.Context, cfg *config.Config, state *appState) {
	syncConfiguredLocalLLMRuntime(ctx, cfg, state)
	genkitRT, err := appai.Init(ctx, buildGenkitConfig(cfg))
	if err != nil {
		slog.Warn("genkit init", "err", err)
		state.addLog("AI providers unavailable — Assist and Voice Agent disabled", "warn")
		return
	}

	state.genkitRT = genkitRT
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

func initDesktopTTSRuntime(ctx context.Context, cfg *config.Config, state *appState, cleanup *desktopCleanupStack) *tts.Router {
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

	audioPlayer, err := audio.NewPlayer()
	if err != nil {
		slog.Warn("audio player init", "err", err)
		state.addLog("TTS audio player unavailable — voice output disabled", "warn")
		return ttsRouter
	}
	state.audioPlayer = audioPlayer
	if cleanup != nil {
		cleanup.Add(audioPlayer.Close)
	}
	slog.Info("audio player ready (24kHz mono)")

	return ttsRouter
}

func initDesktopQuickActions(cfg *config.Config, state *appState) (*output.ClipboardHandler, *quickActionCoordinator, *shortcuts.Resolver) {
	clipHandler := output.NewClipboardHandler()
	shortcutResolver := buildShortcutResolver(cfg)
	quickActions := newQuickActionCoordinator(state, clipHandler, shortcutResolver)
	quickActions.summarizer.Summarizer = textactions.SummarizerFunc(func(ctx context.Context, input textactions.Input) (string, error) {
		state.mu.Lock()
		flow := state.summarizeFlow
		state.mu.Unlock()
		return (&textactions.FlowSummarizer{Flow: flow}).Summarize(ctx, input)
	})

	return clipHandler, quickActions, shortcutResolver
}

func initDesktopAssistRuntime(cfg *config.Config, state *appState, ttsRouter *tts.Router, shortcutResolver *shortcuts.Resolver, quickActions *quickActionCoordinator) {
	state.assistExecutor = newAssistToolExecutor(quickActions)
	if state.assistFlow == nil && state.assistExecutor == nil {
		return
	}

	state.assistPipeline = assist.NewPipeline(
		state.assistFlow,
		state.assistExecutor,
		ttsRouter,
		cfg.TTS.Enabled,
		assist.WithRouter(assist.NewRouter(assist.WithResolver(shortcutResolver))),
	)
	slog.Info("assist pipeline ready")
}

func openDesktopFeedbackStore(cfg *config.Config, state *appState, cleanup *desktopCleanupStack) store.Store {
	feedbackStore, err := store.New(store.StoreConfig{
		Backend:                 cfg.Store.Backend,
		SQLitePath:              cfg.Store.SQLitePath,
		SaveAudio:               cfg.Store.SaveAudio,
		AudioRetentionDays:      cfg.Store.AudioRetentionDays,
		MaxAudioStorageMB:       cfg.Store.MaxAudioStorageMB,
		PostgresDSN:             cfg.Store.PostgresDSN,
		TranscriptionModelHints: transcription.ConfiguredModelHints(cfg),
	})
	if err != nil {
		slog.Warn("store init", "err", err)
		return nil
	}

	if voiceStore, ok := feedbackStore.(store.VoiceAgentSessionStore); ok {
		state.voiceAgentStore = voiceStore
	}
	if cleanup != nil {
		cleanup.Add(func() { _ = feedbackStore.Close() })
	}
	count, _ := feedbackStore.TranscriptionCount(context.Background())
	state.transcriptions = count
	state.syncSpeechKitSnapshot()
	slog.Info("store ready", "records", count, "backend", cfg.Store.Backend)

	return feedbackStore
}
