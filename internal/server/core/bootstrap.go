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
	"net"
	"net/http"
	"strings"
	"time"

	genkitcore "github.com/firebase/genkit/go/core"
	"github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	assistpkg "github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/server/assist"
	"github.com/kombifyio/SpeechKit/internal/server/dictation"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/server/persona"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/tts"
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
	Cfg            *config.Config
	Mux            *http.ServeMux
	Health         *HealthRegistry
	Modes          map[Mode]bool
	Version        string
	AuthState      *middleware.AuthState
	STTRouter      *router.Router
	AssistPipeline *assistpkg.Pipeline

	// Shared AI deps — populated by ensureSharedAIDeps on demand.
	GenkitRuntime *ai.Runtime
	AssistFlow    *genkitcore.Flow[flows.AssistInput, flows.AssistOutput, struct{}]
	AgentFlow     *genkitcore.Flow[flows.AgentInput, flows.AgentOutput, struct{}]
	TTSRouter     *tts.Router
	TTSEnabled    bool

	// PersonaRegistry holds the in-memory persona / role / sequence catalog.
	// Populated by ensurePersonaRegistry — loaded from TOML seeds at boot;
	// admin CRUD writes land here too (M5a). Durable persistence is
	// attached via a Persister when the store supports it (M5b).
	PersonaRegistry *persona.Registry

	// Store is the durable backend for transcriptions, quick notes, voice
	// agent session summaries, and — since M5b — the persona catalog.
	// Nil when the server is configured without a store.
	Store store.Store

	aiDepsOnce bool
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

	app := &App{
		Cfg:     cfg,
		Mux:     http.NewServeMux(),
		Health:  NewHealthRegistry(),
		Modes:   resolveModes(cfg.Server.Modes),
		Version: opts.Version,
	}

	registerHealth(app)
	registerTestUI(app)
	registerServerSettings(app)
	registerAPIAlias(app.Mux)

	// Build the STT router and register dictation/assist/voiceagent handlers
	// for whichever modes are enabled. The router is shared across all three
	// mode packages (dictation uses it directly; assist and voiceagent pull
	// the STT stage from it in M3/M4).
	if needsSTT(app.Modes) {
		sttRouter, providers, notes := buildSTTRouter(cfg)
		app.STTRouter = sttRouter
		for _, note := range notes {
			slog.Info("STT wiring", "msg", note)
		}
		registerProviderHealth(app, providers)
	}

	if app.ModeEnabled(ModeDictation) {
		if app.STTRouter == nil {
			app.Health.SetReady("mode.dictation", StatusUnavailable, "STT router not initialized")
		} else {
			h, err := dictation.New(dictation.Options{
				Router:        app.STTRouter,
				MaxUploadMB:   cfg.Server.MaxUploadMB,
				DefaultPrompt: dictationPromptFromDictionary(cfg.Vocabulary.Dictionary),
			})
			if err != nil {
				return fmt.Errorf("core.Run: build dictation handler: %w", err)
			}
			h.Mount(app.Mux)
			app.Health.SetReady("mode.dictation", StatusOK, "listening")
			slog.Info("mode enabled", "mode", "dictation", "path", "/v1/dictation/transcribe")
		}
	}

	if app.ModeEnabled(ModeAssist) {
		pipeline, notes, err := buildAssistPipeline(ctx, cfg, app)
		if err != nil {
			// buildAssistPipeline is designed not to return fatal errors; if
			// it ever does, we surface it rather than quietly marking the
			// mode degraded.
			return fmt.Errorf("core.Run: build assist pipeline: %w", err)
		}
		for _, note := range notes {
			slog.Info("assist wiring", "msg", note)
		}
		app.AssistPipeline = pipeline

		h, err := assist.New(assist.Options{
			Processor:     pipeline,
			Transcriber:   app.STTRouter,
			MaxUploadMB:   cfg.Server.MaxUploadMB,
			DefaultLocale: cfg.General.Language,
		})
		if err != nil {
			return fmt.Errorf("core.Run: build assist handler: %w", err)
		}
		h.Mount(app.Mux)
		app.Health.SetReady("mode.assist", StatusOK, "listening")
		slog.Info("mode enabled", "mode", "assist", "path", "/v1/assist/process")
	}

	if app.ModeEnabled(ModeVoiceAgent) {
		// Persona registry is scoped to the Voice Agent mode: the CRUD
		// endpoints (/v1/personas etc.) exist only when voiceagent is on.
		// Initialize the durable store first so persona writes persist
		// across restarts when a SQL backend is configured.
		ensureStore(cfg, app)
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
			"provider", firstVANonEmpty(cfg.VoiceAgent.Provider, "gemini"),
			"create", "/v1/voiceagent/sessions",
			"ws", "/v1/voiceagent/sessions/{id}/ws",
			"status", status)
	}

	if opts.HandlerHooks != nil {
		opts.HandlerHooks(app.Mux, app)
	}

	// Always-on server component. Mode handlers flip their own entries above.
	app.Health.SetReady("server", StatusOK, "listening")

	// Order matters: Recover wraps everything (panics from any middleware
	// or handler land in the JSON 500), Logging runs early so even auth
	// failures get an access-log line, CORS runs before Auth so preflight
	// OPTIONS bypasses the bearer check, Auth attaches Identity to the
	// context, and RateLimit reads that Identity to bucket per-user
	// rather than per-IP.
	app.AuthState = middleware.NewAuthState(cfg.Server.AuthMode, cfg.Server.BearerTokenEnv, cfg.Server.EdgeAuthSecretEnv)
	publicPaths := serverPublicPaths()
	publicRoutes := serverPublicRoutes()
	chain := middleware.Chain(
		middleware.Recover(),
		middleware.Logging(),
		middleware.CORS(cfg.Server.CORSAllowedOrigins),
		middleware.Auth(middleware.AuthOptions{
			ModeProvider:        app.AuthState.Mode,
			BearerTokenProvider: app.AuthState.BearerToken,
			EdgeSecretProvider:  app.AuthState.EdgeSecret,
			// Health endpoints are always public so external probes (Render,
			// Kubernetes) can hit them without credentials.
			AllowPublicPaths:     publicPaths,
			AllowPublicRoutes:    publicRoutes,
			AllowBootstrapRoutes: serverBootstrapAuthRoutes(),
			BootstrapAllowed: func(r *http.Request) bool {
				return serverSettingsBootstrapWriteAllowed(app)
			},
		}),
		middleware.RateLimit(middleware.RateLimitOptions{
			RequestsPerSecond: cfg.Server.RateLimitRPS,
			Burst:             cfg.Server.RateLimitBurst,
			// Health probes must never be rate-limited; otherwise a busy
			// neighbour could starve out Render's readiness checks during
			// real outages.
			AllowPublicPaths: publicPaths,
		}),
	)

	addr := strings.TrimSpace(cfg.Server.ListenAddr)
	if addr == "" {
		addr = ":8080"
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           chain(app.Mux),
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       0, // Large audio uploads; bounded per-route instead.
		WriteTimeout:      0, // WebSocket sessions can be long-lived.
		IdleTimeout:       120 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("core.Run: listen on %s: %w", addr, err)
	}

	slog.Info("HTTP server listening", "addr", ln.Addr().String())

	serveErr := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining connections")
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("core.Run: serve: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("HTTP shutdown did not complete cleanly", "err", err)
	}

	// Drain any lingering serve error.
	if err := <-serveErr; err != nil {
		return fmt.Errorf("core.Run: serve (post-shutdown): %w", err)
	}
	return nil
}

