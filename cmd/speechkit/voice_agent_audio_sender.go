package main

import (
	"context"
	"sync"
	"sync/atomic"
)

// defaultVoiceAgentAudioQueueSize bounds the audio sender's in-flight frame
// channel. The buffer is also used as the pre-session ring buffer: mic frames
// captured between hold-to-talk KeyDown and the moment Gemini Live finishes
// its WebSocket handshake queue here, so the user's first words are replayed
// in order once the sender's drain goroutine starts. At 32 ms frames, 96
// entries cover roughly three seconds of pre-session audio — comfortably
// above the typical 300-1500 ms connect latency. Frames captured beyond the
// buffer get the oldest-first eviction in Enqueue.
const defaultVoiceAgentAudioQueueSize = 96

type voiceAgentAudioSink interface {
	SendAudio([]byte) error
}

type voiceAgentAudioSender struct {
	sink voiceAgentAudioSink

	frames  chan []byte
	done    chan struct{}
	started atomic.Bool
	closed  atomic.Bool
	once    sync.Once

	onSendError func(error)
}

func newVoiceAgentAudioSender(sink voiceAgentAudioSink, queueSize int) *voiceAgentAudioSender {
	if queueSize <= 0 {
		queueSize = defaultVoiceAgentAudioQueueSize
	}
	return &voiceAgentAudioSender{
		sink:   sink,
		frames: make(chan []byte, queueSize),
		done:   make(chan struct{}),
	}
}

func (s *voiceAgentAudioSender) Start(ctx context.Context) {
	if s == nil || s.sink == nil {
		return
	}
	if ctx == nil {
		return
	}
	if !s.started.CompareAndSwap(false, true) {
		return
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.done:
				return
			case frame := <-s.frames:
				if len(frame) == 0 {
					continue
				}
				if err := s.sink.SendAudio(frame); err != nil && s.onSendError != nil {
					s.onSendError(err)
				}
			}
		}
	}()
}

func (s *voiceAgentAudioSender) Enqueue(frame []byte) bool {
	if s == nil || s.sink == nil || len(frame) == 0 || s.closed.Load() {
		return false
	}

	stableFrame := append([]byte(nil), frame...)
	select {
	case <-s.done:
		return false
	default:
	}

	select {
	case s.frames <- stableFrame:
		return true
	default:
	}

	select {
	case <-s.frames:
	default:
	}

	select {
	case s.frames <- stableFrame:
		return true
	case <-s.done:
		return false
	default:
		return false
	}
}

func (s *voiceAgentAudioSender) Stop() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		s.closed.Store(true)
		close(s.done)
	})
}

func (s *appState) setVoiceAgentAudioSender(sender *voiceAgentAudioSender) {
	if s == nil {
		return
	}
	s.mu.Lock()
	old := s.voiceAgentAudioSender
	s.voiceAgentAudioSender = sender
	s.mu.Unlock()
	if old != nil && old != sender {
		old.Stop()
	}
}

func (s *appState) stopVoiceAgentAudioSender() {
	if s == nil {
		return
	}
	s.mu.Lock()
	sender := s.voiceAgentAudioSender
	s.voiceAgentAudioSender = nil
	s.mu.Unlock()
	if sender != nil {
		sender.Stop()
	}
}

// setVoiceAgentActivationCancel records the cancel function for the in-flight
// activation goroutine so a hold-to-talk release that lands before the
// session has transitioned out of StateInactive can still abort the WebSocket
// handshake. Any prior cancel function (left over from a previous activation
// that bailed without clearing) is invoked defensively to avoid stranding a
// stuck goroutine.
func (s *appState) setVoiceAgentActivationCancel(cancel context.CancelFunc) {
	if s == nil {
		return
	}
	s.mu.Lock()
	old := s.voiceAgentActivationCancel
	s.voiceAgentActivationCancel = cancel
	s.mu.Unlock()
	if old != nil {
		old()
	}
}

// takeVoiceAgentActivationCancel atomically pulls and clears the activation
// cancel hook. The caller owns invoking it.
func (s *appState) takeVoiceAgentActivationCancel() context.CancelFunc {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	cancel := s.voiceAgentActivationCancel
	s.voiceAgentActivationCancel = nil
	s.mu.Unlock()
	return cancel
}

// clearVoiceAgentActivationCancel drops the activation cancel hook without
// invoking it. Called by the activation goroutine once session.Start has
// either succeeded (state has moved past Inactive — release uses Stop now) or
// failed (the goroutine handled the teardown itself).
func (s *appState) clearVoiceAgentActivationCancel() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.voiceAgentActivationCancel = nil
	s.mu.Unlock()
}
