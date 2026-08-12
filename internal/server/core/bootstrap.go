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
	"sync/atomic"
	"time"

	genkitcore "github.com/firebase/genkit/go/core"
	"github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	assistpkg "github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/server/assist"
	"github.com/kombifyio/SpeechKit/internal/server/catalog"
	"github.com/kombifyio/SpeechKit/internal/server/configapi"
	"github.com/kombifyio/SpeechKit/internal/server/customization"
	deviceagentserver "github.com/kombifyio/SpeechKit/internal/server/deviceagent"
	"github.com/kombifyio/SpeechKit/internal/server/dictation"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/server/persona"
	"github.com/kombifyio/SpeechKit/internal/server/transcripts"
	"github.com/kombifyio/SpeechKit/internal/server/ttsapi"
	"github.com/kombifyio/SpeechKit/internal/server/vocabulary"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/tts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/lifecycle"
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
	Lifecycle      *lifecycle.Registry
	SharedDeps     *lifecycle.SharedDepRegistry
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
		pipeline, notes := buildAssistPipeline(ctx, cfg, app)
		for _, note := range notes {
			slog.Info("assist wiring", "msg", note)
		}
		app.AssistPipeline = pipeline

		h, err := assist.New(assist.Options{
			Processor:              pipeline,
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
			"provider", firstVANonEmpty(cfg.VoiceAgent.Provider, "gemini"),
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

func newServerApp(cfg *config.Config, opts RunOptions) *App {
	return &App{
		Cfg:     cfg,
		Mux:     http.NewServeMux(),
		Health:  NewHealthRegistry(),
		Modes:   resolveModes(cfg.Server.Modes),
		Version: opts.Version,
	}
}

func registerCoreEndpoints(app *App) {
	registerHealth(app)
	registerTestUI(app)
	registerServerSettings(app)
	registerDeploymentStatus(app)
	registerPprof(app)
	registerAPIAlias(app.Mux)
}

// serverOIDCVerifier builds the OIDC JWT verifier when auth_mode is "oidc"
// or "bearer_or_oidc". Returns (nil, nil) for every other mode so the auth
// chain is unchanged.
func serverOIDCVerifier(cfg *config.Config) (func(*http.Request) (middleware.Identity, bool), error) {
	authMode := strings.TrimSpace(cfg.Server.AuthMode)
	if !strings.EqualFold(authMode, string(middleware.AuthModeOIDC)) &&
		!strings.EqualFold(authMode, string(middleware.AuthModeBearerOrOIDC)) {
		return nil, nil
	}
	validator, err := middleware.NewOIDCValidator(middleware.OIDCConfig{
		JWKSURL:          cfg.Server.OIDC.JWKSURL,
		Issuer:           cfg.Server.OIDC.Issuer,
		Audience:         cfg.Server.OIDC.Audience,
		ClockSkewSeconds: cfg.Server.OIDC.ClockSkewSeconds,
		OrgClaim:         cfg.Server.OIDC.OrgClaim,
		RoleClaim:        cfg.Server.OIDC.RoleClaim,
	})
	if err != nil {
		return nil, err
	}
	return validator.Verify, nil
}

// serverSecurityHeaders builds the security-header middleware from config.
// Returns nil (a no-op slot in the chain) only when explicitly disabled.
func serverSecurityHeaders(cfg *config.Config) middleware.Middleware {
	if cfg.Server.Security.Disabled {
		return nil
	}
	return middleware.SecurityHeaders(middleware.SecurityHeadersOptions{
		ContentSecurityPolicy: cfg.Server.Security.ContentSecurityPolicy,
		FrameOptions:          cfg.Server.Security.FrameOptions,
		ReferrerPolicy:        cfg.Server.Security.ReferrerPolicy,
		HSTS:                  cfg.Server.Security.HSTS,
		HSTSMaxAgeSeconds:     cfg.Server.Security.HSTSMaxAgeSeconds,
	})
}

func serveServer(ctx context.Context, cfg *config.Config, app *App) (returnErr error) {
	// serveServer is the single process owner for the HA-capable Box runtime.
	// Keep a fallback defer so middleware or main-listener startup failures also
	// drain Box requests before Run closes the shared claim ledger.
	if app != nil && app.BoxMediaRuntime != nil {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
			defer cancel()
			if err := app.BoxMediaRuntime.Shutdown(shutdownCtx); err != nil && returnErr == nil {
				returnErr = fmt.Errorf("core.Run: Box media shutdown: %w", err)
			}
		}()
	}
	chain, err := serverMiddlewareChain(ctx, cfg, app)
	if err != nil {
		return err
	}

	addr := strings.TrimSpace(cfg.Server.ListenAddr)
	if addr == "" {
		addr = ":8080"
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           chain(app.Mux),
		ReadHeaderTimeout: serverDurationDefault(cfg.Server.ReadHeaderTimeoutSec, 15*time.Second),
		ReadTimeout:       serverDurationDefault(cfg.Server.ReadTimeoutSec, 120*time.Second),
		WriteTimeout:      0, // WebSocket sessions can be long-lived.
		IdleTimeout:       serverDurationDefault(cfg.Server.IdleTimeoutSec, 120*time.Second),
		MaxHeaderBytes:    serverIntDefault(cfg.Server.MaxHeaderBytes, 1<<20),
	}

	var listenConfig net.ListenConfig
	ln, err := listenConfig.Listen(ctx, "tcp", addr)
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

	var boxMediaErrors <-chan error
	if app.BoxMediaRuntime != nil {
		boxMediaErrors = app.BoxMediaRuntime.Errors()
	}
	var runtimeErr error
	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining connections")
	case err, ok := <-serveErr:
		if err != nil {
			runtimeErr = fmt.Errorf("core.Run: serve: %w", err)
		} else if !ok && ctx.Err() == nil {
			runtimeErr = errors.New("core.Run: main HTTP listener stopped unexpectedly")
		}
	case err, ok := <-boxMediaErrors:
		switch {
		case err != nil:
			_, components, _ := app.Health.Snapshot()
			if entry, exists := components[boxMediaHealthComponent]; !exists || entry.Status != StatusUnavailable {
				app.Health.SetReady(boxMediaHealthComponent, StatusUnavailable, "runtime dependency failed")
			}
			runtimeErr = fmt.Errorf("core.Run: Box media runtime: %w", err)
		case !ok && ctx.Err() == nil:
			app.Health.SetReady(boxMediaHealthComponent, StatusUnavailable, "runtime stopped unexpectedly")
			runtimeErr = errors.New("core.Run: Box media runtime stopped unexpectedly")
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	// Drain the HA-capable Box path before any shared lifecycle or claim-ledger
	// dependency can be torn down. Box shutdown cancels active request contexts
	// and does not return until every tracked handler has exited.
	if app.BoxMediaRuntime != nil {
		if err := app.BoxMediaRuntime.Shutdown(shutdownCtx); err != nil {
			slog.Warn("Box media shutdown did not complete cleanly", "err", err)
			if runtimeErr == nil {
				runtimeErr = fmt.Errorf("core.Run: Box media shutdown: %w", err)
			}
		}
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("HTTP shutdown did not complete cleanly", "err", err)
		_ = srv.Close()
	}
	if app.Lifecycle != nil {
		if err := app.Lifecycle.Shutdown(shutdownCtx); err != nil {
			slog.Warn("server lifecycle shutdown", "err", err)
		}
	}
	// Flush the OpenTelemetry trace pipeline last so spans emitted during
	// lifecycle shutdown are exported before the batch processor stops.
	if app.telemetryShutdown != nil {
		if err := app.telemetryShutdown(shutdownCtx); err != nil {
			slog.Warn("telemetry flush/shutdown failed", "err", err)
		}
	}

	// Drain any lingering serve error.
	if err := <-serveErr; err != nil && runtimeErr == nil {
		runtimeErr = fmt.Errorf("core.Run: serve (post-shutdown): %w", err)
	}
	return runtimeErr
}

func serverMiddlewareChain(ctx context.Context, cfg *config.Config, app *App) (func(http.Handler) http.Handler, error) {
	if app == nil {
		return nil, errors.New("core.Run: app is required")
	}
	// Order matters: Recover wraps everything (panics from any middleware
	// or handler land in the JSON 500), Logging runs early so even auth
	// failures get an access-log line, CORS runs before Auth so preflight
	// OPTIONS bypasses the bearer check, Auth attaches Identity to the
	// context, and RateLimit reads that Identity to bucket per-user
	// rather than per-IP.
	app.AuthState = middleware.NewAuthState(
		cfg.Server.AuthMode,
		cfg.Server.BearerTokenEnv,
		cfg.Server.EdgeAuthSecretEnv,
		cfg.Server.AdminUsername,
		cfg.Server.AdminPasswordHash,
	)
	app.AuthState.SetSmokeTokenEnv(cfg.Server.SmokeTokenEnv)
	publicPaths := serverPublicPaths()
	publicRoutes := serverPublicRoutes()
	if app.DeviceAgentBridgeMounted {
		publicRoutes = append(publicRoutes, deviceAgentAuthRoutes()...)
	}
	bootstrapPaths := serverBootstrapPaths()
	oidcVerifier, err := serverOIDCVerifier(cfg)
	if err != nil {
		return nil, fmt.Errorf("core.Run: %w", err)
	}
	return middleware.Chain(
		middleware.Recover(),
		middleware.RequestID(),
		middleware.Logging(),
		middleware.CORS(cfg.Server.CORSAllowedOrigins),
		serverSecurityHeaders(cfg),
		middleware.Auth(middleware.AuthOptions{
			ModeProvider:        app.AuthState.Mode,
			BearerTokenProvider: app.AuthState.BearerToken,
			EdgeSecretProvider:  app.AuthState.EdgeSecret,
			AdminUsernameProvider: func() string {
				if app.Cfg == nil || !app.Cfg.Server.AdminAuthEnabled {
					return ""
				}
				return app.AuthState.AdminUsername()
			},
			AdminPasswordHashProvider: func() string {
				if app.Cfg == nil || !app.Cfg.Server.AdminAuthEnabled {
					return ""
				}
				return app.AuthState.AdminPasswordHash()
			},
			SmokeTokenProvider: app.AuthState.SmokeToken,
			// Health endpoints are always public so external probes (Render,
			// Kubernetes) can hit them without credentials.
			AllowPublicPaths:      publicPaths,
			AllowPublicRoutes:     publicRoutes,
			HTMLUnauthorizedPaths: serverAdminUIPaths(),
			AllowBootstrapPaths:   bootstrapPaths,
			AllowBootstrapRoutes:  serverBootstrapAuthRoutes(),
			BearerRole:            cfg.Server.BearerRole,
			BootstrapAllowed: func(r *http.Request) bool {
				return serverSettingsBootstrapWriteAllowed(app)
			},
			// Defence-in-depth: if the operator bound to a non-loopback
			// address, refuse to issue the implicit anonymous Identity
			// from AuthModeNone even if config validation was bypassed.
			// ValidateServerProductionAuth already rejects this at
			// startup; this is a runtime backstop for code paths that
			// embed the server without calling that validator (tests,
			// in-process hosts, future helper binaries).
			RequireAuthenticatedMode: !config.IsLoopbackListenAddr(cfg.Server.ListenAddr),
			TrustedProxyCIDRs:        cfg.Server.TrustedProxyCIDRs,
			OIDCVerifier:             oidcVerifier,
			// Voice-agent tool bridge: the header carrying the per-session
			// bridge credential (accepted only on edge-HMAC identities).
			OboSubjectTokenHeader: cfg.Server.VoiceAgent.ToolBridge.CredentialHeader,
		}),
		middleware.RateLimit(middleware.RateLimitOptions{ //nolint:contextcheck // RateLimit receives the server lifetime context via options.Context; contextcheck does not model contained context fields.
			RequestsPerSecond: cfg.Server.RateLimitRPS,
			Burst:             cfg.Server.RateLimitBurst,
			Context:           ctx,
			// Health probes must never be rate-limited; otherwise a busy
			// neighbour could starve out Render's readiness checks during
			// real outages.
			AllowPublicPaths: publicPaths,
			// Audit S-4: cost-weighted bucket so a few expensive calls
			// (LLM, transcription, voice-agent session create) drain
			// the budget appropriately. Empty map falls back to flat
			// cost=1 — backwards compatible.
			EndpointCosts: cfg.Server.RateLimitEndpointCosts,
			// Audit S-5: hard daily ceiling for Plan="demo" (smoke
			// token) surface so a casual scraper can't burn provider
			// budget overnight. Zero disables.
			DemoDailyQuota: cfg.Server.DemoDailyQuota,
		}),
	), nil
}

func serverDurationDefault(seconds int, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func serverIntDefault(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
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

func mountModeDisabled(mux *http.ServeMux, mode Mode, patterns ...string) {
	if mux == nil {
		return
	}
	for _, pattern := range patterns {
		pattern := strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
			httpx.WriteModeDisabled(w, string(mode))
		})
	}
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
	// bootstrap stays compile-time explicit about durable backends.
	switch concreteStore := app.Store.(type) {
	case *store.SQLiteStore:
		persister := persona.NewSQLitePersister(concreteStore.DB())
		if err := reg.HydrateFrom(ctx, persister); err != nil {
			slog.Warn("persona: hydrate from store failed; falling back to TOML-only", "err", err)
		} else {
			slog.Info("persona: hydrated from SQLite store")
		}
		reg.WithPersister(persister)
	case *store.PostgresStore:
		persister := persona.NewPostgresPersister(concreteStore.DB())
		if err := reg.HydrateFrom(ctx, persister); err != nil {
			slog.Warn("persona: hydrate from Postgres store failed; falling back to TOML-only", "err", err)
		} else {
			slog.Info("persona: hydrated from Postgres store")
		}
		reg.WithPersister(persister)
	default:
		if app.Store != nil {
			slog.Info("persona: store backend does not support durable personas; admin writes are in-memory only")
		}
	}

	// (3) overlay TOML seeds. Seeds are tagged Source="toml" and never
	// round-trip to the persister — they're the baseline, not data.
	notes := persona.LoadSeeds(reg, cfg) //nolint:contextcheck // seed loading is in-memory TOML overlay work with no request or cancellable I/O.
	for _, note := range notes {
		slog.Debug("persona seed", "note", note)
	}
	app.PersonaRegistry = reg

	personaCount := len(reg.ListPersonas())
	roleCount := len(reg.ListRoles())
	sequenceCount := len(reg.ListSequences())
	detail := fmt.Sprintf("%d personas, %d roles, %d sequences", personaCount, roleCount, sequenceCount)
	if personaCount == 0 {
		app.Health.SetReady("persona.registry", StatusDegraded, "no personas seeded; clients must create one via POST /v1/personas")
	} else {
		app.Health.SetReady("persona.registry", StatusOK, detail)
	}
	slog.Info("persona registry ready", "summary", detail)
}
