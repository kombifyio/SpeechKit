//go:build linux

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

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/discovery"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

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
		AllowedTenants:   cfg.Server.OIDC.AllowedTenants,
		TenantClaim:      cfg.Server.OIDC.TenantClaim,
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

	// LAN-Announcement (opt-in, [server.discovery]): non-fatal — a homelab
	// without multicast still serves normally, clients just type the URL.
	var enabledModes []string
	for mode, on := range app.Modes {
		if on {
			enabledModes = append(enabledModes, string(mode))
		}
	}
	announcer, err := discovery.Start(cfg, app.Version, enabledModes)
	if err != nil {
		slog.Warn("mDNS discovery unavailable", "err", err)
	}
	defer announcer.Shutdown()

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
serveLoop:
	for {
		select {
		case <-ctx.Done():
			slog.Info("shutdown signal received, draining connections")
			break serveLoop
		case err, ok := <-serveErr:
			if err != nil {
				runtimeErr = fmt.Errorf("core.Run: serve: %w", err)
			} else if !ok && ctx.Err() == nil {
				runtimeErr = errors.New("core.Run: main HTTP listener stopped unexpectedly")
			}
			break serveLoop
		case err, ok := <-boxMediaErrors:
			// Box is an optional feature. One dead satellite listener or a
			// stalled local STT child must not take down dictation, assist,
			// Voice Agent, Wyoming, or the device-agent bridge.
			boxMediaErrors = nil
			switch {
			case err != nil:
				_, components, _ := app.Health.Snapshot()
				if entry, exists := components[boxMediaHealthComponent]; !exists || entry.Status != StatusUnavailable {
					app.Health.SetReady(boxMediaHealthComponent, StatusUnavailable, "runtime dependency failed")
				}
				slog.Error("Box media runtime failed; HTTP server continues", "err", err)
			case !ok && ctx.Err() == nil:
				app.Health.SetReady(boxMediaHealthComponent, StatusUnavailable, "runtime stopped unexpectedly")
				slog.Error("Box media runtime stopped unexpectedly; HTTP server continues")
			}
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
			// Liveness is always public. Detailed readiness and browser operator
			// UI paths can require authentication in hosted deployments.
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
