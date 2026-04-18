// Package voiceagent implements the Voice Agent Mode — a real-time,
// bidirectional voice conversation using native audio-to-audio models
// (Gemini Live API, OpenAI Realtime API) over WebSocket.
package voiceagent

import (
	"context"
	"fmt"
	"log/slog"
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

// ThinkingLevel controls how much deliberate reasoning Gemini Live should spend.
type ThinkingLevel string

const (
	ThinkingLevelOff    ThinkingLevel = "off"
	ThinkingLevelLow    ThinkingLevel = "low"
	ThinkingLevelMedium ThinkingLevel = "medium"
	ThinkingLevelHigh   ThinkingLevel = "high"
)

// StartSensitivity controls how aggressively automatic activity detection commits speech start.
type StartSensitivity string

const (
	StartSensitivityLow    StartSensitivity = "low"
	StartSensitivityMedium StartSensitivity = "medium"
	StartSensitivityHigh   StartSensitivity = "high"
)

// EndSensitivity controls how aggressively automatic activity detection commits speech end.
type EndSensitivity string

const (
	EndSensitivityLow    EndSensitivity = "low"
	EndSensitivityMedium EndSensitivity = "medium"
	EndSensitivityHigh   EndSensitivity = "high"
)

// ActivityHandling controls what Gemini Live should do when new activity starts.
type ActivityHandling string

const (
	ActivityHandlingUnspecified               ActivityHandling = ""
	ActivityHandlingNoInterrupt               ActivityHandling = "no_interrupt"
	ActivityHandlingStartOfActivityInterrupts ActivityHandling = "start_of_activity_interrupts"
)

// TurnCoverage controls how the live API builds a user turn from incoming activity.
type TurnCoverage string

const (
	TurnCoverageUnspecified               TurnCoverage = ""
	TurnCoverageTurnIncludesOnlyActivity  TurnCoverage = "turn_includes_only_activity"
	TurnCoverageTurnIncludesAllInput      TurnCoverage = "turn_includes_all_input"
	TurnCoverageTurnIncludesAudioActivity TurnCoverage = "turn_includes_audio_activity"
)

// ThinkingPolicy defines optional Gemini Live thinking behavior.
type ThinkingPolicy struct {
	Enabled         bool
	IncludeThoughts bool
	ThinkingBudget  int32
	ThinkingLevel   ThinkingLevel
}

// ContextCompressionPolicy defines how the live API should compress long sessions.
type ContextCompressionPolicy struct {
	Enabled       bool
	TriggerTokens int64
	TargetTokens  int64
}

// ActivityDetectionPolicy defines server-side VAD/session turn behavior.
type ActivityDetectionPolicy struct {
	Automatic         bool
	StartSensitivity  StartSensitivity
	EndSensitivity    EndSensitivity
	PrefixPaddingMs   int32
	SilenceDurationMs int32
	ActivityHandling  ActivityHandling
	TurnCoverage      TurnCoverage
}

// LivePolicies configures Google Live API features that shape Voice Agent behavior.
type LivePolicies struct {
	EnableInputAudioTranscription  bool
	EnableOutputAudioTranscription bool
	EnableAffectiveDialog          bool
	Thinking                       ThinkingPolicy
	ContextCompression             ContextCompressionPolicy
	ActivityDetection              ActivityDetectionPolicy
}

// ToolBehavior controls whether the model waits for a tool result.
type ToolBehavior string

const (
	ToolBehaviorUnspecified ToolBehavior = ""
	ToolBehaviorBlocking    ToolBehavior = "blocking"
	ToolBehaviorNonBlocking ToolBehavior = "non_blocking"
)

// ToolResponseScheduling controls how a non-blocking tool result is reintroduced into the conversation.
type ToolResponseScheduling string

const (
	ToolResponseSchedulingUnspecified ToolResponseScheduling = ""
	ToolResponseSchedulingSilent      ToolResponseScheduling = "silent"
	ToolResponseSchedulingWhenIdle    ToolResponseScheduling = "when_idle"
	ToolResponseSchedulingInterrupt   ToolResponseScheduling = "interrupt"
)

// ToolDefinition exposes a host-side action the Voice Agent may call.
type ToolDefinition struct {
	Name                 string
	Description          string
	ParametersJSONSchema map[string]any
	ResponseJSONSchema   map[string]any
	Behavior             ToolBehavior
}

// ToolCall is a host-side action request emitted by the Voice Agent runtime.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// ToolResponse resolves a previously emitted tool call.
type ToolResponse struct {
	ID       string
	Name     string
	Response map[string]any

	Scheduling   ToolResponseScheduling
	WillContinue *bool
}

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

	// SendToolResponse sends the result of a host-side tool invocation back to the model.
	SendToolResponse(response ToolResponse) error

	// Close terminates the WebSocket session.
	Close() error

	// Name returns the provider identifier.
	Name() string
}

