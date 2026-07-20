//go:build linux

package core

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/persona"
	"github.com/kombifyio/SpeechKit/internal/server/toolbridge"
	vsserver "github.com/kombifyio/SpeechKit/internal/server/voiceagent"
	vskernel "github.com/kombifyio/SpeechKit/internal/voiceagent"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	livecatalog "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// Supported provider strings for cfg.VoiceAgent.Provider.
const (
	ProviderGemini     = "gemini"
	ProviderOpenAI     = "openai"
	ProviderDeepgram   = "deepgram"
	ProviderAssemblyAI = "assemblyai"
	ProviderCascaded   = "cascaded"
	ProviderMoshi      = "moshi"
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
	defaultProvider := strings.ToLower(strings.TrimSpace(cfg.VoiceAgent.Provider))
	if defaultProvider == "" {
		defaultProvider = ProviderGemini
	}
	defaultProvider = normalizeVoiceAgentProvider(defaultProvider)

	// Register a factory for every Voice-Agent-capable provider that builds on
	// this deployment so a client can switch backend per session via the WS
	// start frame ("laufend wechseln" without a redeploy). The switchable set
	// is the configured default plus Deepgram (native), Gemini Live (native),
	// OpenAI gpt-realtime (native), AssemblyAI Voice Agent (native), and
	// Cascaded (STT router -> LLM -> TTS). Providers whose keys/deps are
	// missing are skipped and surfaced in the status line rather than failing
	// the handler.
	candidates := dedupeProviders(defaultProvider, ProviderDeepgram, ProviderGemini, ProviderOpenAI, ProviderAssemblyAI, ProviderCascaded)
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
	})
	if err != nil {
		return nil, status, err
	}
	return h, status, nil
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

// toolBridgeRouter adapts the platform-neutral toolbridge.Bridge to the
// linux-only vsserver.SessionToolRouter interface. It remembers each
// session's resolved locale/persona from Definitions so Execute can populate
// the invoke request's session block; entries are purged lazily.
type toolBridgeRouter struct {
	bridge *toolbridge.Bridge

	mu       sync.Mutex
	sessions map[string]toolBridgeSessionMeta
}

type toolBridgeSessionMeta struct {
	locale    string
	personaID string
	touched   time.Time
}

const toolBridgeSessionMetaTTL = 2 * time.Hour

func (r *toolBridgeRouter) Definitions(ctx context.Context, session *vsserver.ManagedSession, cfg vsserver.LiveConfigFrame) []vsserver.ToolDefinitionFrame {
	if session == nil || strings.TrimSpace(session.BridgeCredential) == "" {
		// Fail-closed: no forwarded credential means no tools for this
		// session (e.g. direct bearer callers, self-hosted without an edge).
		return nil
	}
	r.rememberSession(session.ID, cfg)
	defs, err := r.bridge.Definitions(ctx, r.bridgeSession(session))
	if err != nil {
		slog.Warn("voiceagent: tool bridge manifest fetch failed; session continues tool-less", "err", err) // #nosec G706 -- structured attribute, no credential in the error path.
		return nil
	}
	out := make([]vsserver.ToolDefinitionFrame, 0, len(defs))
	for _, def := range defs {
		out = append(out, vsserver.ToolDefinitionFrame{
			Name:                 def.Name,
			Description:          def.Description,
			ParametersJSONSchema: def.Parameters,
			// Bridge tools are always blocking: Gemini 3.1 flash live rejects
			// non_blocking function declarations.
			Behavior:  string(vskernel.ToolBehaviorBlocking),
			TimeoutMs: def.TimeoutMs,
		})
	}
	return out
}

func (r *toolBridgeRouter) Execute(ctx context.Context, session *vsserver.ManagedSession, call vsserver.ToolCall) (map[string]any, bool) {
	if session == nil || strings.TrimSpace(session.BridgeCredential) == "" {
		return nil, false
	}
	return r.bridge.Execute(ctx, r.bridgeSession(session), call.Name, call.Args)
}

func (r *toolBridgeRouter) bridgeSession(session *vsserver.ManagedSession) toolbridge.Session {
	meta := r.sessionMeta(session.ID)
	return toolbridge.Session{
		ID:         session.ID,
		Locale:     meta.locale,
		PersonaID:  meta.personaID,
		Credential: session.BridgeCredential,
	}
}

