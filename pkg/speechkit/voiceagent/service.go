// Package voiceagent provides an embeddable Voice Agent service.
//
// Voice Agent is the realtime audio-to-audio mode: a duplex WebSocket
// session with the underlying live model (for example OpenAI Realtime,
// or a pipeline fallback) where the user and the agent take audio turns
// in sequence. Use this when the host needs brainstorming, support, or
// follow-up dialogue rather than a one-shot result.
//
// For tool registration, lifecycle hooks, and session memory, build on
// top of this with
// [github.com/kombifyio/SpeechKit/pkg/speechkit/agentkit].
//
// Construct an instance with [NewService], passing a provider and the strict-
// mode policy fields from the host config.
package voiceagent

import (
	"context"
	"errors"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// ErrMissingProvider is returned by [NewService] when [Options.Provider] is
// nil, and by every [Service] method called on a nil receiver or on a
// Service without a provider.
var ErrMissingProvider = errors.New("speechkit voiceagent: provider is required")

// Config carries the per-session settings a [Service] hands to its
// [Provider] on Start. Empty fields keep the provider's own configuration.
type Config struct {
	// ProviderProfileID is the public realtime profile to run, for example
	// "realtime.openai.gpt-realtime-2".
	ProviderProfileID string
	// Model is the provider-specific realtime model id.
	Model string
	// Locale is the BCP-47 dialogue locale, for example "de-DE".
	Locale string
	// Instruction is the system prompt the agent follows.
	Instruction string
}

// Callbacks are the host handlers a [Provider] invokes during a session.
// Every field is optional; nil handlers are skipped. Providers may call them
// from a background goroutine, so handlers must return quickly and guard
// their own state.
type Callbacks struct {
	// OnAudio receives a chunk of agent audio to play back; SpeechKit
	// providers deliver 24 kHz 16-bit little-endian mono PCM.
	OnAudio func([]byte)
	// OnText receives agent text (an answer or output transcript) to display.
	OnText func(string)
	// OnError receives session errors, including the failure that ends a
	// session.
	OnError func(error)
}

// Provider is the backend a [Service] delegates to. SpeechKit's in-process
// implementation lives in the voiceagent/local subpackage; hosts may supply
// their own.
type Provider interface {
	// StartVoiceAgent opens a session configured by the given Config and
	// reports events through the Callbacks until the session ends.
	StartVoiceAgent(context.Context, Config, Callbacks) error
	// StopVoiceAgent ends the session and returns its record.
	StopVoiceAgent(context.Context) (speechkit.VoiceAgentSession, error)
	// SendText injects a user text turn into the running session.
	SendText(context.Context, string) error
	// CurrentSession returns a snapshot of the running or most recent
	// session.
	CurrentSession(context.Context) (speechkit.VoiceAgentSession, error)
}

// Options are the inputs to [NewService]. Provider is required; Config and
// Callbacks are forwarded unchanged on every Start.
type Options struct {
	Config    Config
	Callbacks Callbacks
	Provider  Provider
}

// Service implements [speechkit.VoiceAgentService] by delegating to a
// [Provider] with a fixed [Config] and [Callbacks]. It keeps no session
// state of its own, and a nil *Service is safe to call: every method then
// reports [ErrMissingProvider].
type Service struct {
	config    Config
	callbacks Callbacks
	provider  Provider
}

var _ speechkit.VoiceAgentService = (*Service)(nil)

// NewService builds a [Service] from opts. It returns [ErrMissingProvider]
// when opts.Provider is nil.
func NewService(opts Options) (*Service, error) {
	if opts.Provider == nil {
		return nil, ErrMissingProvider
	}
	return &Service{
		config:    opts.Config,
		callbacks: opts.Callbacks,
		provider:  opts.Provider,
	}, nil
}

// Start opens a Voice Agent session through the provider with the
// configured [Config] and [Callbacks].
func (s *Service) Start(ctx context.Context) error {
	if s == nil || s.provider == nil {
		return ErrMissingProvider
	}
	return s.provider.StartVoiceAgent(ctx, s.config, s.callbacks)
}

// Stop ends the current session and returns its
// [speechkit.VoiceAgentSession] record.
func (s *Service) Stop(ctx context.Context) (speechkit.VoiceAgentSession, error) {
	if s == nil || s.provider == nil {
		return speechkit.VoiceAgentSession{}, ErrMissingProvider
	}
	return s.provider.StopVoiceAgent(ctx)
}

// SendText injects text as a user turn into the running session.
func (s *Service) SendText(ctx context.Context, text string) error {
	if s == nil || s.provider == nil {
		return ErrMissingProvider
	}
	return s.provider.SendText(ctx, text)
}

// CurrentSession returns the provider's snapshot of the running or most
// recent session.
func (s *Service) CurrentSession(ctx context.Context) (speechkit.VoiceAgentSession, error) {
	if s == nil || s.provider == nil {
		return speechkit.VoiceAgentSession{}, ErrMissingProvider
	}
	return s.provider.CurrentSession(ctx)
}
