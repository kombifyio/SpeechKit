package cascaded

import (
	"context"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// liveProviderEvent is the provider_event attribution every message the
// LiveProvider relays carries in its ProviderMetadata.
const liveProviderEvent = "cascaded.message"

// LiveProvider adapts the turn-based [Provider] to the realtime
// [live.LiveProvider] contract so a [live.Session] can drive the local
// STT -> LLM -> TTS pipeline exactly the way it drives a native realtime
// provider. It also implements [live.LiveInstructionUpdater] so hosts can
// push mid-session prompt updates.
//
// Connect and UpdateInstructions map the rich [live.LiveConfig] onto the
// minimal [SessionConfig] (Locale, Voice, FrameworkPrompt -> SystemPrompt,
// RefinementPrompt, Speaker). Receive wraps every [Message] in a
// [live.LiveMessage] normalized by [live.NormalizeMessageEvents] with the
// provider event "cascaded.message".
type LiveProvider struct {
	inner *Provider
}

// NewLiveProvider constructs a LiveProvider around a new [Provider] built
// from deps. Dependencies are not validated here: Connect reports a missing
// STT or Agent through [ErrNotConfigured], and a nil TTS keeps the session
// text-only, matching [NewProvider].
func NewLiveProvider(deps Deps) *LiveProvider {
	return &LiveProvider{inner: NewProvider(deps)}
}

// Connect translates cfg into a [SessionConfig] and starts the turn
// processor. It returns an error wrapping [ErrNotConfigured] when STT or
// Agent was not supplied.
func (p *LiveProvider) Connect(ctx context.Context, cfg live.LiveConfig) error {
	return p.inner.Connect(ctx, sessionConfigFromLive(cfg))
}

// UpdateInstructions translates cfg into a [SessionConfig] for a
// mid-session prompt update. Implementing it satisfies
// [live.LiveInstructionUpdater].
func (p *LiveProvider) UpdateInstructions(ctx context.Context, cfg live.LiveConfig) error {
	return p.inner.UpdateInstructions(ctx, sessionConfigFromLive(cfg))
}

// SendAudio appends a PCM chunk to the current turn buffer.
func (p *LiveProvider) SendAudio(chunk []byte) error { return p.inner.SendAudio(chunk) }

// SendAudioStreamEnd commits the current turn buffer as a complete turn.
func (p *LiveProvider) SendAudioStreamEnd() error { return p.inner.SendAudioStreamEnd() }

// SendText injects a text turn, skipping STT.
func (p *LiveProvider) SendText(text string) error { return p.inner.SendText(text) }

// SendToolResponse is a documented no-op. The cascaded path is turn-based
// and never surfaces tool calls, so there is never a call to answer; hosts
// that need tool calling use a realtime provider instead.
func (p *LiveProvider) SendToolResponse(live.ToolResponse) error { return nil }

// Receive blocks for the next [Message] and adapts it into a
// [live.LiveMessage] carrying normalized event types and the
// "cascaded.message" provider attribution.
func (p *LiveProvider) Receive(ctx context.Context) (*live.LiveMessage, error) {
	msg, err := p.inner.Receive(ctx)
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, nil
	}
	return live.NormalizeMessageEvents(&live.LiveMessage{
		Audio:                  msg.Audio,
		InputTranscript:        msg.InputTranscript,
		InputTranscriptDone:    msg.InputTranscriptDone,
		InputSpeakerLabel:      msg.InputSpeakerLabel,
		InputPersonID:          msg.InputPersonID,
		InputDisplayName:       msg.InputDisplayName,
		InputSpeakerConfidence: msg.InputSpeakerConfidence,
		OutputTranscript:       msg.OutputTranscript,
		OutputTranscriptDone:   msg.OutputTranscriptDone,
	}, liveProviderEvent), nil
}

// Close stops the underlying [Provider]. It is idempotent.
func (p *LiveProvider) Close() error { return p.inner.Close() }

// Name identifies the adapter in logs and observability. It deliberately
// differs from [Provider.Name] so an in-process cascaded session is
// distinguishable from a speechkit-server-hosted one.
func (p *LiveProvider) Name() string { return "local-cascaded" }

func sessionConfigFromLive(cfg live.LiveConfig) SessionConfig {
	return SessionConfig{
		Locale:           cfg.Locale,
		Voice:            cfg.Voice,
		SystemPrompt:     cfg.FrameworkPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Speaker:          cfg.Speaker,
	}
}

// Compile-time interface checks.
var (
	_ live.LiveProvider           = (*LiveProvider)(nil)
	_ live.LiveInstructionUpdater = (*LiveProvider)(nil)
)
