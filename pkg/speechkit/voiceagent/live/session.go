package live

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const speakingSettleDelay = 900 * time.Millisecond

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
	workflow  *workflowState
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

	cfg, workflow := prepareWorkflowStart(cfg)
	if err := s.provider.Connect(sessionCtx, cfg); err != nil {
		s.setState(StateInactive)
		cancel()
		return fmt.Errorf("voiceagent: connect failed: %w", err)
	}

	// Store config for potential reconnection.
	s.mu.Lock()
	s.lastCfg = cfg
	s.lastIdle = idleCfg
	s.workflow = workflow
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

// EndAudioStream tells the live provider that the current microphone stream ended.
func (s *Session) EndAudioStream() error {
	if s.currentState() == StateInactive || s.currentState() == StateDeactivating {
		return nil
	}
	if s.provider == nil {
		return nil
	}
	if err := s.provider.SendAudioStreamEnd(); err != nil {
		return err
	}
	s.setProcessingUnlessSpeaking()
	return nil
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

// AdvanceWorkflowStep moves a configured local workflow to its next step and
// updates the active provider instructions. It returns nil when no workflow is
// active or the workflow has already completed.
func (s *Session) AdvanceWorkflowStep(ctx context.Context, reason string) error {
	next, ok := s.prepareWorkflowAdvance(reason)
	if !ok {
		return nil
	}
	if updater, ok := s.provider.(LiveInstructionUpdater); ok {
		return updater.UpdateInstructions(ctx, next)
	}
	text := RenderHostInstructionUpdate(next)
	if text == "" {
		return nil
	}
	return s.provider.SendText(text)
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
	s.workflow = nil

	s.setState(StateInactive)
	slog.Info("voice agent session stopped")
}

// State returns the current session state.
func (s *Session) CurrentState() State {
	return s.currentState()
}

func (s *Session) ProviderName() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.provider == nil {
		return ""
	}
	return s.provider.Name()
}

func (s *Session) currentState() State {
	return s.state.Load().(State) //nolint:errcheck // atomic.Value always stores State; type assertion is safe
}

func (s *Session) setState(state State) {
	if s.currentState() == state {
		return
	}
	s.state.Store(state)
	if s.callbacks.OnStateChange != nil {
		s.callbacks.OnStateChange(state)
	}
}

func (s *Session) setProcessingUnlessSpeaking() {
	if s.currentState() == StateSpeaking {
		return
	}
	s.setState(StateProcessing)
}

func (s *Session) receiveLoop(ctx context.Context) {
	var speakingSettleTimer *time.Timer
	stopSpeakingSettleTimer := func() {
		if speakingSettleTimer != nil {
			speakingSettleTimer.Stop()
			speakingSettleTimer = nil
		}
	}
	scheduleSpeakingSettle := func() {
		stopSpeakingSettleTimer()
		speakingSettleTimer = time.AfterFunc(speakingSettleDelay, func() {
			if s.currentState() != StateSpeaking {
				return
			}
			slog.Warn("voice agent speaking turn did not emit completion; returning to listening")
			s.setState(StateListening)
			s.resetIdleTimer()
		})
	}
	defer stopSpeakingSettleTimer()

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

		if !s.handleLiveMessage(ctx, msg, scheduleSpeakingSettle, stopSpeakingSettleTimer) {
			return
		}
	}
}

func (s *Session) handleLiveMessage(ctx context.Context, msg *LiveMessage, scheduleSpeakingSettle, stopSpeakingSettleTimer func()) bool {
	if msg.Interrupted {
		s.handleInterruption(stopSpeakingSettleTimer)
	}
	s.handleInputTranscript(ctx, msg)
	s.handleOutputTranscript(msg)
	s.handleText(msg)
	s.handleAudio(msg, scheduleSpeakingSettle)
	s.handleToolEvents(msg)
	if msg.GoAway {
		return s.handleGoAway(ctx)
	}
	if msg.Done || (msg.OutputTranscriptDone && len(msg.Audio) == 0) {
		stopSpeakingSettleTimer()
		s.setState(StateListening)
		s.resetIdleTimer()
	}
	return true
}

func (s *Session) handleInterruption(stopSpeakingSettleTimer func()) {
	stopSpeakingSettleTimer()
	s.setState(StateListening)
	if s.callbacks.OnInterrupted != nil {
		s.callbacks.OnInterrupted()
	}
}

func (s *Session) handleInputTranscript(ctx context.Context, msg *LiveMessage) {
	if msg.InputTranscript == "" {
		return
	}
	if s.callbacks.OnInputTranscript != nil {
		s.callbacks.OnInputTranscript(msg.InputTranscript, msg.InputTranscriptDone)
	}
	if msg.InputTranscriptDone {
		s.recordWorkflowUserTurn(ctx)
	}
	if msg.InputTranscriptDone && !msg.Done && len(msg.Audio) == 0 && msg.OutputTranscript == "" && msg.Text == "" {
		s.setState(StateProcessing)
	}
}

func (s *Session) handleOutputTranscript(msg *LiveMessage) {
	if msg.OutputTranscript == "" {
		return
	}
	if s.callbacks.OnOutputTranscript != nil {
		s.callbacks.OnOutputTranscript(msg.OutputTranscript, msg.OutputTranscriptDone)
	}
	if len(msg.Audio) == 0 && !msg.OutputTranscriptDone {
		s.setProcessingUnlessSpeaking()
	}
}

func (s *Session) handleText(msg *LiveMessage) {
	if msg.Text == "" {
		return
	}
	if s.callbacks.OnText != nil {
		s.callbacks.OnText(msg.Text)
	}
	if len(msg.Audio) == 0 {
		s.setProcessingUnlessSpeaking()
	}
}

func (s *Session) handleAudio(msg *LiveMessage, scheduleSpeakingSettle func()) {
	if len(msg.Audio) == 0 {
		return
	}
	s.setState(StateSpeaking)
	if s.callbacks.OnAudio != nil {
		s.callbacks.OnAudio(msg.Audio)
	}
	scheduleSpeakingSettle()
}

func (s *Session) handleToolEvents(msg *LiveMessage) {
	if len(msg.ToolCalls) > 0 && s.callbacks.OnToolCall != nil {
		for _, call := range msg.ToolCalls {
			s.callbacks.OnToolCall(call)
		}
	}
	if len(msg.ToolCallCancellationIDs) > 0 && s.callbacks.OnToolCallCancellation != nil {
		s.callbacks.OnToolCallCancellation(msg.ToolCallCancellationIDs)
	}
}

func (s *Session) handleGoAway(ctx context.Context) bool {
	reconnector, ok := s.provider.(LiveReconnector)
	if !ok {
		slog.Warn("voice agent: GoAway received but provider does not support reconnect")
		s.cleanupOnError()
		return false
	}
	slog.Info("voice agent: GoAway received, attempting reconnect")
	s.setState(StateRecovering)
	if err := reconnector.Reconnect(ctx); err != nil {
		slog.Error("voice agent reconnect failed", "err", err)
		if s.callbacks.OnError != nil {
			s.callbacks.OnError(fmt.Errorf("reconnect failed: %w", err))
		}
		s.cleanupOnError()
		return false
	}
	slog.Info("voice agent: reconnected successfully")
	s.setState(StateListening)
	return true
}

func (s *Session) resetIdleTimer() {
	// Hold the lock across Reset to prevent a TOCTOU race with Stop().
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idleTimer != nil {
		s.idleTimer.Reset()
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
	s.workflow = nil
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