func (r *toolBridgeRouter) rememberSession(sessionID string, cfg vsserver.LiveConfigFrame) {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, meta := range r.sessions {
		if now.Sub(meta.touched) > toolBridgeSessionMetaTTL {
			delete(r.sessions, id)
		}
	}
	r.sessions[sessionID] = toolBridgeSessionMeta{
		locale:    cfg.Locale,
		personaID: cfg.PersonaID,
		touched:   now,
	}
}

func (r *toolBridgeRouter) sessionMeta(sessionID string) toolBridgeSessionMeta {
	r.mu.Lock()
	defer r.mu.Unlock()
	meta := r.sessions[sessionID]
	meta.touched = time.Now()
	r.sessions[sessionID] = meta
	return meta
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
		apiKey := resolveRealtimeAPIKey(cfg, provider)
		status := "ready (gemini)"
		if apiKey == "" {
			status = "degraded: no Google API key; Gemini Live sessions will fail at upgrade"
		}
		return &geminiProviderFactory{}, status, nil

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

// ── persona resolver ────────────────────────────────────────────────────────

// personaResolver implements vsserver.PersonaResolver by composing a
// LiveConfigFrame from three layers, in order of precedence:
//
//  1. explicit fields on the client-sent StartFrame (highest priority)
//  2. the resolved persona + role + sequence step from the registry
//  3. the server-wide [voice_agent] config (lowest)
//
// This preserves the previous behaviour when no persona_id and no configured
// agent profile are supplied, while giving clients full control when they do
// pick a persona.
type personaResolver struct {
	cfg      *config.Config
	registry *persona.Registry
}

func (r *personaResolver) Resolve(start vsserver.StartFrame) (vsserver.LiveConfigFrame, error) {
	return r.resolve(start, 0)
}

func (r *personaResolver) ResolveStep(start vsserver.StartFrame, stepIndex int) (vsserver.LiveConfigFrame, error) {
	return r.resolve(start, stepIndex)
}

func normalizeStartSpeakerOptions(opts *speaker.Options) speaker.Options {
	if opts == nil {
		return speaker.Options{}
	}
	return opts.Normalized()
}

func (r *personaResolver) resolve(start vsserver.StartFrame, stepIndex int) (vsserver.LiveConfigFrame, error) {
	va := r.cfg.VoiceAgent

	// The adapter normalises start.Provider to the resolved backend before
	// calling Resolve, so the API key + model match THIS session's provider
	// (empty falls back to the configured default provider's credentials).
	provider := normalizeVoiceAgentProvider(start.Provider)
	if provider == "" {
		provider = normalizeVoiceAgentProvider(va.Provider)
	}

	frame := vsserver.LiveConfigFrame{
		Model:             r.resolveModel(start.Model, provider),
		FallbackModel:     va.FallbackModel,
		APIKey:            resolveRealtimeAPIKey(r.cfg, provider),
		Voice:             firstNonEmpty(start.Voice, va.Voice),
		SystemPrompt:      firstNonEmpty(start.SystemPromptOverride, va.FrameworkPrompt),
		RefinementPrompt:  va.RefinementPrompt,
		Locale:            firstNonEmpty(start.Locale, r.cfg.General.Language, "en"),
		Automatic:         va.AutomaticActivityDetection,
		StartSensitivity:  va.VADStartSensitivity,
		EndSensitivity:    va.VADEndSensitivity,
		PrefixPaddingMs:   intToInt32Clamp(va.VADPrefixPaddingMs),
		SilenceDurationMs: intToInt32Clamp(va.VADSilenceDurationMs),
		ActivityHandling:  va.ActivityHandling,
		TurnCoverage:      va.TurnCoverage,
		Speaker:           normalizeStartSpeakerOptions(start.Speaker),
	}

	// Layer (2): persona + role + sequence. Explicit start-frame personas win;
	// otherwise a non-default server-wide agent profile acts as the default
	// persona for clients that do not send persona_id yet.
	personaID := strings.TrimSpace(start.PersonaID)
	if personaID == "" {
		if configured := voiceagentprofile.NormalizeID(va.AgentProfileID); configured != voiceagentprofile.DefaultID {
			personaID = configured
		}
	}
	if personaID != "" && r.registry != nil {
		resolved, err := r.registry.Resolve(personaID, start.RoleID, start.SequenceID, stepIndex)
		if err != nil {
			return vsserver.LiveConfigFrame{}, err
		}
		frame.PersonaID = resolved.PersonaID
		frame.RoleID = resolved.RoleID
		frame.SequenceID = resolved.SequenceID
		frame.SequenceCompletion = resolved.SequenceCompletion
		frame.SequenceMaxTurns = resolved.SequenceMaxTurns
		frame.StepID = resolved.StepID
		frame.StepIndex = resolved.StepIndex
		frame.StepCount = resolved.StepCount
		frame.StepInstruction = resolved.StepInstruction
		frame.StepExitCriteria = resolved.StepExitCriteria
		frame.StepMaxTurns = resolved.StepMaxTurns
		if strings.TrimSpace(start.Voice) == "" && resolved.Voice != "" {
			frame.Voice = resolved.Voice
		}
		if strings.TrimSpace(start.Locale) == "" && resolved.Locale != "" {
			frame.Locale = resolved.Locale
		}
		if strings.TrimSpace(start.SystemPromptOverride) == "" && resolved.SystemPrompt != "" {
			frame.SystemPrompt = resolved.SystemPrompt
		} else if strings.TrimSpace(start.SystemPromptOverride) != "" && resolved.StepInstruction != "" {
			frame.SystemPrompt = composeStartOverrideWithStep(start.SystemPromptOverride, resolved.StepID, resolved.StepInstruction)
		}
		if resolved.RefinementPrompt != "" {
			frame.RefinementPrompt = resolved.RefinementPrompt
		}
		// Role VAD/activity fields override config defaults only when they
		// are non-empty — this keeps roles minimal without wiping server
		// defaults that the admin cares about.
		if resolved.AutomaticVAD {
			frame.Automatic = true
		}
		if resolved.StartSensitivity != "" {
			frame.StartSensitivity = resolved.StartSensitivity
		}
		if resolved.EndSensitivity != "" {
			frame.EndSensitivity = resolved.EndSensitivity
		}
		if resolved.PrefixPaddingMs != 0 {
			frame.PrefixPaddingMs = resolved.PrefixPaddingMs
		}
		if resolved.SilenceDurationMs != 0 {
			frame.SilenceDurationMs = resolved.SilenceDurationMs
		}
		if resolved.ActivityHandling != "" {
			frame.ActivityHandling = resolved.ActivityHandling
		}
		if resolved.TurnCoverage != "" {
			frame.TurnCoverage = resolved.TurnCoverage
		}
	}

	// Layer (1): explicit client activity-detection override.
	if start.ActivityDetection != nil {
		ad := start.ActivityDetection
		frame.Automatic = ad.Automatic
		if ad.StartSensitivity != "" {
			frame.StartSensitivity = ad.StartSensitivity
		}
		if ad.EndSensitivity != "" {
			frame.EndSensitivity = ad.EndSensitivity
		}
		if ad.PrefixPaddingMs != 0 {
			frame.PrefixPaddingMs = ad.PrefixPaddingMs
		}
		if ad.SilenceDurationMs != 0 {
			frame.SilenceDurationMs = ad.SilenceDurationMs
		}
		if ad.ActivityHandling != "" {
			frame.ActivityHandling = ad.ActivityHandling
		}
		if ad.TurnCoverage != "" {
			frame.TurnCoverage = ad.TurnCoverage
		}
	}
	return frame, nil
}

func intToInt32Clamp(value int) int32 {
	const (
		maxInt32 = 1<<31 - 1
		minInt32 = -1 << 31
	)
	switch {
	case value > maxInt32:
		return maxInt32
	case value < minInt32:
		return minInt32
	default:
		return int32(value) // #nosec G115 -- value is clamped to the int32 range above.
	}
}

func (r *personaResolver) resolveModel(startModel, provider string) string {
	if explicit := strings.TrimSpace(startModel); explicit != "" {
		return explicit
	}
	provider = normalizeVoiceAgentProvider(provider)
	if provider == ProviderOpenAI {
		return firstNonEmpty(r.cfg.Providers.OpenAI.RealtimeModel, liveDefaultModel(provider), vskernel.DefaultOpenAIRealtimeModel)
	}
	configuredProvider := normalizeVoiceAgentProvider(r.cfg.VoiceAgent.Provider)
	if provider == configuredProvider && strings.TrimSpace(r.cfg.VoiceAgent.Model) != "" {
		return strings.TrimSpace(r.cfg.VoiceAgent.Model)
	}
	if model := liveDefaultModel(provider); model != "" {
		return model
	}
	return ""
}

func liveDefaultModel(provider string) string {
	cfg, ok := livecatalog.DefaultLiveConfigForProvider(provider)
	if !ok {
		return ""
	}
	return strings.TrimSpace(cfg.Model)
}

func composeStartOverrideWithStep(prompt, stepID, stepInstruction string) string {
	prompt = strings.TrimSpace(prompt)
	stepInstruction = strings.TrimSpace(stepInstruction)
	if stepInstruction == "" {
		return prompt
	}
	if prompt == "" {
		return stepInstruction
	}
	return prompt + "\n\n[Current step: " + stepID + "]\n" + stepInstruction
}

// resolveRealtimeAPIKey selects the API key matching the configured Voice
// Agent provider. The persona resolver receives this so the LiveConfigFrame
// it produces carries the right credential downstream — Gemini Live, Deepgram,
// AssemblyAI, OpenAI Realtime, and the Cascaded pipeline have distinct env vars.
func resolveRealtimeAPIKey(cfg *config.Config, provider string) string {
	value, _, err := config.ResolveProviderCredentialValue(cfg, realtimeCredentialTarget(provider))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func realtimeCredentialTarget(provider string) string {
	switch normalizeVoiceAgentProvider(provider) {
	case ProviderOpenAI:
		return "openai"
	case ProviderDeepgram:
		return "deepgram"
	case ProviderAssemblyAI:
		return "assemblyai"
	default:
		return "google"
	}
}

// ── Gemini Live provider factory + bridge ───────────────────────────────────

type geminiProviderFactory struct{}

func (f *geminiProviderFactory) NewProvider() vsserver.LiveProviderAdapter {
	return &geminiLiveBridge{inner: vskernel.NewGeminiLive()}
}

// geminiLiveBridge adapts the Framework kernel's Gemini Live implementation
// to the narrow interface the WebSocket handler consumes. The translation
// is mostly field-for-field; kernel enum types are rebuilt from the string
// fields on vsserver.LiveConfigFrame.
type geminiLiveBridge struct {
	inner *vskernel.GeminiLive
}

func (b *geminiLiveBridge) Connect(ctx context.Context, cfg vsserver.LiveConfigFrame) error {
	if cfg.APIKey == "" {
		return errors.New("voiceagent: no Google API key configured for this deployment")
	}
	liveCfg := vskernel.LiveConfig{
		Model:            cfg.Model,
		FallbackModel:    cfg.FallbackModel,
		APIKey:           cfg.APIKey,
		Voice:            cfg.Voice,
		FrameworkPrompt:  cfg.SystemPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Locale:           cfg.Locale,
		Speaker:          cfg.Speaker,
		Tools:            kernelToolDefinitions(cfg.Tools),
		Policies: vskernel.LivePolicies{
			EnableInputAudioTranscription:  true,
			EnableOutputAudioTranscription: true,
			ActivityDetection: vskernel.ActivityDetectionPolicy{
				Automatic:         cfg.Automatic,
				StartSensitivity:  vskernel.StartSensitivity(strings.ToLower(cfg.StartSensitivity)),
				EndSensitivity:    vskernel.EndSensitivity(strings.ToLower(cfg.EndSensitivity)),
				PrefixPaddingMs:   cfg.PrefixPaddingMs,
				SilenceDurationMs: cfg.SilenceDurationMs,
				ActivityHandling:  vskernel.ActivityHandling(strings.ToLower(cfg.ActivityHandling)),
				TurnCoverage:      vskernel.TurnCoverage(strings.ToLower(cfg.TurnCoverage)),
			},
		},
	}
	if err := b.inner.Connect(ctx, liveCfg); err != nil {
		slog.Warn("voiceagent: Gemini Live connect failed", "err", err)
		return err
	}
	return nil
}

func (b *geminiLiveBridge) SendAudio(chunk []byte) error { return b.inner.SendAudio(chunk) }
func (b *geminiLiveBridge) SendAudioStreamEnd() error    { return b.inner.SendAudioStreamEnd() }
func (b *geminiLiveBridge) SendText(text string) error   { return b.inner.SendText(text) }
func (b *geminiLiveBridge) Close() error                 { return b.inner.Close() }
func (b *geminiLiveBridge) Name() string                 { return b.inner.Name() }
func (b *geminiLiveBridge) SupportsLiveKitTransport() bool {
	return true
}

func (b *geminiLiveBridge) UpdateInstructions(_ context.Context, cfg vsserver.LiveConfigFrame) error {
	text := vsserver.RenderHostInstructionUpdate(cfg)
	if text == "" {
		return nil
	}
	return b.inner.SendText(text)
}

func (b *geminiLiveBridge) SendToolResponse(frame vsserver.ToolResponseFrame) error {
	return b.inner.SendToolResponse(vskernel.ToolResponse{
		ID:       frame.ID,
		Name:     frame.Name,
		Response: frame.Response,
	})
}

func (b *geminiLiveBridge) Receive(ctx context.Context) (*vsserver.LiveMessage, error) {
	msg, err := b.inner.Receive(ctx)
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, nil
	}
	return mapKernelLiveMessage(msg), nil
}

func mapKernelLiveMessage(msg *vskernel.LiveMessage) *vsserver.LiveMessage {
	if msg == nil {
		return nil
	}
	eventTypes := make([]string, 0, len(msg.EventTypes))
	for _, eventType := range msg.EventTypes {
		eventTypes = append(eventTypes, string(eventType))
	}
	return &vsserver.LiveMessage{
		EventType:              string(msg.EventType),
		EventTypes:             eventTypes,
		ProviderMetadata:       msg.ProviderMetadata,
		Audio:                  msg.Audio,
		Done:                   msg.Done,
		InputTranscript:        msg.InputTranscript,
		InputTranscriptDone:    msg.InputTranscriptDone,
		InputSpeakerLabel:      msg.InputSpeakerLabel,
		InputPersonID:          msg.InputPersonID,
		InputDisplayName:       msg.InputDisplayName,
		InputSpeakerConfidence: msg.InputSpeakerConfidence,
		OutputTranscript:       msg.OutputTranscript,
		OutputTranscriptDone:   msg.OutputTranscriptDone,
		ToolCalls:              mapKernelToolCalls(msg.ToolCalls),
		Interrupted:            msg.Interrupted,
		GoAway:                 msg.GoAway,
		SessionResumable:       msg.SessionResumable,
	}
}

// kernelToolDefinitions maps server-frame tool definitions (from the tool
// bridge) onto the kernel's live ToolDefinition type. Used by the gemini and
// openai live bridges; cascaded has no tool support and skips it.
func kernelToolDefinitions(defs []vsserver.ToolDefinitionFrame) []vskernel.ToolDefinition {
	if len(defs) == 0 {
		return nil
	}
	out := make([]vskernel.ToolDefinition, 0, len(defs))
	for _, def := range defs {
		out = append(out, vskernel.ToolDefinition{
			Name:                 def.Name,
			Description:          def.Description,
			ParametersJSONSchema: def.ParametersJSONSchema,
			Behavior:             vskernel.ToolBehavior(def.Behavior),
		})
	}
	return out
}

func mapKernelToolCalls(calls []vskernel.ToolCall) []vsserver.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]vsserver.ToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, vsserver.ToolCall{
			ID:   call.ID,
			Name: call.Name,
			Args: call.Args,
		})
	}
	return out
}

