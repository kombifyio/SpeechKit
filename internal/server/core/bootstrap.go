//go:build linux

// Package core is the SpeechKit server bootstrap layer. It owns process-level
// state (config, routers, registries, lifecycle) and wires the HTTP mux that
// each mode package hangs its handlers off.
//
// M1 scope: HTTP listener, /healthz, /readyz, signal handling, middleware
// chain. STT/TTS/Voice-Agent wiring comes in M2–M4 as those modes come online.
package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/assist"
	"github.com/kombifyio/SpeechKit/internal/server/catalog"
	"github.com/kombifyio/SpeechKit/internal/server/configapi"
	"github.com/kombifyio/SpeechKit/internal/server/customization"
	deviceagentserver "github.com/kombifyio/SpeechKit/internal/server/deviceagent"
	"github.com/kombifyio/SpeechKit/internal/server/dictation"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/server/persona"
	"github.com/kombifyio/SpeechKit/internal/server/transcripts"
	"github.com/kombifyio/SpeechKit/internal/server/ttsapi"
	"github.com/kombifyio/SpeechKit/internal/server/vocabulary"
	"github.com/kombifyio/SpeechKit/internal/store"
	assistpkg "github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/lifecycle"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

// Mode identifies a server mode toggle.
type Mode string

const (
	ModeDictation  Mode = "dictation"
	ModeAssist     Mode = "assist"
	ModeVoiceAgent Mode = "voiceagent"
)

// RunOptions exposes bootstrap-time knobs that don't belong in config.toml.
type RunOptions struct {
	Version string
	// HandlerHooks lets tests inject extra routes onto the mux before the
	// server starts listening. Production leaves this nil.
	HandlerHooks func(mux *http.ServeMux, app *App)
}

// App is the process-wide dependency bundle. Mode packages receive it and
// register their handlers against app.Mux.
//
// Shared AI dependencies (Genkit runtime, TTS router, Agent flow) are built
// lazily via ensureSharedAIDeps so Assist and Cascaded-VoiceAgent share one
// Genkit instance instead of paying init cost twice.
type App struct {
	Cfg           *config.Config
	Mux           *http.ServeMux
	Health        *HealthRegistry
	Modes         map[Mode]bool
	Lifecycle     *lifecycle.Registry
	SharedDeps    *lifecycle.SharedDepRegistry
	Version       string
	AuthState     *middleware.AuthState
	STTRouter     *stt.Router
	AssistService *assistpkg.Service

	// Shared AI deps — populated by ensureSharedAIDeps on demand.
	GenkitRuntime *ai.Runtime
	AssistFlow    *flows.Flow[flows.AssistInput, flows.AssistOutput]
	AgentFlow     *flows.Flow[flows.AgentInput, flows.AgentOutput]
	TTSRouter     *tts.Router
	TTSEnabled    bool

	// DeviceAgentBridgeMounted is set only after every bridge dependency has
	// been constructed and the four credential-bearing local handlers have
	// been mounted. The global auth carve-out is conditional on this runtime
	// fact, never on configuration intent alone.
	DeviceAgentBridgeMounted bool
	DeviceAgentBridge        *deviceagentserver.Bridge

	// BoxMediaRuntime owns the separate TLS 1.3 listener and any concrete local
	// STT subprocess that Box wiring started. It never mounts on Mux or extends
	// the four G0 routes.
	BoxMediaRuntime *boxMediaServerRuntime

	// PersonaRegistry holds the in-memory persona / role / sequence catalog.
	// Populated by ensurePersonaRegistry — loaded from TOML seeds at boot;
	// admin CRUD writes land here too (M5a). Durable persistence is
	// attached via a Persister when the store supports it (M5b).
	PersonaRegistry *persona.Registry

	// Store is the durable backend for transcriptions, quick notes, voice
	// agent session summaries, and — since M5b — the persona catalog.
	// Nil when the server is configured without a store.
	Store store.Store

	// bootstrapSealed latches once a process has observed a final
	// post-onboarding state (settings file marks complete with matching
	// version, or a bearer token is already set). After the latch flips,
	// serverSettingsBootstrapWriteAllowed returns false for the rest of
	// the process lifetime even if the on-disk settings file is mutated
	// or deleted out-of-band. The bootstrap window stays closed until a
	// fresh process starts on a host whose disk truly reflects an
	// unbootstrapped state.
	bootstrapSealed atomic.Bool

	aiDepsOnce bool

	// telemetryShutdown flushes and stops the OpenTelemetry trace pipeline
	// installed by initServerTelemetry. Nil when trace export is disabled.
	telemetryShutdown func(context.Context) error
}