// LiveConfig configures a real-time session.
type LiveConfig struct {
	Model            string // e.g. "gemini-2.5-flash-native-audio-preview-12-2025"
	APIKey           string
	Voice            string // Voice name
	FrameworkPrompt  string
	RefinementPrompt string
	Instruction      string // Deprecated: kept as compat alias for FrameworkPrompt.
	SystemPrompt     string // Deprecated: kept as compat alias, prefer FrameworkPrompt.
	VocabularyHint   string
	Locale           string
	Policies         LivePolicies
	Tools            []ToolDefinition
}

// LiveMessage is a message received from the real-time model.
type LiveMessage struct {
	Audio []byte // PCM audio chunk (24kHz 16-bit mono)
	Text  string // Text transcript (may be partial or empty)
	Done  bool   // True when the model's turn is complete

	// Transcription fields (populated when transcription is enabled).
	InputTranscript      string // User speech transcribed by server
	InputTranscriptDone  bool   // True when input transcription segment is final
	OutputTranscript     string // Model speech transcribed by server
	OutputTranscriptDone bool   // True when output transcription segment is final

	ToolCalls               []ToolCall
	ToolCallCancellationIDs []string
	Interrupted             bool // True when user interrupted model (barge-in)
	GoAway                  bool // True when server signals imminent session end
}

// Callbacks are event handlers for UI integration.
type Callbacks struct {
	OnStateChange          func(state State)
	OnAudio                func(audio []byte) // Audio chunk to play
	OnText                 func(text string)  // Text for display (speech bubble)
	OnError                func(err error)
	OnInputTranscript      func(text string, done bool) // User speech transcribed
	OnOutputTranscript     func(text string, done bool) // Model speech transcribed
	OnToolCall             func(call ToolCall)
	OnToolCallCancellation func(ids []string)
	OnInterrupted          func() // User interrupted model (barge-in)
	OnSessionEnd           func() // Session ended (error, GoAway failure, or deactivation)
}

// LiveReconnector is an optional interface for providers that support session reconnection.
type LiveReconnector interface {
	Reconnect(ctx context.Context) error
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
	lastCfg   LiveConfig // Stored for reconnection
	lastIdle  IdleConfig // Stored for reconnection
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

	// Store config for potential reconnection.
	s.mu.Lock()
	s.lastCfg = cfg
	s.lastIdle = idleCfg
	s.mu.Unlock()

	// Start idle timer.
	s.mu.Lock()
	s.idleTimer = NewIdleTimer(idleCfg, s)
	s.mu.Unlock()

	// Start receive loop in background.
	go s.receiveLoop(sessionCtx)

	s.setState(StateListening)
	s.idleTimer.Reset()

	slog.Info("voice agent session started", "provider", s.provider.Name(), "model", cfg.Model)
	return nil
}

// SendAudio forwards a PCM audio chunk to the real-time model.
func (s *Session) SendAudio(chunk []byte) error {
	if s.currentState() == StateInactive || s.currentState() == StateDeactivating {
		return nil
	}
	// Hold the lock across the Reset call to prevent a TOCTOU race with Stop(),
	// which sets idleTimer to nil under the same lock.
	s.mu.Lock()
	if s.idleTimer != nil {
		s.idleTimer.Reset()
	}
	s.mu.Unlock()
	return s.provider.SendAudio(chunk)
}

// SendText injects a user text turn into the live session.
func (s *Session) SendText(text string) error {
	if s.currentState() == StateInactive || s.currentState() == StateDeactivating {
		return nil
	}
	s.mu.Lock()
	if s.idleTimer != nil {
		s.idleTimer.Reset()
	}
	s.mu.Unlock()
	return s.provider.SendText(text)
}

