//go:build linux

package core

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/persona"
	vsserver "github.com/kombifyio/SpeechKit/internal/server/voiceagent"
	vskernel "github.com/kombifyio/SpeechKit/internal/voiceagent"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

// Supported provider strings for cfg.VoiceAgent.Provider.
const (
	ProviderGemini   = "gemini"
	ProviderOpenAI   = "openai"
	ProviderCascaded = "cascaded"
	ProviderMoshi    = "moshi"
)

// buildVoiceAgentHandler wires the server-target Voice Agent handler,
// dispatching between Gemini Live, Cascaded, and Moshi providers based on
// cfg.VoiceAgent.Provider.
//
// /readyz surfaces the selected provider's state; the handler is always
// mounted so POST /v1/voiceagent/sessions works even when the provider
// itself is degraded (clients get a `provider_connect_failed` error at WS
// upgrade rather than a 404 at session creation — operators can then read
// /readyz to see why).
func buildVoiceAgentHandler(ctx context.Context, cfg *config.Config, app *App) (*vsserver.Handler, string, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.VoiceAgent.Provider))
	if provider == "" {
		provider = ProviderGemini
	}

	factory, status, err := buildProviderFactory(ctx, cfg, app, provider)
	if err != nil {
		return nil, status, err
	}

	manager, err := vsserver.NewSessionManager(vsserver.Options{
		TicketTTL:              0,
		MaxGlobalSessions:      cfg.Server.MaxVoiceAgentSessions,
		MaxPerIdentitySessions: cfg.Server.MaxSessionsPerUser,
	})
	if err != nil {
		return nil, status, err
	}

	resolver := &personaResolver{
		cfg:      cfg,
		apiKey:   resolveRealtimeAPIKey(cfg, provider),
		registry: app.PersonaRegistry,
	}

	idleTimeout := time.Duration(cfg.Server.VoiceAgentIdleTimeoutSec) * time.Second
	if cfg.Server.VoiceAgentIdleTimeoutSec < 0 {
		// Negative value disables the watchdog explicitly. Pass through
		// untouched; vsserver.New treats negatives as "disabled".
		idleTimeout = -1
	}

	h, err := vsserver.New(vsserver.HandlerOptions{
		Manager:        manager,
		Provider:       factory,
		Persona:        resolver,
		PublicURL:      cfg.Server.PublicURL,
		AllowedOrigins: cfg.Server.CORSAllowedOrigins,
		IdleTimeout:    idleTimeout,
		Store:          app.Store,
		LiveKit:        buildLiveKitIssuer(cfg, app),
	})
	if err != nil {
		return nil, status, err
	}
	return h, status, nil
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
	switch provider {
	case ProviderGemini:
		apiKey := strings.TrimSpace(config.ResolveSecret(cfg.Providers.Google.APIKeyEnv))
		status := "ready (gemini)"
		if apiKey == "" {
			status = "degraded: no Google API key; Gemini Live sessions will fail at upgrade"
		}
		return &geminiProviderFactory{}, status, nil

	case ProviderOpenAI:
		apiKey := strings.TrimSpace(config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv))
		status := "ready (openai)"
		if apiKey == "" {
			status = "degraded: no OpenAI API key; gpt-realtime sessions will fail at upgrade"
		}
		return &openaiProviderFactory{}, status, nil

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
			stt:   app.STTRouter,
			agent: vsserver.NewAgentFlowAdapter(app.AgentFlow),
			tts:   ttsImpl,
			cfg:   cfg,
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
	apiKey   string
	registry *persona.Registry
}

func (r *personaResolver) Resolve(start vsserver.StartFrame) (vsserver.LiveConfigFrame, error) {
	return r.resolve(start, 0)
}

func (r *personaResolver) ResolveStep(start vsserver.StartFrame, stepIndex int) (vsserver.LiveConfigFrame, error) {
	return r.resolve(start, stepIndex)
}

func (r *personaResolver) resolve(start vsserver.StartFrame, stepIndex int) (vsserver.LiveConfigFrame, error) {
	va := r.cfg.VoiceAgent

	frame := vsserver.LiveConfigFrame{
		Model:             firstNonEmpty(start.Model, va.Model),
		FallbackModel:     va.FallbackModel,
		APIKey:            r.apiKey,
		Voice:             firstNonEmpty(start.Voice, va.Voice),
		SystemPrompt:      firstNonEmpty(start.SystemPromptOverride, va.FrameworkPrompt),
		RefinementPrompt:  va.RefinementPrompt,
		Locale:            firstNonEmpty(start.Locale, r.cfg.General.Language, "en"),
		Automatic:         va.AutomaticActivityDetection,
		StartSensitivity:  va.VADStartSensitivity,
		EndSensitivity:    va.VADEndSensitivity,
		PrefixPaddingMs:   int32(va.VADPrefixPaddingMs),
		SilenceDurationMs: int32(va.VADSilenceDurationMs),
		ActivityHandling:  va.ActivityHandling,
		TurnCoverage:      va.TurnCoverage,
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
// it produces carries the right credential downstream — Gemini Live, OpenAI
// Realtime, and the Cascaded pipeline have distinct env vars.
func resolveRealtimeAPIKey(cfg *config.Config, provider string) string {
	switch provider {
	case ProviderOpenAI:
		return strings.TrimSpace(config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv))
	default:
		return strings.TrimSpace(config.ResolveSecret(cfg.Providers.Google.APIKeyEnv))
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
	return &vsserver.LiveMessage{
		Audio:                msg.Audio,
		InputTranscript:      msg.InputTranscript,
		InputTranscriptDone:  msg.InputTranscriptDone,
		OutputTranscript:     msg.OutputTranscript,
		OutputTranscriptDone: msg.OutputTranscriptDone,
		ToolCalls:            mapKernelToolCalls(msg.ToolCalls),
		Interrupted:          msg.Interrupted,
		GoAway:               msg.GoAway,
	}, nil
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
	return &vsserver.LiveMessage{
		Audio:                msg.Audio,
		InputTranscript:      msg.InputTranscript,
		InputTranscriptDone:  msg.InputTranscriptDone,
		OutputTranscript:     msg.OutputTranscript,
		OutputTranscriptDone: msg.OutputTranscriptDone,
		ToolCalls:            mapKernelToolCalls(msg.ToolCalls),
		Interrupted:          msg.Interrupted,
		GoAway:               msg.GoAway,
	}, nil
}

// ── Cascaded provider factory ───────────────────────────────────────────────

// cascadedProviderFactory produces CascadedProvider instances backed by the
// shared AI deps held on App. Each session gets its own provider so
// turn-state (buffer, conversation history, in-flight STT/LLM/TTS) is
// isolated between concurrent sessions. The STT router, agent flow, and
// TTS router themselves are stateless/thread-safe and are reused across
// sessions.
type cascadedProviderFactory struct {
	stt   vsserver.CascadedSTT
	agent vsserver.CascadedAgent
	tts   vsserver.CascadedTTS
	cfg   *config.Config
}

func (f *cascadedProviderFactory) NewProvider() vsserver.LiveProviderAdapter {
	return vsserver.NewCascadedProvider(vsserver.CascadedDeps{
		STT:   f.stt,
		Agent: f.agent,
		TTS:   f.tts,
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
