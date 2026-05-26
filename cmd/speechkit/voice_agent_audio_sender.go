package main

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/kombifyio/SpeechKit/internal/audio"
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

// voiceAgentAudioFrame is one queue entry. data points at a buffer leased
// from internal/audio's FramePool; release returns it. The drain goroutine
// MUST call release exactly once per item (after SendAudio, on evict, or
// during Stop drainage) so the pool's refcount stays balanced.
//
// The struct is intentionally small (one slice header + one func pointer)
// so the channel ops don't escape it to heap. The slice header still goes
// through the channel by value; the underlying buffer is the pool's.
type voiceAgentAudioFrame struct {
	data    []byte
	release func()
}

type voiceAgentAudioSender struct {
	sink voiceAgentAudioSink

	frames  chan voiceAgentAudioFrame
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
		frames: make(chan voiceAgentAudioFrame, queueSize),
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
				s.drainQueueLocked()
				return
			case <-s.done:
				s.drainQueueLocked()
				return
			case item := <-s.frames:
				if len(item.data) == 0 {
					item.release()
					continue
				}
				if err := s.sink.SendAudio(item.data); err != nil && s.onSendError != nil {
					s.onSendError(err)
				}
				item.release()
			}
		}
	}()
}

// drainQueueLocked drains any frames remaining in the channel after the
// drain goroutine has been told to exit, returning their pool buffers.
// Called once per Start goroutine, so concurrent invocation is impossible.
func (s *voiceAgentAudioSender) drainQueueLocked() {
	for {
		select {
		case item := <-s.frames:
			item.release()
		default:
			return
		}
	}
}

func (s *voiceAgentAudioSender) Enqueue(frame []byte) bool {
	if s == nil || s.sink == nil || len(frame) == 0 || s.closed.Load() {
		return false
	}

	// Lease a recyclable buffer from the package-level FramePool and
	// copy the caller's frame into it. The buffer is held until the
	// drain goroutine releases it (after SendAudio) or until the
	// eviction path below releases it on queue-full.
	pooled := audio.Get()
	pooled = append(pooled, frame...)
	item := voiceAgentAudioFrame{
		data:    pooled,
		release: makeFramePoolRelease(pooled),
	}

	select {
	case <-s.done:
		item.release()
		return false
	default:
	}

	select {
	case s.frames <- item:
		return true
	default:
	}

	// Queue full: evict the oldest (returning its buffer to the pool)
	// so the freshest pre-session audio wins. Matches the historical
	// "ring buffer that prefers recent" behaviour.
	select {
	case evicted := <-s.frames:
		evicted.release()
	default:
	}

	select {
	case s.frames <- item:
		return true
	case <-s.done:
		item.release()
		return false
	default:
		item.release()
		return false
	}
}

// makeFramePoolRelease wraps audio.Put in a one-shot closure so each
// queue item carries its own release callback. The closure is idempotent
// — second and subsequent calls are no-ops — so multi-path teardown
// (drain after sink + final Stop drain) doesn't double-release.
func makeFramePoolRelease(buf []byte) func() {
	var (
		mu       sync.Mutex
		released bool
	)
	return func() {
		mu.Lock()
		if released {
			mu.Unlock()
			return
		}
		released = true
		mu.Unlock()
		audio.Put(buf)
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
