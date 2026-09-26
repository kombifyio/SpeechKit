//go:build linux

package core

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	vsserver "github.com/kombifyio/SpeechKit/internal/server/voiceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/assemblyai"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/deepgram"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/openai"
)

func mapKernelLiveMessage(msg *live.LiveMessage) *vsserver.LiveMessage {
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
// bridge) onto the kernel's live ToolDefinition type. Used by live bridges
// with tool support; cascaded has no tool support and skips it.
func kernelToolDefinitions(defs []vsserver.ToolDefinitionFrame) []live.ToolDefinition {
	if len(defs) == 0 {
		return nil
	}
	out := make([]live.ToolDefinition, 0, len(defs))
	for _, def := range defs {
		out = append(out, live.ToolDefinition{
			Name:                 def.Name,
			Description:          def.Description,
			ParametersJSONSchema: def.ParametersJSONSchema,
			Behavior:             live.ToolBehavior(def.Behavior),
		})
	}
	return out
}

func mapKernelToolCalls(calls []live.ToolCall) []vsserver.ToolCall {
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
	return &openaiLiveBridge{inner: openai.New()}
}

// openaiLiveBridge adapts the public openai.Provider to the narrow
// interface the WebSocket handler consumes. Kernel enum types are rebuilt from
// string fields on vsserver.LiveConfigFrame.
type openaiLiveBridge struct {
	inner *openai.Provider
}

func (b *openaiLiveBridge) Connect(ctx context.Context, cfg vsserver.LiveConfigFrame) error {
	if cfg.APIKey == "" {
		return errors.New("voiceagent: no OpenAI API key configured for this deployment")
	}
	liveCfg := live.LiveConfig{
		Model:            cfg.Model,
		FallbackModel:    cfg.FallbackModel,
		APIKey:           cfg.APIKey,
		Voice:            cfg.Voice,
		FrameworkPrompt:  cfg.SystemPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Locale:           cfg.Locale,
		Speaker:          cfg.Speaker,
		Tools:            kernelToolDefinitions(cfg.Tools),
		Policies: live.LivePolicies{
			EnableInputAudioTranscription:  true,
			EnableOutputAudioTranscription: true,
			ActivityDetection: live.ActivityDetectionPolicy{
				Automatic:         cfg.Automatic,
				StartSensitivity:  live.StartSensitivity(strings.ToLower(cfg.StartSensitivity)),
				EndSensitivity:    live.EndSensitivity(strings.ToLower(cfg.EndSensitivity)),
				PrefixPaddingMs:   cfg.PrefixPaddingMs,
				SilenceDurationMs: cfg.SilenceDurationMs,
				ActivityHandling:  live.ActivityHandling(strings.ToLower(cfg.ActivityHandling)),
				TurnCoverage:      live.TurnCoverage(strings.ToLower(cfg.TurnCoverage)),
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
	liveCfg := live.LiveConfig{
		Model:            cfg.Model,
		Voice:            cfg.Voice,
		FrameworkPrompt:  cfg.SystemPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Locale:           cfg.Locale,
		Speaker:          cfg.Speaker,
	}
	return b.inner.UpdateInstructions(ctx, liveCfg)
}

// CancelResponse forwards the client's tap-to-interrupt to the Realtime
// `response.cancel` event. Only the OpenAI bridge implements
// vsserver.LiveResponseCanceller: Gemini Live, Deepgram Voice Agent, and
// AssemblyAI Voice Agent expose no client-side cancel message (interruption
// there is speech-driven server-side), so those sessions rely on the
// adapter's downlink suppression until the current turn ends.
func (b *openaiLiveBridge) CancelResponse() error { return b.inner.CancelResponse() }

func (b *openaiLiveBridge) SendToolResponse(frame vsserver.ToolResponseFrame) error {
	return b.inner.SendToolResponse(live.ToolResponse{
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
	return &assemblyAILiveBridge{inner: assemblyai.New()}
}

// assemblyAILiveBridge adapts the kernel's AssemblyAI Voice Agent provider to
// the server WebSocket adapter contract. Note: cfg.Model is forwarded for
// contract uniformity only — the AssemblyAI Voice Agents WS API session config
// carries no model/LLM field (agent_id binds a stored server-side agent
// instead), so the kernel provider intentionally does not send it; see
// assemblyAISessionUpdate in pkg/speechkit/voiceagent/live.
type assemblyAILiveBridge struct {
	inner *assemblyai.Provider
}

func (b *assemblyAILiveBridge) Connect(ctx context.Context, cfg vsserver.LiveConfigFrame) error {
	if cfg.APIKey == "" {
		return errors.New("voiceagent: no AssemblyAI API key configured for this deployment")
	}
	liveCfg := live.LiveConfig{
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
		Policies: live.LivePolicies{
			EnableInputAudioTranscription:  true,
			EnableOutputAudioTranscription: true,
			ActivityDetection: live.ActivityDetectionPolicy{
				Automatic:         cfg.Automatic,
				StartSensitivity:  live.StartSensitivity(strings.ToLower(cfg.StartSensitivity)),
				EndSensitivity:    live.EndSensitivity(strings.ToLower(cfg.EndSensitivity)),
				PrefixPaddingMs:   cfg.PrefixPaddingMs,
				SilenceDurationMs: cfg.SilenceDurationMs,
				ActivityHandling:  live.ActivityHandling(strings.ToLower(cfg.ActivityHandling)),
				TurnCoverage:      live.TurnCoverage(strings.ToLower(cfg.TurnCoverage)),
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
	return b.inner.UpdateInstructions(ctx, live.LiveConfig{
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
	return b.inner.SendToolResponse(live.ToolResponse{
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

type registeredAgentProviderFactory struct {
	deps vsserver.CascadedDeps
}

func (f *registeredAgentProviderFactory) NewProvider() vsserver.LiveProviderAdapter {
	return vsserver.NewRegisteredAgentProvider(f.deps, func() string {
		return os.Getenv("KOMBIFY_A2A_DELEGATION_SIGNING_SECRET")
	})
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
	// Canonical alias table lives in internal/config so the serving wiring,
	// catalog readiness, and the WebSocket adapter cannot drift.
	return config.NormalizeVoiceAgentProviderName(provider)
}

// ── Deepgram Voice Agent provider factory + bridge ───────────────────────────

type deepgramProviderFactory struct {
	cfg *config.Config
}

func (f *deepgramProviderFactory) NewProvider() vsserver.LiveProviderAdapter {
	inner := deepgram.New()
	// All three legs are configurable. deepgram_think_* selects the think
	// provider/model and an optional bring-your-own endpoint+credential; a
	// non-Gemini [voice_agent].model is reused as the think model for back-compat
	// (a Gemini model id is ignored so it can't pin a non-existent Deepgram think
	// model). deepgram_listen_*/deepgram_speak_* — or the catalog's "listen+speak"
	// composite — select the audio legs and Flux turn-detection tuning.
	if f.cfg != nil {
		think := f.cfg.DeepgramThinkConfig()
		inner.ConfigureThink(think.Provider, think.Model, think.EndpointURL, think.APIKey)
		audio := f.cfg.DeepgramAudioConfig()
		inner.ConfigureAudio(deepgram.AudioSettings{
			ListenModel:       audio.ListenModel,
			SpeakModel:        audio.SpeakModel,
			SpeakSpeed:        audio.SpeakSpeed,
			EOTThreshold:      audio.EOTThreshold,
			EagerEOTThreshold: audio.EagerEOTThreshold,
			EOTTimeoutMs:      audio.EOTTimeoutMs,
		})
	}
	return &deepgramLiveBridge{inner: inner}
}

// deepgramLiveBridge adapts the public deepgram.Provider to the narrow
// interface the WebSocket handler consumes. Same translation pattern as
// geminiLiveBridge / openaiLiveBridge.
type deepgramLiveBridge struct {
	inner *deepgram.Provider
}

func (b *deepgramLiveBridge) Connect(ctx context.Context, cfg vsserver.LiveConfigFrame) error {
	if cfg.APIKey == "" {
		return errors.New("voiceagent: no Deepgram API key configured for this deployment")
	}
	liveCfg := live.LiveConfig{
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
	return b.inner.UpdateInstructions(ctx, live.LiveConfig{
		FrameworkPrompt:  cfg.SystemPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Locale:           cfg.Locale,
		Voice:            cfg.Voice,
	})
}

func (b *deepgramLiveBridge) SendToolResponse(frame vsserver.ToolResponseFrame) error {
	return b.inner.SendToolResponse(live.ToolResponse{
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