// ── OpenAI Realtime provider factory + bridge ───────────────────────────────

type openaiProviderFactory struct{}

func (f *openaiProviderFactory) NewProvider() vsserver.LiveProviderAdapter {
	return &openaiLiveBridge{inner: vskernel.NewOpenAILive()}
}

// openaiLiveBridge adapts the kernel's OpenAILive provider to the narrow
// interface the WebSocket handler consumes. Same translation pattern as
// geminiLiveBridge — kernel enum types are rebuilt from string fields on
// vsserver.LiveConfigFrame.
type openaiLiveBridge struct {
	inner *vskernel.OpenAILive
}

func (b *openaiLiveBridge) Connect(ctx context.Context, cfg vsserver.LiveConfigFrame) error {
	if cfg.APIKey == "" {
		return errors.New("voiceagent: no OpenAI API key configured for this deployment")
	}
	liveCfg := vskernel.LiveConfig{
		Model:            cfg.Model,
		FallbackModel:    cfg.FallbackModel,
		APIKey:           cfg.APIKey,
		Voice:            cfg.Voice,
		FrameworkPrompt:  cfg.SystemPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Locale:           cfg.Locale,
		Speaker:          cfg.Speaker,
		Tools:            kernelToolDefinitions(cfg.Tools),
		Policies: vskernel.LivePolicies{
			EnableInputAudioTranscription:  true,
			EnableOutputAudioTranscription: true,
			ActivityDetection: vskernel.ActivityDetectionPolicy{
				Automatic:         cfg.Automatic,
				StartSensitivity:  vskernel.StartSensitivity(strings.ToLower(cfg.StartSensitivity)),
				EndSensitivity:    vskernel.EndSensitivity(strings.ToLower(cfg.EndSensitivity)),
				PrefixPaddingMs:   cfg.PrefixPaddingMs,
				SilenceDurationMs: cfg.SilenceDurationMs,
				ActivityHandling:  vskernel.ActivityHandling(strings.ToLower(cfg.ActivityHandling)),
				TurnCoverage:      vskernel.TurnCoverage(strings.ToLower(cfg.TurnCoverage)),
			},
		},
	}
	if err := b.inner.Connect(ctx, liveCfg); err != nil {
		slog.Warn("voiceagent: OpenAI Realtime connect failed", "err", err)
		return err
	}
	return nil
}

