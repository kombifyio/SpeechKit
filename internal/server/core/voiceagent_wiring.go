//go:build linux

package core

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/toolbridge"
	vsserver "github.com/kombifyio/SpeechKit/internal/server/voiceagent"
)

// Supported provider strings for cfg.VoiceAgent.Provider.
const (
	ProviderGemini       = "gemini"
	ProviderOpenAI       = "openai"
	ProviderDeepgram     = "deepgram"
	ProviderAssemblyAI   = "assemblyai"
	ProviderCascaded     = "cascaded"
	ProviderKombifyAgent = "kombify-agent"
	ProviderMoshi        = "moshi"
)

// buildVoiceAgentHandler wires the server-target Voice Agent handler,
// dispatching between Gemini Live, Deepgram, AssemblyAI, OpenAI, Cascaded, and
// Moshi providers based on cfg.VoiceAgent.Provider.
//
// /readyz surfaces the selected provider's state; the handler is always
// mounted so POST /v1/voiceagent/sessions works even when the provider
// itself is degraded (clients get a `provider_connect_failed` error at WS
// upgrade rather than a 404 at session creation — operators can then read
// /readyz to see why).
func buildVoiceAgentHandler(ctx context.Context, cfg *config.Config, app *App) (*vsserver.Handler, string, error) {
	// Single source of truth with the catalog readiness surface: GET
	// /v1/catalog/readiness marks the voice_agent profile Active from the
	// same derivation, so the profile shown active is the provider that
	// actually serves a default session (kombify-SpeechKit-5nt5).
	defaultProvider := config.EffectiveVoiceAgentProvider(cfg)

	// Register a factory for every Voice-Agent-capable provider that builds on
	// this deployment so a client can switch backend per session via the WS
	// start frame ("laufend wechseln" without a redeploy). The switchable set
	// is the configured default plus Deepgram (native), OpenAI gpt-realtime
	// (native), AssemblyAI Voice Agent (native), Cascaded (STT router -> LLM
	// -> TTS), and — only when the operator opted into [providers.google] —
	// Gemini Live (native). Providers whose keys/deps are missing are skipped
	// and surfaced in the status line rather than failing the handler.
	candidates := dedupeProviders(defaultProvider, ProviderDeepgram, ProviderGemini, ProviderOpenAI, ProviderAssemblyAI, ProviderCascaded, ProviderKombifyAgent)
	factories := make(map[string]vsserver.ProviderFactory, len(candidates))
	statusByProvider := make(map[string]string, len(candidates))
	for _, p := range candidates {
		f, st, err := buildProviderFactory(ctx, cfg, app, p)
		if err != nil {
			statusByProvider[p] = "unavailable: " + err.Error()
			slog.Info("voiceagent: provider not registered (per-session switch disabled for it)", "provider", p, "reason", err)
			continue
		}
		factories[p] = f
		statusByProvider[p] = st
	}
	if factories[defaultProvider] == nil {
		return nil, "no usable default voice agent provider", errors.New("voiceagent: default provider " + defaultProvider + " is unavailable: " + statusByProvider[defaultProvider])
	}
	// Status line leads with the default provider's status (bootstrap prefix-
	// matches "ready"/"degraded"/"partial" on it), then lists the switchable
	// alternates that registered successfully.
	alts := make([]string, 0, len(factories))
	for p := range factories {
		if p != defaultProvider {
			alts = append(alts, p)
		}
	}
	sort.Strings(alts)
	status := statusByProvider[defaultProvider]
	if len(alts) > 0 {
		status += "; switchable: " + strings.Join(alts, ", ")
	}

	ticketTTL := time.Duration(cfg.Server.TicketTTLSec) * time.Second
	if cfg.Server.TicketTTLSec <= 0 {
		ticketTTL = 0
	}
	limits := cfg.VoiceAgentSessionLimits()
	manager, err := vsserver.NewSessionManager(vsserver.Options{
		TicketTTL:              ticketTTL,
		MaxGlobalSessions:      limits.MaxGlobalSessions,
		MaxPerIdentitySessions: limits.MaxPerIdentitySessions,
	})
	if err != nil {
		return nil, status, err
	}

	resolver := &personaResolver{
		cfg:      cfg,
		registry: app.PersonaRegistry,
	}

	idleTimeout := time.Duration(cfg.Server.VoiceAgentIdleTimeoutSec) * time.Second
	if cfg.Server.VoiceAgentIdleTimeoutSec < 0 {
		// Negative value disables the watchdog explicitly. Pass through
		// untouched; vsserver.New treats negatives as "disabled".
		idleTimeout = -1
	}
	maxSessionDuration := time.Duration(cfg.Server.VoiceAgentMaxSessionSec) * time.Second
	if cfg.Server.VoiceAgentMaxSessionSec <= 0 {
		maxSessionDuration = 0
	}

	h, err := vsserver.New(vsserver.HandlerOptions{
		Manager:            manager,
		Providers:          factories,
		DefaultProvider:    defaultProvider,
		Persona:            resolver,
		PublicURL:          cfg.Server.PublicURL,
		AllowedOrigins:     cfg.Server.CORSAllowedOrigins,
		TrustedProxyCIDRs:  cfg.Server.TrustedProxyCIDRs,
		IdleTimeout:        idleTimeout,
		MaxSessionDuration: maxSessionDuration,
		Store:              app.Store,
		LiveKit:            buildLiveKitIssuer(cfg, app),
		ReadLimit:          cfg.Server.WSReadLimitBytes,
		ToolRouter:         buildVoiceAgentToolRouter(cfg),
		Usage:              buildVoiceUsageReporter(),
	})
	if err != nil {
		return nil, status, err
	}
	return h, status, nil
}