// SendToolResponse forwards the result of a host-side tool invocation to the model.
func (s *Session) SendToolResponse(response ToolResponse) error {
	if s.currentState() == StateInactive || s.currentState() == StateDeactivating {
		return nil
	}
	return s.provider.SendToolResponse(response)
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
			slog.Error("voice agent close error", "err", err)
		}
	}

	s.setState(StateInactive)
	slog.Info("voice agent session stopped")
}

// State returns the current session state.
func (s *Session) CurrentState() State {
	return s.currentState()
}

func (s *Session) currentState() State {
	return s.state.Load().(State) //nolint:errcheck // atomic.Value always stores State; type assertion is safe
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
			slog.Error("voice agent receive error", "err", err)
			if s.callbacks.OnError != nil {
				s.callbacks.OnError(err)
			}
			// Hard cleanup: move to inactive so the session can be restarted.
			s.cleanupOnError()
			return
		}

		if msg == nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("voice agent received nil message")
			if s.callbacks.OnError != nil {
				s.callbacks.OnError(fmt.Errorf("voiceagent: received nil message"))
			}
			s.cleanupOnError()
			return
		}

		// Barge-in: user interrupted model.
		if msg.Interrupted {
			s.setState(StateListening)
			if s.callbacks.OnInterrupted != nil {
				s.callbacks.OnInterrupted()
			}
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

		// Input transcription (user speech).
		if msg.InputTranscript != "" {
			if s.callbacks.OnInputTranscript != nil {
				s.callbacks.OnInputTranscript(msg.InputTranscript, msg.InputTranscriptDone)
			}
		}

		// Output transcription (model speech).
		if msg.OutputTranscript != "" {
			if s.callbacks.OnOutputTranscript != nil {
				s.callbacks.OnOutputTranscript(msg.OutputTranscript, msg.OutputTranscriptDone)
			}
		}

		if len(msg.ToolCalls) > 0 && s.callbacks.OnToolCall != nil {
			for _, call := range msg.ToolCalls {
				s.callbacks.OnToolCall(call)
			}
		}

		if len(msg.ToolCallCancellationIDs) > 0 && s.callbacks.OnToolCallCancellation != nil {
			s.callbacks.OnToolCallCancellation(msg.ToolCallCancellationIDs)
		}

		// GoAway: server signals imminent session end — attempt reconnect.
		if msg.GoAway {
			if reconnector, ok := s.provider.(LiveReconnector); ok {
				slog.Info("voice agent: GoAway received, attempting reconnect")
				if err := reconnector.Reconnect(ctx); err != nil {
					slog.Error("voice agent reconnect failed", "err", err)
					if s.callbacks.OnError != nil {
						s.callbacks.OnError(fmt.Errorf("reconnect failed: %w", err))
					}
					s.cleanupOnError()
					return
				}
				slog.Info("voice agent: reconnected successfully")
				continue // Resume receive loop with reconnected session.
			}
			// Provider doesn't support reconnect — treat as fatal.
			slog.Warn("voice agent: GoAway received but provider does not support reconnect")
			s.cleanupOnError()
			return
		}

		if msg.Done {
			s.setState(StateListening)
			// Hold the lock across Reset to prevent a TOCTOU race with Stop().
			s.mu.Lock()
			if s.idleTimer != nil {
				s.idleTimer.Reset()
			}
			s.mu.Unlock()
		}
	}
}

// cleanupOnError transitions the session to inactive and notifies the caller.
// Called when the receive loop encounters an unrecoverable error.
func (s *Session) cleanupOnError() {
	s.mu.Lock()
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	if s.cancelFn != nil {
		s.cancelFn()
	}
	if s.provider != nil {
		if err := s.provider.Close(); err != nil {
			// The session is already on an error path; we still want the
			// provider close failure visible in logs for diagnosis (leaked
			// WebSocket, stuck HTTP pool, etc.) instead of silently
			// swallowed.
			slog.Warn("voiceagent: provider close during cleanup returned error", "err", err)
		}
	}
	s.mu.Unlock()

	s.setState(StateInactive)
	if s.callbacks.OnSessionEnd != nil {
		s.callbacks.OnSessionEnd()
	}
}