func (b *openaiLiveBridge) SendAudio(chunk []byte) error { return b.inner.SendAudio(chunk) }
func (b *openaiLiveBridge) SendAudioStreamEnd() error    { return b.inner.SendAudioStreamEnd() }
func (b *openaiLiveBridge) SendText(text string) error   { return b.inner.SendText(text) }
func (b *openaiLiveBridge) Close() error                 { return b.inner.Close() }
func (b *openaiLiveBridge) Name() string                 { return b.inner.Name() }
func (b *openaiLiveBridge) SupportsLiveKitTransport() bool {
	return true
}

func (b *openaiLiveBridge) UpdateInstructions(ctx context.Context, cfg vsserver.LiveConfigFrame) error {
	liveCfg := vskernel.LiveConfig{
		Model:            cfg.Model,
		Voice:            cfg.Voice,
		FrameworkPrompt:  cfg.SystemPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Locale:           cfg.Locale,
		Speaker:          cfg.Speaker,
	}
	return b.inner.UpdateInstructions(ctx, liveCfg)
}

func (b *openaiLiveBridge) SendToolResponse(frame vsserver.ToolResponseFrame) error {
	return b.inner.SendToolResponse(vskernel.ToolResponse{
		ID:       frame.ID,
		Name:     frame.Name,
		Response: frame.Response,
	})
}

