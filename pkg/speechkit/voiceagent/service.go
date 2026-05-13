// Package voiceagent provides an embeddable Voice Agent service.
//
// Voice Agent is the realtime audio-to-audio mode: a duplex WebSocket
// session with the underlying live model (Gemini Live, OpenAI Realtime,
// or a pipeline fallback) where the user and the agent take audio turns
// in sequence. Use this when the host needs brainstorming, support, or
// follow-up dialogue rather than a one-shot result.
//
// For tool registration, lifecycle hooks, and session memory, build on
// top of this with
// [github.com/kombifyio/SpeechKit/pkg/speechkit/agentkit].
//
// Construct an instance with [New], passing a provider and the strict-
// mode policy fields from the host config.
package voiceagent

import (
	"context"
	"errors"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

var ErrMissingProvider = errors.New("speechkit voiceagent: provider is required")

type Config struct {
	ProviderProfileID string
	Model             string
	Locale            string
	Instruction       string
}

type Callbacks struct {
	OnAudio func([]byte)
	OnText  func(string)
	OnError func(error)
}

type Provider interface {
	StartVoiceAgent(context.Context, Config, Callbacks) error
	StopVoiceAgent(context.Context) (speechkit.VoiceAgentSession, error)
	SendText(context.Context, string) error
	CurrentSession(context.Context) (speechkit.VoiceAgentSession, error)
}

type Options struct {
	Config    Config
	Callbacks Callbacks
	Provider  Provider
}

type Service struct {
	config    Config
	callbacks Callbacks
	provider  Provider
}

var _ speechkit.VoiceAgentService = (*Service)(nil)

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

func (s *Service) Start(ctx context.Context) error {
	if s == nil || s.provider == nil {
		return ErrMissingProvider
	}
	return s.provider.StartVoiceAgent(ctx, s.config, s.callbacks)
}

func (s *Service) Stop(ctx context.Context) (speechkit.VoiceAgentSession, error) {
	if s == nil || s.provider == nil {
		return speechkit.VoiceAgentSession{}, ErrMissingProvider
	}
	return s.provider.StopVoiceAgent(ctx)
}

func (s *Service) SendText(ctx context.Context, text string) error {
	if s == nil || s.provider == nil {
		return ErrMissingProvider
	}
	return s.provider.SendText(ctx, text)
}

func (s *Service) CurrentSession(ctx context.Context) (speechkit.VoiceAgentSession, error) {
	if s == nil || s.provider == nil {
		return speechkit.VoiceAgentSession{}, ErrMissingProvider
	}
	return s.provider.CurrentSession(ctx)
}