// resolveModes turns the toml "modes" list into a lookup map. Empty input means
// all modes are enabled, matching the documented default.
func resolveModes(configured []string) map[Mode]bool {
	result := map[Mode]bool{
		ModeDictation:  false,
		ModeAssist:     false,
		ModeVoiceAgent: false,
	}
	if len(configured) == 0 {
		for k := range result {
			result[k] = true
		}
		return result
	}
	for _, raw := range configured {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case string(ModeDictation):
			result[ModeDictation] = true
		case string(ModeAssist):
			result[ModeAssist] = true
		case string(ModeVoiceAgent):
			result[ModeVoiceAgent] = true
		}
	}
	return result
}

// ModeEnabled reports whether the given mode is active for this process.
func (a *App) ModeEnabled(m Mode) bool {
	if a == nil || a.Modes == nil {
		return false
	}
	return a.Modes[m]
}

// needsSTT reports whether any enabled mode depends on the STT router.
// Dictation always needs it; Assist and Cascaded-VoiceAgent use it for the
// STT stage of their pipelines; Gemini-VoiceAgent does its own server-side
// STT inside the realtime provider.
func needsSTT(modes map[Mode]bool) bool {
	return modes[ModeDictation] || modes[ModeAssist] || modes[ModeVoiceAgent]
}

func firstVANonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// ensureStore opens the configured durable store. Idempotent — the first
// successful call populates app.Store; subsequent calls are no-ops. If
// the backend is unconfigured or the driver fails, we log and continue:
// Voice Agent mode still works (personas stay in-memory), dictation still
// serves requests, and /readyz surfaces the missing capability.
func ensureStore(cfg *config.Config, app *App) {
	if app.Store != nil {
		return
	}
	backend := strings.TrimSpace(cfg.Store.Backend)
	if backend == "" {
		backend = "sqlite"
	}
	storeCfg := store.StoreConfig{
		Backend:            backend,
		SQLitePath:         cfg.Store.SQLitePath,
		PostgresDSN:        cfg.Store.PostgresDSN,
		SaveAudio:          cfg.Store.SaveAudio,
		AudioRetentionDays: cfg.Store.AudioRetentionDays,
		MaxAudioStorageMB:  cfg.Store.MaxAudioStorageMB,
	}
	s, err := store.New(storeCfg)
	if err != nil {
		slog.Warn("store init failed; durable features disabled", "backend", backend, "err", err)
		app.Health.SetReady("store", StatusDegraded, err.Error())
		return
	}
	app.Store = s
	app.Health.SetReady("store", StatusOK, backend)
	slog.Info("store initialized", "backend", backend)
}

// ensurePersonaRegistry lazily initializes the persona catalog.
//
// Boot order:
//  1. Build an empty registry.
//  2. If the Store exposes a *sql.DB (SQLite backend), hydrate previously
//     persisted entries FIRST so admin-authored overrides are in place,
//     then attach a Persister so subsequent admin writes survive restart.
//  3. Overlay TOML seeds on top — TOML acts as a baseline of defaults
//     that admin writes can replace per ID.
//
// Idempotent — second+ calls are no-ops.
func ensurePersonaRegistry(ctx context.Context, cfg *config.Config, app *App) {
	if app.PersonaRegistry != nil {
		return
	}
	reg := persona.NewRegistry()

	// (2) store-backed persistence, opt-in per concrete store type so
	// bootstrap stays compile-time explicit about which backends are
	// supported. Postgres lands when the pg persister file is added;
	// until then Postgres deployments run with in-memory persona state.
	if sqliteStore, ok := app.Store.(*store.SQLiteStore); ok {
		persister := persona.NewSQLitePersister(sqliteStore.DB())
		if err := reg.HydrateFrom(ctx, persister); err != nil {
			slog.Warn("persona: hydrate from store failed; falling back to TOML-only", "err", err)
		} else {
			slog.Info("persona: hydrated from SQLite store")
		}
		reg.WithPersister(persister)
	} else if app.Store != nil {
		slog.Info("persona: store backend does not support durable personas yet; admin writes are in-memory only")
	}

	// (3) overlay TOML seeds. Seeds are tagged Source="toml" and never
	// round-trip to the persister — they're the baseline, not data.
	notes := persona.LoadSeeds(reg, cfg)
	for _, note := range notes {
		slog.Debug("persona seed", "note", note)
	}
	app.PersonaRegistry = reg

	personaCount := len(reg.ListPersonas())
	roleCount := len(reg.ListRoles())
	sequenceCount := len(reg.ListSequences())
	detail := fmt.Sprintf("%d personas, %d roles, %d sequences", personaCount, roleCount, sequenceCount)
	switch {
	case personaCount == 0:
		app.Health.SetReady("persona.registry", StatusDegraded, "no personas seeded; clients must create one via POST /v1/personas")
	default:
		app.Health.SetReady("persona.registry", StatusOK, detail)
	}
	slog.Info("persona registry ready", "summary", detail)
}
