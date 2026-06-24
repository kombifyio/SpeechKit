package voiceagent

import (
	"context"

	"github.com/kombifyio/SpeechKit/internal/voiceagent/cascaded"
	live "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// LocalVoiceAgentDeps bundles the kernel-level routers and flows the
// Device-Target wires into the local Voice Agent provider.
//
// Why a separate name from cascaded.Deps: a Device-Target caller cares
// about "I'm wiring the LOCAL provider", not about the underlying
// cascaded turn-based architecture. The latter is an implementation
// detail of the local path.
type LocalVoiceAgentDeps struct {
	STT             cascaded.STT
	Agent           cascaded.Agent
	TTS             cascaded.TTS
	SpeakerStreamer cascaded.SpeakerStreamer
	Config          cascaded.Config
}

// LocalVoiceAgentProvider is the Device-Target-side local Voice Agent
// provider. It wraps a cascaded.Provider and adapts its minimal
// SessionConfig / Message types to the public live.LiveConfig /
// live.LiveMessage interface so a Wails session can drive it the same
// way it drives Gemini Live.
type LocalVoiceAgentProvider struct {
	inner *cascaded.Provider
}

// NewLocalVoiceAgentProvider constructs the local provider. Panics if
// STT or Agent is nil — these are hard requirements; nothing in the
// Device-Target should ever construct this without them.
func NewLocalVoiceAgentProvider(deps LocalVoiceAgentDeps) *LocalVoiceAgentProvider {
	if deps.STT == nil {
		panic("voiceagent: LocalVoiceAgentDeps.STT is nil")
	}
	if deps.Agent == nil {
		panic("voiceagent: LocalVoiceAgentDeps.Agent is nil")
	}
	inner := cascaded.NewProvider(cascaded.Deps{
		STT:             deps.STT,
		Agent:           deps.Agent,
		TTS:             deps.TTS,
		SpeakerStreamer: deps.SpeakerStreamer,
		Config:          deps.Config,
	})
	return &LocalVoiceAgentProvider{inner: inner}
}

// Connect translates the rich live.LiveConfig into cascaded.SessionConfig.
func (p *LocalVoiceAgentProvider) Connect(ctx context.Context, cfg live.LiveConfig) error {
	return p.inner.Connect(ctx, cascaded.SessionConfig{
		Locale:           cfg.Locale,
		Voice:            cfg.Voice,
		SystemPrompt:     cfg.FrameworkPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Speaker:          cfg.Speaker,
	})
}

// SendAudio delegates.
func (p *LocalVoiceAgentProvider) SendAudio(chunk []byte) error {
	return p.inner.SendAudio(chunk)
}

// SendAudioStreamEnd delegates.
func (p *LocalVoiceAgentProvider) SendAudioStreamEnd() error {
	return p.inner.SendAudioStreamEnd()
}

// SendText delegates.
func (p *LocalVoiceAgentProvider) SendText(text string) error {
	return p.inner.SendText(text)
}

// SendToolResponse is a no-op. The local cascaded path is turn-based
// and does not surface tool calls upstream — clients that ask for
// tool calls fall back to the cloud Voice Agent.
func (p *LocalVoiceAgentProvider) SendToolResponse(_ live.ToolResponse) error {
	return nil
}

// Receive blocks for the next cascaded.Message and adapts it into a
// live.LiveMessage.
func (p *LocalVoiceAgentProvider) Receive(ctx context.Context) (*live.LiveMessage, error) {
	msg, err := p.inner.Receive(ctx)
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, nil
	}
	out := &live.LiveMessage{
		Audio:                  msg.Audio,
		InputTranscript:        msg.InputTranscript,
		InputTranscriptDone:    msg.InputTranscriptDone,
		InputSpeakerLabel:      msg.InputSpeakerLabel,
		InputPersonID:          msg.InputPersonID,
		InputDisplayName:       msg.InputDisplayName,
		InputSpeakerConfidence: msg.InputSpeakerConfidence,
		OutputTranscript:       msg.OutputTranscript,
		OutputTranscriptDone:   msg.OutputTranscriptDone,
		ProviderMetadata:       map[string]any{"provider_event": "cascaded.message"},
	}
	out.EventTypes = live.InferLiveEventTypes(out)
	if len(out.EventTypes) > 0 {
		out.EventType = out.EventTypes[0]
	}
	return out, nil
}

// Close delegates.
func (p *LocalVoiceAgentProvider) Close() error { return p.inner.Close() }

// Name overrides cascaded.Provider.Name() so observability tells the
// Device-Target-embedded path apart from the Server-Target wiring.
func (p *LocalVoiceAgentProvider) Name() string { return "local-cascaded" }

// UpdateInstructions translates rich live.LiveConfig to SessionConfig
// for mid-session prompt updates. Implementing this also satisfies
// live.LiveInstructionUpdater.
func (p *LocalVoiceAgentProvider) UpdateInstructions(ctx context.Context, cfg live.LiveConfig) error {
	return p.inner.UpdateInstructions(ctx, cascaded.SessionConfig{
		Locale:           cfg.Locale,
		Voice:            cfg.Voice,
		SystemPrompt:     cfg.FrameworkPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Speaker:          cfg.Speaker,
	})
}

// Compile-time interface checks.
var (
	_ live.LiveProvider           = (*LocalVoiceAgentProvider)(nil)
	_ live.LiveInstructionUpdater = (*LocalVoiceAgentProvider)(nil)
)