func (b *openaiLiveBridge) Receive(ctx context.Context) (*vsserver.LiveMessage, error) {
	msg, err := b.inner.Receive(ctx)
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, nil
	}
	return mapKernelLiveMessage(msg), nil
}

// ── AssemblyAI Voice Agent provider factory + bridge ────────────────────────

type assemblyAIProviderFactory struct{}

func (f *assemblyAIProviderFactory) NewProvider() vsserver.LiveProviderAdapter {
	return &assemblyAILiveBridge{inner: vskernel.NewAssemblyAILive()}
}

// assemblyAILiveBridge adapts the kernel's AssemblyAI Voice Agent provider to
// the server WebSocket adapter contract.
type assemblyAILiveBridge struct {
	inner *vskernel.AssemblyAILive
}

func (b *assemblyAILiveBridge) Connect(ctx context.Context, cfg vsserver.LiveConfigFrame) error {
	if cfg.APIKey == "" {
		return errors.New("voiceagent: no AssemblyAI API key configured for this deployment")
	}
	liveCfg := vskernel.LiveConfig{
		Provider:         ProviderAssemblyAI,
		ProfileID:        "realtime.assemblyai.voice-agent",
		Model:            cfg.Model,
		FallbackModel:    cfg.FallbackModel,
		APIKey:           cfg.APIKey,
		Voice:            cfg.Voice,
		FrameworkPrompt:  cfg.SystemPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Locale:           cfg.Locale,
		Speaker:          cfg.Speaker,
		Policies: vskernel.LivePolicies{
			EnableInputAudioTranscription:  true,
			EnableOutputAudioTranscription: true,
			ActivityDetection: vskernel.ActivityDetectionPolicy{
				Automatic:         cfg.Automatic,
				StartSensitivity:  vskernel.StartSensitivity(strings.ToLower(cfg.StartSensitivity)),
				EndSensitivity:    vskernel.EndSensitivity(strings.ToLower(cfg.EndSensitivity)),
				PrefixPaddingMs:   cfg.PrefixPaddingMs,
				SilenceDurationMs: cfg.SilenceDurationMs,
				ActivityHandling:  vskernel.ActivityHandling(strings.ToLower(cfg.ActivityHandling)),
				TurnCoverage:      vskernel.TurnCoverage(strings.ToLower(cfg.TurnCoverage)),
			},
		},
	}
	if err := b.inner.Connect(ctx, liveCfg); err != nil {
		slog.Warn("voiceagent: AssemblyAI Voice Agent connect failed", "err", err)
		return err
	}
	return nil
}

