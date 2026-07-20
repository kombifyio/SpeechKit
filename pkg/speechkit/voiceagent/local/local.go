// Package local implements [voiceagent.Provider] on top of an in-process
// live session — realtime voice agents (Deepgram Voice Agent, Gemini Live,
// OpenAI Realtime, AssemblyAI, cascaded) without a speechkit-server.
//
// It is the composition seam between the embeddable [voiceagent.Service]
// (what companion.HandsFree TargetVoiceAgent consumes) and the realtime
// runtime in [live] + [agentkit]: tool definitions from the registry are
// announced to the provider (e.g. Deepgram agent.think.functions), tool
// calls are executed client-side and answered via SendToolResponse, and
// final input/output transcripts accumulate into the returned
// [speechkit.VoiceAgentSession] record.
//
// Hosts that own audio I/O (the kombify box companion, future satellites)
// feed microphone PCM through [Provider.SendAudio] and play back agent
// audio via Options.Callbacks.OnAudio.
package local

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

var (
	ErrSessionActive   = errors.New("speechkit voiceagent/local: a session is already active")
	ErrNoActiveSession = errors.New("speechkit voiceagent/local: no active session")
)

// RuntimeKindLocal marks session records produced by this provider.
const RuntimeKindLocal = "local"

// ProviderFactory builds a live provider for a normalized config. The
// default is [live.NewProviderForConfig]; tests inject fakes here.
type ProviderFactory func(live.LiveConfig) (live.LiveProvider, live.LiveConfig, error)

// Options configure the local provider. All fields are optional except
// Live, which must at least name the realtime provider (or profile) to use.
type Options struct {
	// Live is the base realtime configuration (provider, model, voice,
	// locale, API key, framework prompt, policies, provider options).
	// StartVoiceAgent merges the per-session [voiceagent.Config] on top.
	Live live.LiveConfig
	// Idle bounds session lifetime; zero values fall back to
	// [live.DefaultIdleConfig]. Hosts paying per connection minute
	// (Deepgram Voice Agent) should set these aggressively.
	Idle live.IdleConfig
	// Tools are executed client-side when the agent calls them; their
	// definitions are announced to the provider on session start.
	Tools *agentkit.ToolRegistry
	// Hooks observe session lifecycle (user/agent messages, tool calls).
	Hooks agentkit.LifecycleHooks
	// Memory is the session-scoped store handed to tools; defaults to the
	// in-memory implementation.
	Memory agentkit.Memory
	// Callbacks are the rich host callbacks (audio playback, barge-in,
	// transcripts, state changes). The thin [voiceagent.Callbacks] passed
	// by the service are invoked in addition, never instead.
	Callbacks live.Callbacks
	// Factory overrides live-provider construction; nil uses
	// [live.NewProviderForConfig].
	Factory ProviderFactory
}

// Provider adapts an agentkit.AgentSession to [voiceagent.Provider].
// Methods are safe for concurrent use; only one session is active at a
// time (matching live.Session semantics).
type Provider struct {
	opts Options

	mu      sync.Mutex
	session *agentkit.AgentSession
	active  bool
	record  speechkit.VoiceAgentSession
	turns   []speechkit.VoiceAgentTurn
}

var _ voiceagent.Provider = (*Provider)(nil)

func New(opts Options) (*Provider, error) {
	if opts.Factory == nil {
		opts.Factory = live.NewProviderForConfig
	}
	if opts.Idle.ReminderAfter <= 0 && opts.Idle.DeactivateAfter <= 0 {
		opts.Idle = live.DefaultIdleConfig()
	}
	return &Provider{opts: opts}, nil
}

