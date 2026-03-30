// Package voiceagent implements the Voice Agent Mode — a real-time,
// bidirectional voice conversation using native audio-to-audio models
// (Gemini Live API, OpenAI Realtime API) over WebSocket.
package voiceagent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
)

// State represents the current state of the Voice Agent session.
type State string

const (
	StateInactive     State = "inactive"
	StateConnecting   State = "connecting"
	StateListening    State = "listening"
	StateProcessing   State = "processing"
	StateSpeaking     State = "speaking"
	StateDeactivating State = "deactivating"
)

// LiveProvider abstracts a real-time audio-to-audio model connection.
type LiveProvider interface {
	// Connect establishes a WebSocket session to the real-time model.
	Connect(ctx context.Context, cfg LiveConfig) error

	// SendAudio streams PCM audio chunks to the model.
	// Format: 16-bit signed int, little-endian, mono, 16kHz.
	SendAudio(chunk []byte) error

	// Receive blocks until the next server message arrives.
	// Returns audio chunks and/or text from the model.
	Receive(ctx context.Context) (*LiveMessage, error)

	// SendText injects a text prompt into the session (for idle reminders).
	SendText(text string) error

	// Close terminates the WebSocket session.
	Close() error

	// Name returns the provider identifier.
	Name() string
}

// LiveConfig configures a real-time session.
type LiveConfig struct {
	Model        string // e.g. "gemini-3.1-flash-live-preview"
	APIKey       string
	Voice        string // Voice name
	SystemPrompt string
	Locale       string
}

// LiveMessage is a message received from the real-time model.
type LiveMessage struct {
	Audio []byte // PCM audio chunk (24kHz 16-bit mono)
	Text  string // Text transcript (may be partial or empty)
	Done  bool   // True when the model's turn is complete
}

// Callbacks are event handlers for UI integration.
type Callbacks struct {
	OnStateChange func(state State)
	OnAudio       func(audio []byte)  // Audio chunk to play
	OnText        func(text string)   // Text for display (speech bubble)
	OnError       func(err error)
}

// Session manages a Voice Agent conversation.
type Session struct {
	mu        sync.Mutex
	state     atomic.Value // State
	provider  LiveProvider
	callbacks Callbacks
	idleTimer *IdleTimer
	cancelFn  context.CancelFunc
	locale    string
}

// NewSession creates a Voice Agent session with the given provider.
func NewSession(provider LiveProvider, callbacks Callbacks) *Session {
	s := &Session{
		provider:  provider,
		callbacks: callbacks,
	}
	s.state.Store(StateInactive)
	return s
}

// Start activates the Voice Agent session.
func (s *Session) Start(ctx context.Context, cfg LiveConfig, idleCfg IdleConfig) error {
	s.mu.Lock()
	if s.currentState() != StateInactive {
		s.mu.Unlock()
		return fmt.Errorf("voiceagent: session already active (state: %s)", s.currentState())
	}
	s.setState(StateConnecting)
	s.locale = cfg.Locale

	sessionCtx, cancel := context.WithCancel(ctx)
	s.cancelFn = cancel
	s.mu.Unlock()

	if err := s.provider.Connect(sessionCtx, cfg); err != nil {
		s.setState(StateInactive)
		cancel()
		return fmt.Errorf("voiceagent: connect failed: %w", err)
	}

	// Start idle timer.
	s.mu.Lock()
	s.idleTimer = NewIdleTimer(idleCfg, s)
	s.mu.Unlock()

	// Start receive loop in background.
	go s.receiveLoop(sessionCtx)

	s.setState(StateListening)
	s.idleTimer.Reset()

	log.Printf("Voice Agent: session started with %s (model: %s)", s.provider.Name(), cfg.Model)
	return nil
}

// SendAudio forwards a PCM audio chunk to the real-time model.
func (s *Session) SendAudio(chunk []byte) error {
	if s.currentState() == StateInactive || s.currentState() == StateDeactivating {
		return nil
	}
	s.mu.Lock()
	timer := s.idleTimer
	s.mu.Unlock()
	if timer != nil {
		timer.Reset()
	}
	return s.provider.SendAudio(chunk)
}

// Stop deactivates the Voice Agent session.
func (s *Session) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentState() == StateInactive {
		return
	}

	s.setState(StateDeactivating)

	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	if s.cancelFn != nil {
		s.cancelFn()
	}
	if s.provider != nil {
		if err := s.provider.Close(); err != nil {
			log.Printf("Voice Agent: close error: %v", err)
		}
	}

	s.setState(StateInactive)
	log.Printf("Voice Agent: session stopped")
}

// State returns the current session state.
func (s *Session) CurrentState() State {
	return s.currentState()
}

func (s *Session) currentState() State {
	return s.state.Load().(State)
}

func (s *Session) setState(state State) {
	s.state.Store(state)
	if s.callbacks.OnStateChange != nil {
		s.callbacks.OnStateChange(state)
	}
}

func (s *Session) receiveLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := s.provider.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return // Context cancelled, normal shutdown.
			}
			log.Printf("Voice Agent: receive error: %v", err)
			if s.callbacks.OnError != nil {
				s.callbacks.OnError(err)
			}
			return
		}

		if len(msg.Audio) > 0 {
			s.setState(StateSpeaking)
			if s.callbacks.OnAudio != nil {
				s.callbacks.OnAudio(msg.Audio)
			}
		}

		if msg.Text != "" {
			if s.callbacks.OnText != nil {
				s.callbacks.OnText(msg.Text)
			}
		}

		if msg.Done {
			s.setState(StateListening)
			s.mu.Lock()
			timer := s.idleTimer
			s.mu.Unlock()
			if timer != nil {
				timer.Reset()
			}
		}
	}
}
