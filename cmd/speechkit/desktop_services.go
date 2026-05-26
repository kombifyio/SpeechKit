package main

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/firebase/genkit/go/core"

	"github.com/kombifyio/SpeechKit/cmd/speechkit/internal/transcription"
	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/assist/skills/voice_companion"
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

	summarizeFlow := flows.DefineSummarizeFlow(genkitRT.G, genkitRT.UtilityModels())
	agentFlow := flows.DefineAgentFlow(genkitRT.G, genkitRT.AgentModels())
	var assistFlow *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}]
	if len(genkitRT.AssistModels()) > 0 {
		assistFlow = flows.DefineAssistFlow(genkitRT.G, genkitRT.AssistModels())
	}
	state.mu.Lock()
	state.genkitRT = genkitRT
	state.summarizeFlow = summarizeFlow
	state.agentFlow = agentFlow
	state.assistFlow = assistFlow
	state.mu.Unlock()

	slog.Info("genkit initialized",
		"utility_models", len(genkitRT.UtilityModels()),
		"assist_models", len(genkitRT.AssistModels()),
		"agent_models", len(genkitRT.AgentModels()),
	)
}

func initDesktopTTSRuntime(ctx context.Context, cfg *config.Config, state *appState, cleanup *desktopCleanupStack) *tts.Router {
	ttsRouter := buildTTSRouter(cfg)
	state.mu.Lock()
	state.ttsRouter = ttsRouter
	state.mu.Unlock()
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
	if ttsRouter == nil {
		return nil
	}

	audioPlayer, err := audio.NewPlayer()
	if err != nil {
		slog.Warn("audio player init", "err", err)
		state.addLog("TTS audio player unavailable — voice output disabled", "warn")
		return ttsRouter
	}
	state.mu.Lock()
	state.audioPlayer = audioPlayer
	state.mu.Unlock()
	if cleanup != nil {
		cleanup.Add(audioPlayer.Close)
	}
	slog.Info("audio player ready (24kHz mono)")

	return ttsRouter
}

func teardownDesktopTTSRuntime(state *appState) {
	if state == nil {
		return
	}
	state.mu.Lock()
	ttsRouter := state.ttsRouter
	audioPlayer := state.audioPlayer
	state.ttsRouter = nil
	state.audioPlayer = nil
	state.mu.Unlock()

	if ttsRouter != nil {
		ttsRouter.CloseIdleConnections()
	}
	if audioPlayer != nil {
		audioPlayer.Close()
	}
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
	hostExecutor := newAssistToolExecutor(quickActions)
	state.mu.Lock()
	state.assistExecutor = hostExecutor
	assistFlow := state.assistFlow
	if state.assistSkillContextStore == nil {
		state.assistSkillContextStore = assist.NewInMemorySkillContextStore(60*time.Second, nil)
	}
	skillContexts := state.assistSkillContextStore
	state.mu.Unlock()

	if assistFlow == nil && hostExecutor == nil {
		return
	}

	// Voice-Companion skill catalog runs alongside the legacy host-side
	// executor (copy_last / insert_last / summarize / quick_note). The
	// CompositeExecutor dispatches first to a matching pure-Go skill
	// (Time / Date / Math / Weather / Timer / Reminder / Wikipedia /
	// HomeAssistant), and falls back to the legacy executor for the
	// existing text-utility intents.
	haURL := strings.TrimSpace(cfg.Assist.HomeAssistant.URL)
	haToken := strings.TrimSpace(config.ResolveSecret(cfg.Assist.HomeAssistant.TokenEnv))
	haConfigured := haURL != "" && haToken != ""
	skills := voice_companion.AllSkills(haURL, haToken)
	executor := voice_companion.NewCompositeExecutor(skills, hostExecutor)

	routerOpts := []assist.RouterOption{assist.WithResolver(shortcutResolver)}
	if haConfigured {
		// Flip the HomeAssistant UtilityDefinition Enabled=true so the
		// router actually routes HA-prefix matches to the executor.
		// Without this override the default registry leaves the entry
		// disabled and the resolver bypasses HA entirely.
		registry := assist.DefaultUtilityRegistry()
		registry.Register(assist.UtilityDefinition{
			ID:             assist.UtilityHomeAssistant,
			Intent:         shortcuts.IntentHomeAssistant,
			Label:          "Send command to Home Assistant",
			Input:          assist.UtilityInputUtterance,
			DefaultSurface: assist.ResultSurfaceActionAck,
			DefaultKind:    assist.ResultKindUtilityAction,
			Enabled:        true,
		})
		routerOpts = append(routerOpts, assist.WithUtilityRegistry(registry))
	}

	// v0.38 multi-turn: a per-device SkillContextStore lets Voice-Companion
	// skills ask follow-up questions ("Timer für wie lange?"). One store
	// survives pipeline re-builds on model-profile switches so an in-flight
	// follow-up does not reset when the user changes the LLM.
	pipeline := assist.NewPipeline(
		assistFlow,
		executor,
		ttsRouter,
		cfg.TTS.Enabled,
		assist.WithRouter(assist.NewRouter(routerOpts...)),
		assist.WithSkillContextStore(skillContexts),
	)
	state.mu.Lock()
	state.assistPipeline = pipeline
	state.mu.Unlock()
	slog.Info("assist pipeline ready (voice-companion skills wired)",
		"home_assistant", haConfigured,
		"multi_turn", skillContexts != nil)
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
