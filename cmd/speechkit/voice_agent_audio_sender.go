package main

import (
	"context"
	"sync"
	"sync/atomic"
)

const defaultVoiceAgentAudioQueueSize = 24

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