func (b *assemblyAILiveBridge) SendAudio(chunk []byte) error { return b.inner.SendAudio(chunk) }
func (b *assemblyAILiveBridge) SendAudioStreamEnd() error    { return b.inner.SendAudioStreamEnd() }
func (b *assemblyAILiveBridge) SendText(text string) error   { return b.inner.SendText(text) }
func (b *assemblyAILiveBridge) Close() error                 { return b.inner.Close() }
func (b *assemblyAILiveBridge) Name() string                 { return b.inner.Name() }
func (b *assemblyAILiveBridge) SupportsLiveKitTransport() bool {
	return true
}

func (b *assemblyAILiveBridge) UpdateInstructions(ctx context.Context, cfg vsserver.LiveConfigFrame) error {
	return b.inner.UpdateInstructions(ctx, vskernel.LiveConfig{
		Provider:         ProviderAssemblyAI,
		ProfileID:        "realtime.assemblyai.voice-agent",
		Model:            cfg.Model,
		Voice:            cfg.Voice,
		FrameworkPrompt:  cfg.SystemPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Locale:           cfg.Locale,
		Speaker:          cfg.Speaker,
	})
}

func (b *assemblyAILiveBridge) SendToolResponse(frame vsserver.ToolResponseFrame) error {
	return b.inner.SendToolResponse(vskernel.ToolResponse{
		ID:       frame.ID,
		Name:     frame.Name,
		Response: frame.Response,
	})
}

