package main

import (
	"sync"
	"sync/atomic"
)

// audioSubscription exposes a loss counter alongside the frame channel. A
// security-sensitive capture must reject its transcript authority if this
// counter is non-zero; silently truncated microphone input is not authoritative.
type audioSubscription struct {
	frames               chan []byte
	dropped              atomic.Uint64
	playbackContaminated atomic.Bool
}

func (s *audioSubscription) PlaybackContaminated() bool {
	return s != nil && s.playbackContaminated.Load()
}

func (s *audioSubscription) DroppedFrames() uint64 {
	if s == nil {
		return 0
	}
	return s.dropped.Load()
}

type audioFanout struct {
	mu            sync.Mutex
	sinks         []*audioSubscription
	playbackDepth int
}

func (f *audioFanout) subscribe(buffer int) *audioSubscription {
	if buffer < 0 {
		buffer = 0
	}
	subscription := &audioSubscription{frames: make(chan []byte, buffer)}
	f.mu.Lock()
	f.sinks = append(f.sinks, subscription)
	if f.playbackDepth > 0 {
		subscription.playbackContaminated.Store(true)
	}
	f.mu.Unlock()
	return subscription
}

func (f *audioFanout) unsubscribe(frames chan []byte) {
	if frames == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, subscription := range f.sinks {
		if subscription.frames == frames {
			f.sinks = append(f.sinks[:i], f.sinks[i+1:]...)
			close(subscription.frames)
			return
		}
	}
}

func (f *audioFanout) publish(input []byte) {
	if len(input) == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, subscription := range f.sinks {
		// Each subscriber owns its frame. A wake-word or diagnostics consumer
		// must not be able to mutate the side-effect authority capture.
		frame := append([]byte(nil), input...)
		select {
		case subscription.frames <- frame:
		default:
			subscription.dropped.Add(1)
		}
	}
}

func (f *audioFanout) beginPlayback() {
	f.mu.Lock()
	f.playbackDepth++
	for _, subscription := range f.sinks {
		subscription.playbackContaminated.Store(true)
	}
	f.mu.Unlock()
}

func (f *audioFanout) endPlayback() {
	f.mu.Lock()
	if f.playbackDepth > 0 {
		f.playbackDepth--
	}
	f.mu.Unlock()
}