// StartVoiceAgent opens a live session. cfg fields override the base
// Options.Live where set: Model, Locale, ProviderProfileID → ProfileID,
// Instruction → FrameworkPrompt.
func (p *Provider) StartVoiceAgent(ctx context.Context, cfg voiceagent.Config, cbs voiceagent.Callbacks) error {
	if p == nil {
		return ErrNoActiveSession
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active {
		return ErrSessionActive
	}

	merged := p.opts.Live
	if cfg.Model != "" {
		merged.Model = cfg.Model
	}
	if cfg.Locale != "" {
		merged.Locale = cfg.Locale
	}
	if cfg.ProviderProfileID != "" {
		merged.ProfileID = cfg.ProviderProfileID
	}
	if cfg.Instruction != "" {
		merged.FrameworkPrompt = cfg.Instruction
	}

	liveProvider, normalized, err := p.opts.Factory(merged)
	if err != nil {
		return err
	}

	//nolint:contextcheck // agentkit dispatches tool goroutines on the session
	// context installed by session.Start(ctx) directly below; NewAgentSession
	// itself takes no context by design.
	session := agentkit.NewAgentSession(
		liveProvider,
		p.sessionCallbacks(cbs),
		p.opts.Tools,
		p.recordingHooks(),
		p.opts.Memory,
	)

	if err := session.Start(ctx, normalized, p.opts.Idle); err != nil {
		return err
	}

	p.session = session
	p.active = true
	p.turns = nil
	p.record = speechkit.VoiceAgentSession{
		ID:                session.SessionID(),
		StartedAt:         time.Now(),
		Locale:            normalized.Locale,
		ProviderProfileID: normalized.ProfileID,
		RuntimeKind:       RuntimeKindLocal,
	}
	return nil
}

// StopVoiceAgent ends the session and returns the accumulated record.
func (p *Provider) StopVoiceAgent(context.Context) (speechkit.VoiceAgentSession, error) {
	if p == nil {
		return speechkit.VoiceAgentSession{}, ErrNoActiveSession
	}
	p.mu.Lock()
	session := p.session
	p.mu.Unlock()
	if session == nil {
		return speechkit.VoiceAgentSession{}, ErrNoActiveSession
	}

	// Stop outside the lock: live.Session.Stop synchronizes with its
	// receive loop, whose callbacks re-enter p.mu via markEnded/appendTurn.
	session.Stop()

	p.mu.Lock()
	defer p.mu.Unlock()
	p.markEndedLocked()
	p.session = nil
	return p.snapshotLocked(), nil
}

// SendText injects a user text turn into the active session.
func (p *Provider) SendText(_ context.Context, text string) error {
	session, err := p.activeSession()
	if err != nil {
		return err
	}
	return session.SendText(text)
}

// CurrentSession returns a snapshot of the running (or last) session record.
func (p *Provider) CurrentSession(context.Context) (speechkit.VoiceAgentSession, error) {
	if p == nil {
		return speechkit.VoiceAgentSession{}, ErrNoActiveSession
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.record.ID == "" {
		return speechkit.VoiceAgentSession{}, ErrNoActiveSession
	}
	return p.snapshotLocked(), nil
}

// SendAudio forwards a 16 kHz S16LE mono PCM chunk from the host
// microphone into the session. It exceeds the voiceagent.Provider
// interface: audio-owning hosts (the box companion) call it directly.
func (p *Provider) SendAudio(chunk []byte) error {
	session, err := p.activeSession()
	if err != nil {
		return err
	}
	return session.SendAudio(chunk)
}

// EndAudioStream signals the end of the current microphone stream.
func (p *Provider) EndAudioStream() error {
	session, err := p.activeSession()
	if err != nil {
		return err
	}
	return session.EndAudioStream()
}

// State exposes the underlying live session state (inactive when no
// session is active).
func (p *Provider) State() live.State {
	if p == nil {
		return live.StateInactive
	}
	p.mu.Lock()
	session := p.session
	p.mu.Unlock()
	if session == nil {
		return live.StateInactive
	}
	return session.State()
}

func (p *Provider) activeSession() (*agentkit.AgentSession, error) {
	if p == nil {
		return nil, ErrNoActiveSession
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.active || p.session == nil {
		return nil, ErrNoActiveSession
	}
	return p.session, nil
}

// sessionCallbacks chains the rich host callbacks with the thin service
// callbacks and the provider's own bookkeeping.
func (p *Provider) sessionCallbacks(thin voiceagent.Callbacks) live.Callbacks {
	out := p.opts.Callbacks

	origAudio := out.OnAudio
	out.OnAudio = func(audio []byte) {
		if origAudio != nil {
			origAudio(audio)
		}
		if thin.OnAudio != nil {
			thin.OnAudio(audio)
		}
	}

	origText := out.OnText
	out.OnText = func(text string) {
		if origText != nil {
			origText(text)
		}
		if thin.OnText != nil {
			thin.OnText(text)
		}
	}

	origErr := out.OnError
	out.OnError = func(err error) {
		if origErr != nil {
			origErr(err)
		}
		if thin.OnError != nil {
			thin.OnError(err)
		}
	}

	origEnd := out.OnSessionEnd
	out.OnSessionEnd = func() {
		if origEnd != nil {
			origEnd()
		}
		p.markEnded()
	}

	return out
}

// recordingHooks chains the host lifecycle hooks with turn accumulation.
// agentkit already buffers partial transcripts and flushes complete
// messages into OnUserMessage/OnAgentMessage — exactly turn granularity.
func (p *Provider) recordingHooks() agentkit.LifecycleHooks {
	hooks := p.opts.Hooks

	origUser := hooks.OnUserMessage
	hooks.OnUserMessage = func(ctx context.Context, sc agentkit.SessionContext, text string) {
		p.appendTurn("user", text)
		if origUser != nil {
			origUser(ctx, sc, text)
		}
	}

	origAgent := hooks.OnAgentMessage
	hooks.OnAgentMessage = func(ctx context.Context, sc agentkit.SessionContext, text string) {
		p.appendTurn("assistant", text)
		if origAgent != nil {
			origAgent(ctx, sc, text)
		}
	}

	return hooks
}

func (p *Provider) appendTurn(role, text string) {
	if text == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.turns = append(p.turns, speechkit.VoiceAgentTurn{
		Role:      role,
		Text:      text,
		CreatedAt: time.Now(),
	})
}

func (p *Provider) markEnded() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.markEndedLocked()
}

func (p *Provider) markEndedLocked() {
	if !p.active {
		return
	}
	p.active = false
	p.record.EndedAt = time.Now()
}

func (p *Provider) snapshotLocked() speechkit.VoiceAgentSession {
	out := p.record
	out.Turns = append([]speechkit.VoiceAgentTurn(nil), p.turns...)
	return out
}