func (b *assemblyAILiveBridge) Receive(ctx context.Context) (*vsserver.LiveMessage, error) {
	msg, err := b.inner.Receive(ctx)
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, nil
	}
	return mapKernelLiveMessage(msg), nil
}

// ── Cascaded provider factory ───────────────────────────────────────────────

// cascadedProviderFactory produces CascadedProvider instances backed by the
// shared AI deps held on App. Each session gets its own provider so
// turn-state (buffer, conversation history, in-flight STT/LLM/TTS) is
// isolated between concurrent sessions. The STT router, agent flow, and
// TTS router themselves are stateless/thread-safe and are reused across
// sessions.
type cascadedProviderFactory struct {
	stt             vsserver.CascadedSTT
	speakerStreamer speaker.StreamingProvider
	agent           vsserver.CascadedAgent
	tts             vsserver.CascadedTTS
	cfg             *config.Config
}

func (f *cascadedProviderFactory) NewProvider() vsserver.LiveProviderAdapter {
	return vsserver.NewCascadedProvider(vsserver.CascadedDeps{
		STT:             f.stt,
		SpeakerStreamer: f.speakerStreamer,
		Agent:           f.agent,
		TTS:             f.tts,
		Config: vsserver.CascadedConfig{
			TTSFormat: firstNonEmpty(f.cfg.TTS.Format, "mp3"),
			TTSSpeed:  nonZeroFloat(f.cfg.TTS.Speed, 1.0),
		},
	})
}

// moshiStubFactory returns providers that report a clear "not implemented"
// error at Connect time. Used until M9b wires the real Moshi client.
type moshiStubFactory struct{}

func (moshiStubFactory) NewProvider() vsserver.LiveProviderAdapter {
	return &moshiStubProvider{}
}

type moshiStubProvider struct{}