func buildVoiceUsageReporter() vsserver.UsageReporter {
	endpoint := strings.TrimSpace(os.Getenv("KOMBIFY_USAGE_ENDPOINT"))
	if endpoint == "" {
		return nil
	}
	return vsserver.NewHTTPUsageReporter(endpoint)
}

// buildVoiceAgentToolRouter constructs the generic tool bridge from
// [server.voiceagent.tool_bridge]. Fail-closed: disabled or misconfigured
// bridges yield a nil router — sessions run tool-less rather than broken.
func buildVoiceAgentToolRouter(cfg *config.Config) vsserver.SessionToolRouter {
	tb := cfg.Server.VoiceAgent.ToolBridge
	if !tb.Enabled {
		return nil
	}
	bridge, err := toolbridge.New(toolbridge.Options{
		ManifestURL:        tb.ManifestURL,
		InvokeURL:          tb.InvokeURL,
		Timeout:            time.Duration(tb.TimeoutMs) * time.Millisecond,
		MaxCallsPerSession: tb.MaxCallsPerSession,
	})
	if err != nil {
		// validation.go could not gain this check in this change (parked WIP);
		// degrade loudly instead of shipping a half-configured bridge.
		slog.Error("voiceagent: tool bridge enabled but misconfigured; running tool-less", "err", err)
		return nil
	}
	slog.Info("voiceagent: tool bridge enabled",
		"manifest_url", tb.ManifestURL,
		"invoke_url", tb.InvokeURL,
		"timeout_ms", tb.TimeoutMs,
		"max_calls_per_session", tb.MaxCallsPerSession,
	)
	return &toolBridgeRouter{bridge: bridge, sessions: make(map[string]toolBridgeSessionMeta)}
}

func buildLiveKitIssuer(cfg *config.Config, app *App) *vsserver.LiveKitTokenIssuer {
	lk := cfg.Server.LiveKit
	if !lk.Enabled {
		if app != nil && app.Health != nil {
			app.Health.SetReady("livekit.token_mint", StatusOK, "disabled")
		}
		return nil
	}
	apiKey := strings.TrimSpace(config.ResolveSecret(lk.APIKeyEnv))
	apiSecret := strings.TrimSpace(config.ResolveSecret(lk.APISecretEnv))
	url := strings.TrimRight(strings.TrimSpace(lk.URL), "/")
	if url == "" || apiKey == "" || apiSecret == "" {
		if app != nil && app.Health != nil {
			app.Health.SetReady("livekit.token_mint", StatusDegraded, "enabled but URL/API key/API secret is missing")
		}
		return &vsserver.LiveKitTokenIssuer{
			URL:        url,
			APIKey:     apiKey,
			APISecret:  apiSecret,
			TokenTTL:   time.Duration(lk.TokenTTLSec) * time.Second,
			RoomPrefix: lk.RoomPrefix,
		}
	}
	if app != nil && app.Health != nil {
		app.Health.SetReady("livekit.token_mint", StatusOK, url)
	}
	return &vsserver.LiveKitTokenIssuer{
		URL:        url,
		APIKey:     apiKey,
		APISecret:  apiSecret,
		TokenTTL:   time.Duration(lk.TokenTTLSec) * time.Second,
		RoomPrefix: lk.RoomPrefix,
	}
}