func dictationPromptFromDictionary(dictionary string) string {
	dictionary = strings.ReplaceAll(dictionary, "\r\n", "\n")
	dictionary = strings.ReplaceAll(dictionary, "\r", "\n")
	terms := make([]string, 0)
	for _, line := range strings.Split(dictionary, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if before, after, ok := strings.Cut(line, "=>"); ok {
			line = strings.TrimSpace(after)
			if line == "" {
				line = strings.TrimSpace(before)
			}
		}
		if line != "" {
			terms = append(terms, line)
		}
	}
	if len(terms) == 0 {
		return ""
	}
	return "Prefer these dictionary terms in transcription: " + strings.Join(terms, ", ") + "."
}

// Run boots the server, blocks until ctx is cancelled or the listener fails,
// and performs graceful shutdown. The caller is responsible for cancelling ctx
// on SIGTERM/SIGINT (see NotifySignals).
func Run(ctx context.Context, cfg *config.Config, opts RunOptions) error {
	if cfg == nil {
		return errors.New("core.Run: nil config")
	}

	app := newServerApp(cfg, opts)
	registerCoreEndpoints(app)
	initServerTelemetry(ctx, app) //nolint:contextcheck // the trace provider is process-scoped startup state, not request-scoped work.

	// Build the STT router and register dictation/assist/voiceagent handlers
	// for whichever modes are enabled. The router is shared across all three
	// mode packages (dictation uses it directly; assist and voiceagent pull
	// the STT stage from it in M3/M4).
	if needsSTT(app.Modes) || wyomingNeedsSTT(cfg) || cfg.Server.DeviceAgent.BoxMedia.Enabled {
		sttRouter, providers, notes := buildSTTRouter(cfg)
		app.STTRouter = sttRouter
		for _, note := range notes {
			slog.Info("STT wiring", "msg", note)
		}
		registerProviderHealth(app, providers, app.ModeEnabled(ModeDictation)) //nolint:contextcheck // health probes own their short-lived contexts and are not request-scoped.
	}
	initServerLifecycle(app) //nolint:contextcheck // lifecycle hooks are process-scoped startup state, not request-scoped work.

	if cfg.Server.Features.StorageReads || cfg.Server.Features.Vocabulary {
		ensureStore(cfg, app) //nolint:contextcheck // store.Open has no context-aware API; startup migration is bounded by CI/runtime gates.
	}
	if (cfg.Server.Features.TTSDirect || wyomingNeedsTTS(cfg) || cfg.Server.DeviceAgent.Enabled) && app.TTSRouter == nil {
		ttsRouter, ttsEnabled, ttsNotes := buildTTSRouter(cfg)
		for _, note := range ttsNotes {
			slog.Info("TTS wiring", "msg", note)
		}
		app.TTSRouter = ttsRouter
		app.TTSEnabled = ttsEnabled
		switch {
		case ttsEnabled:
			app.Health.SetReady("tts", StatusOK, "enabled")
		case !cfg.TTS.Enabled:
			app.Health.SetReady("tts", StatusOK, "disabled")
		default:
			app.Health.SetReady("tts", StatusDegraded, "enabled but no providers configured")
		}
	}

	deviceAgentClaims, err := wireDeviceAgentBridge(ctx, cfg, app)
	if err != nil {
		return fmt.Errorf("core.Run: wire local device-agent bridge: %w", err)
	}
	if deviceAgentClaims != nil {
		defer func() {
			if err := deviceAgentClaims.Close(); err != nil {
				slog.Warn("close device-agent claim ledger", "err", err)
			}
		}()
	}
	if app.ModeEnabled(ModeDictation) {
		if app.STTRouter == nil {
			app.Health.SetReady("mode.dictation", StatusUnavailable, "STT router not initialized")
		} else {
			h, err := dictation.New(dictation.Options{
				Router:                 app.STTRouter,
				MaxUploadMB:            cfg.Server.MaxUploadMB,
				MaxDecodedAudioSeconds: cfg.Server.MaxDecodedAudioSeconds,
				DefaultPrompt:          dictationPromptFromDictionary(cfg.Vocabulary.Dictionary),
				Store:                  app.Store,
				ActiveTemplateIDs:      cfg.Customization.ActiveTemplateIDs,
				// Lowest-precedence provider preference (voice-prefs
				// contract): explicit request override → edge-injected user
				// preference → this ModelSelection primary → router order.
				DefaultProviderProfileID: cfg.ModelSelection.Dictate.PrimaryProfileID,
			})
			if err != nil {
				return fmt.Errorf("core.Run: build dictation handler: %w", err)
			}
			h.Mount(app.Mux)
			app.Health.SetReady("mode.dictation", StatusOK, "listening")
			slog.Info("mode enabled", "mode", "dictation", "path", "/v1/dictation/transcribe")
			// Streaming dictation rides on the same mode + STT router:
			// session create + ticket-authenticated WS with live partials.
			wireDictationStream(cfg, app)
		}
	} else {
		mountModeDisabled(app.Mux, ModeDictation, "/v1/dictation/transcribe")
		app.Health.SetReady("mode.dictation", StatusDisabled, "configured off")
	}

	if cfg.Server.Features.Catalog {
		catalog.New(cfg, func(component string) string {
			_, components, _ := app.Health.Snapshot()
			entry, ok := components[component]
			if !ok {
				return ""
			}
			return string(entry.Status)
		}, app.Version).Mount(app.Mux)
		app.Health.SetReady("api.catalog", StatusOK, "listening")
	}
	configapi.New(cfg, app.Version, func() string {
		overall, _, _ := app.Health.Snapshot()
		return string(overall)
	}).Mount(app.Mux)
	if cfg.Server.Features.Vocabulary {
		dictStore, _ := app.Store.(store.UserDictionaryStore)
		vocabulary.New(dictStore).Mount(app.Mux)
		customizationStore, _ := app.Store.(store.CustomizationStore)
		customization.New(customizationStore, cfg.Customization.ActiveTemplateIDs).Mount(app.Mux)
		switch {
		case app.Store == nil:
			app.Health.SetReady("api.vocabulary", StatusUnavailable, "store unavailable")
		case dictStore == nil:
			app.Health.SetReady("api.vocabulary", StatusUnavailable, "store does not support user dictionary")
		case customizationStore == nil:
			app.Health.SetReady("api.vocabulary", StatusDegraded, "dictionary listening; customization store unavailable")
		default:
			app.Health.SetReady("api.vocabulary", StatusOK, "listening")
		}
	}
	if cfg.Server.Features.StorageReads {
		transcriptHandler := transcripts.New(app.Store)
		transcriptHandler.Mount(app.Mux)
		if !app.ModeEnabled(ModeVoiceAgent) {
			transcriptHandler.MountVoiceAgentReads(app.Mux)
		}
		if app.Store == nil {
			app.Health.SetReady("api.storage_reads", StatusUnavailable, "store unavailable")
		} else {
			app.Health.SetReady("api.storage_reads", StatusOK, "listening")
		}
	}
	if cfg.Server.Features.TTSDirect {
		ttsapi.New(cfg, app.TTSRouter).Mount(app.Mux)
		switch {
		case app.TTSRouter != nil && app.TTSEnabled:
			app.Health.SetReady("api.tts_direct", StatusOK, "listening")
		case !cfg.TTS.Enabled:
			// TTS deliberately off (self-hosted defaults disable it when
			// no cloud TTS key is present). Mark non-blocking so /readyz
			// stays green for the other modes that don't depend on TTS.
			app.Health.SetReadyWithOptions("api.tts_direct", StatusUnavailable, "tts disabled", ComponentOptions{
				Blocking: false,
				Kind:     "feature",
			})
		default:
			app.Health.SetReady("api.tts_direct", StatusUnavailable, "tts router unavailable")
		}
	}

	// Wake-word training-data uploads (v0.37.5). Always wired so the
	// device-side uploader can probe the endpoint; AcceptUploads=false
	// (the default) makes every request return 503 with a structured
	// "training_data_disabled" payload. See
	// docs/wakeword-training-data.md for the full privacy contract.
	wireWakewordTraining(cfg, app)

	// Wake-word model catalog (openWakeWord ONNX + microWakeWord manifests).
	// Public + default-on: serves already-public model metadata so a
	// SpeechKit-trained phrase can be individualized on ESPHome satellites and
	// the Kombify-Box on-device (microWakeWord), while host consumers read the
	// openWakeWord triplet.
	wireWakewordModels(cfg, app)

	if app.ModeEnabled(ModeAssist) {
		service, notes, err := buildAssistService(ctx, cfg, app)
		for _, note := range notes {
			slog.Info("assist wiring", "msg", note)
		}
		if err != nil {
			return fmt.Errorf("core.Run: build assist service: %w", err)
		}
		app.AssistService = service

		h, err := assist.New(assist.Options{
			Processor:              service,
			Transcriber:            app.STTRouter,
			MaxUploadMB:            cfg.Server.MaxUploadMB,
			MaxDecodedAudioSeconds: cfg.Server.MaxDecodedAudioSeconds,
			DefaultLocale:          cfg.General.Language,
			Store:                  app.Store,
			ActiveTemplateIDs:      cfg.Customization.ActiveTemplateIDs,
		})
		if err != nil {
			return fmt.Errorf("core.Run: build assist handler: %w", err)
		}
		h.Mount(app.Mux)
		app.Health.SetReady("mode.assist", StatusOK, "listening")
		slog.Info("mode enabled", "mode", "assist", "path", "/v1/assist/process")
	} else {
		mountModeDisabled(app.Mux, ModeAssist, "/v1/assist/process", "/v1/assist/self-test")
		app.Health.SetReady("mode.assist", StatusDisabled, "configured off")
	}

	if app.ModeEnabled(ModeVoiceAgent) {
		// Persona registry is scoped to the Voice Agent mode: the CRUD
		// endpoints (/v1/personas etc.) exist only when voiceagent is on.
		// Initialize the durable store first so persona writes persist
		// across restarts when a SQL backend is configured.
		ensureStore(cfg, app) //nolint:contextcheck // store.Open has no context-aware API; startup migration is bounded by CI/runtime gates.
		ensurePersonaRegistry(ctx, cfg, app)

		personaHandler, err := persona.New(persona.HandlerOptions{
			Registry:    app.PersonaRegistry,
			AllowWrites: true,
		})
		if err != nil {
			return fmt.Errorf("core.Run: build persona handler: %w", err)
		}
		personaHandler.Mount(app.Mux)
		slog.Info("mounted persona CRUD endpoints",
			"paths", "/v1/personas, /v1/roles, /v1/sequences")

		h, status, err := buildVoiceAgentHandler(ctx, cfg, app)
		if err != nil {
			return fmt.Errorf("core.Run: build voiceagent handler: %w", err)
		}
		h.Mount(app.Mux)
		switch {
		case strings.HasPrefix(status, "degraded"), strings.HasPrefix(status, "unavailable"):
			app.Health.SetReady("mode.voiceagent", StatusDegraded, status)
		case strings.HasPrefix(status, "partial"):
			app.Health.SetReady("mode.voiceagent", StatusOK, "listening: "+status)
		default:
			app.Health.SetReady("mode.voiceagent", StatusOK, "listening: "+status)
		}
		slog.Info("mode enabled", "mode", "voiceagent",
			"provider", firstVANonEmpty(cfg.VoiceAgent.Provider, "assemblyai"),
			"create", "/v1/voiceagent/sessions",
			"ws", "/v1/voiceagent/sessions/{id}/ws",
			"status", status)
	} else {
		mountModeDisabled(app.Mux, ModeVoiceAgent, "/v1/voiceagent/sessions")
		if !cfg.Server.Features.StorageReads {
			mountModeDisabled(app.Mux, ModeVoiceAgent, "/v1/voiceagent/sessions/")
		}
		app.Health.SetReady("mode.voiceagent", StatusDisabled, "configured off")
	}

	if opts.HandlerHooks != nil {
		opts.HandlerHooks(app.Mux, app)
	}

	// Start the separate Box listener only after every normal server handler
	// has been built successfully. This avoids exposing the HA-capable local
	// path during a startup that will later fail for an unrelated mode.
	if _, err := wireBoxMediaListener(ctx, cfg, app); err != nil {
		return fmt.Errorf("core.Run: wire local Box media listener: %w", err)
	}

	// Always-on server component. Mode handlers flip their own entries above.
	app.Health.SetReady("server", StatusOK, "listening")

	// Wyoming voice backend (ESPHome / Home Assistant). Non-blocking: launches
	// its own TCP listener in a goroutine, torn down on ctx cancellation. No-op
	// unless [server.wyoming].enabled.
	startWyoming(ctx, cfg, app)

	return serveServer(ctx, cfg, app)
}