func (moshiStubProvider) Connect(_ context.Context, _ vsserver.LiveConfigFrame) error {
	return errors.New("voiceagent: moshi provider is experimental_unavailable (pending M9b)")
}
func (moshiStubProvider) SendAudio([]byte) error    { return nil }
func (moshiStubProvider) SendAudioStreamEnd() error { return nil }
func (moshiStubProvider) SendText(string) error     { return nil }
func (moshiStubProvider) Close() error              { return nil }
func (moshiStubProvider) Name() string              { return "moshi-stub" }
func (moshiStubProvider) Receive(ctx context.Context) (*vsserver.LiveMessage, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func nonZeroFloat(v, fallback float64) float64 {
	if v > 0 {
		return v
	}
	return fallback
}

func normalizeVoiceAgentProvider(provider string) string {
	name := strings.ToLower(strings.TrimSpace(provider))
	name = strings.ReplaceAll(name, "_", "-")
	switch name {
	case "google", "gemini", "gemini-live", "google-live", "realtime.google.gemini-native-audio":
		return ProviderGemini
	case "deepgram", "deepgram-agent", "deepgram-live", "realtime.deepgram.voice-agent":
		return ProviderDeepgram
	case "assemblyai", "assembly-ai", "assemblyai-agent", "assemblyai-live", "realtime.assemblyai.voice-agent":
		return ProviderAssemblyAI
	case "openai", "openai-realtime", "openai-live", "realtime.openai.gpt-realtime-2":
		return ProviderOpenAI
	case "cascaded", "local-cascaded", "pipeline", "pipeline-fallback", "voice-agent-cascaded", "voice-agent-cascaded-pipeline":
		return ProviderCascaded
	default:
		return name
	}
}

// ── Deepgram Voice Agent provider factory + bridge ───────────────────────────

type deepgramProviderFactory struct {
	cfg *config.Config
}

func (f *deepgramProviderFactory) NewProvider() vsserver.LiveProviderAdapter {
	inner := vskernel.NewDeepgramLive()
	// Listen (Nova-3) and speak (Aura-2) are fixed Deepgram legs; the think LLM
	// is configurable. deepgram_think_* config selects provider/model and an
	// optional bring-your-own endpoint+credential; a non-Gemini
	// [voice_agent].model is reused as the think model for back-compat. A Gemini
	// model id is ignored so it can't pin a non-existent Deepgram think model.
	if f.cfg != nil {
		think := f.cfg.DeepgramThinkConfig()
		inner.ConfigureThink(think.Provider, think.Model, think.EndpointURL, think.APIKey)
	}
	return &deepgramLiveBridge{inner: inner}
}

// deepgramLiveBridge adapts the kernel's DeepgramLive provider to the narrow
// interface the WebSocket handler consumes. Same translation pattern as
// geminiLiveBridge / openaiLiveBridge.
type deepgramLiveBridge struct {
	inner *vskernel.DeepgramLive
}

func (b *deepgramLiveBridge) Connect(ctx context.Context, cfg vsserver.LiveConfigFrame) error {
	if cfg.APIKey == "" {
		return errors.New("voiceagent: no Deepgram API key configured for this deployment")
	}
	liveCfg := vskernel.LiveConfig{
		APIKey:           cfg.APIKey,
		Voice:            cfg.Voice,
		FrameworkPrompt:  cfg.SystemPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Locale:           cfg.Locale,
		Speaker:          cfg.Speaker,
	}
	if err := b.inner.Connect(ctx, liveCfg); err != nil {
		slog.Warn("voiceagent: Deepgram Voice Agent connect failed", "err", err)
		return err
	}
	return nil
}

func (b *deepgramLiveBridge) SendAudio(chunk []byte) error   { return b.inner.SendAudio(chunk) }
func (b *deepgramLiveBridge) SendAudioStreamEnd() error      { return b.inner.SendAudioStreamEnd() }
func (b *deepgramLiveBridge) SendText(text string) error     { return b.inner.SendText(text) }
func (b *deepgramLiveBridge) Close() error                   { return b.inner.Close() }
func (b *deepgramLiveBridge) Name() string                   { return b.inner.Name() }
func (b *deepgramLiveBridge) SupportsLiveKitTransport() bool { return true }

func (b *deepgramLiveBridge) UpdateInstructions(ctx context.Context, cfg vsserver.LiveConfigFrame) error {
	return b.inner.UpdateInstructions(ctx, vskernel.LiveConfig{
		FrameworkPrompt:  cfg.SystemPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Locale:           cfg.Locale,
		Voice:            cfg.Voice,
	})
}

func (b *deepgramLiveBridge) SendToolResponse(frame vsserver.ToolResponseFrame) error {
	return b.inner.SendToolResponse(vskernel.ToolResponse{
		ID:       frame.ID,
		Name:     frame.Name,
		Response: frame.Response,
	})
}

func (b *deepgramLiveBridge) Receive(ctx context.Context) (*vsserver.LiveMessage, error) {
	msg, err := b.inner.Receive(ctx)
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, nil
	}
	return mapKernelLiveMessage(msg), nil
}