// buildProviderFactory constructs the ProviderFactory matching the configured
// provider string. Returns a human-readable status for /readyz.
func buildProviderFactory(ctx context.Context, cfg *config.Config, app *App, provider string) (vsserver.ProviderFactory, string, error) {
	provider = normalizeVoiceAgentProvider(provider)
	switch provider {
	case ProviderGemini:
		// Opt-in BYOK: Gemini Live is switchable only when the operator
		// enabled [providers.google] or selected it as the Voice Agent
		// provider; it is never registered implicitly.
		if !cfg.Providers.Google.Enabled && config.EffectiveVoiceAgentProvider(cfg) != ProviderGemini {
			return nil, "unavailable: Google is an opt-in provider and [providers.google] is not enabled", errors.New("voiceagent: gemini live requires [providers.google] enabled = true")
		}
		apiKey := resolveRealtimeAPIKey(cfg, provider)
		status := "ready (gemini)"
		if apiKey == "" {
			status = "degraded: no Google API key; Gemini Live sessions will fail at upgrade"
		}
		return &geminiProviderFactory{region: cfg.Providers.Google.Region}, status, nil

	case ProviderOpenAI:
		apiKey := resolveRealtimeAPIKey(cfg, provider)
		status := "ready (openai)"
		if apiKey == "" {
			status = "degraded: no OpenAI API key; gpt-realtime-2 sessions will fail at upgrade"
		}
		return &openaiProviderFactory{}, status, nil

	case ProviderDeepgram:
		key := resolveRealtimeAPIKey(cfg, provider)
		status := "ready (deepgram)"
		if strings.TrimSpace(key) == "" {
			status = "degraded: no Deepgram API key; Deepgram Voice Agent sessions will fail at upgrade"
		}
		return &deepgramProviderFactory{cfg: cfg}, status, nil

	case ProviderAssemblyAI:
		key := resolveRealtimeAPIKey(cfg, provider)
		status := "ready (assemblyai)"
		if strings.TrimSpace(key) == "" {
			status = "degraded: no AssemblyAI API key; AssemblyAI Voice Agent sessions will fail at upgrade"
		}
		return &assemblyAIProviderFactory{}, status, nil

	case ProviderCascaded:
		ensureSharedAIDeps(ctx, app)
		if app.STTRouter == nil {
			return nil, "degraded: cascaded provider needs STT router", errors.New("voiceagent: cascaded provider requires STT router (check provider keys)")
		}
		if app.AgentFlow == nil {
			return nil, "degraded: no Genkit agent models configured", errors.New("voiceagent: cascaded provider requires at least one Genkit agent model")
		}
		status := "ready (cascaded)"
		if !app.TTSEnabled {
			status = "partial: cascaded provider running without TTS (transcript-only)"
		}
		var ttsImpl vsserver.CascadedTTS
		if app.TTSRouter != nil {
			ttsImpl = app.TTSRouter
		}
		factory := &cascadedProviderFactory{
			stt:             app.STTRouter,
			speakerStreamer: app.STTRouter,
			agent:           vsserver.NewAgentFlowAdapter(app.AgentFlow),
			tts:             ttsImpl,
			cfg:             cfg,
		}
		return factory, status, nil

	case ProviderKombifyAgent:
		ensureSharedAIDeps(ctx, app)
		if app.STTRouter == nil {
			return nil, "degraded: kombify-agent provider needs STT router", errors.New("voiceagent: kombify-agent provider requires STT router")
		}
		var ttsImpl vsserver.CascadedTTS
		if app.TTSRouter != nil {
			ttsImpl = app.TTSRouter
		}
		status := "ready (kombify-agent)"
		if strings.TrimSpace(os.Getenv("KOMBIFY_A2A_DELEGATION_SIGNING_SECRET")) == "" {
			status = "degraded: kombify-agent A2A delegation signer is not configured"
		}
		return &registeredAgentProviderFactory{
			deps: vsserver.CascadedDeps{
				STT: app.STTRouter, SpeakerStreamer: app.STTRouter, TTS: ttsImpl,
				Config: vsserver.CascadedConfig{TTSFormat: firstNonEmpty(cfg.TTS.Format, "mp3"), TTSSpeed: nonZeroFloat(cfg.TTS.Speed, 1.0)},
			},
		}, status, nil

	case ProviderMoshi:
		// M9b implements this; ship a stub factory that produces helpful
		// errors so operators discover the misconfiguration at session
		// creation rather than silently succeeding and failing at upgrade.
		return &moshiStubFactory{}, "experimental_unavailable: moshi provider is not yet implemented (pending M9b)", nil

	default:
		return nil, "unknown provider", errors.New("voiceagent: unsupported provider " + provider)
	}
}

// dedupeProviders normalises and de-duplicates a provider list while preserving
// first-seen order, dropping empties. Used to assemble the switchable Voice
// Agent provider set without registering the same backend twice.
func dedupeProviders(providers ...string) []string {
	seen := make(map[string]struct{}, len(providers))
	out := make([]string, 0, len(providers))
	for _, p := range providers {
		n := normalizeVoiceAgentProvider(p)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}
