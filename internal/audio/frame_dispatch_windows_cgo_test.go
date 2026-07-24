//go:build windows && cgo

package audio

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

// newDispatchTestSession builds a MalgoSession with just enough state
// for the frame-dispatch machinery — no malgo context or device needed.
func newDispatchTestSession() *MalgoSession {
	return &MalgoSession{events: make(chan Event, 8)}
}

func TestFrameDispatchDeliversInOrder(t *testing.T) {
	s := newDispatchTestSession()

	var mu sync.Mutex
	var received [][]byte
	s.SetPCMHandler(func(pcm []byte) {
		mu.Lock()
		received = append(received, pcm)
		mu.Unlock()
	})

	var levels []float64
	s.SetLevelHandler(func(level float64) {
		mu.Lock()
		levels = append(levels, level)
		mu.Unlock()
	})

	frames := s.startFrameDispatch()
	want := [][]byte{{1, 0, 2, 0}, {3, 0, 4, 0}, {5, 0, 6, 0}}
	for _, chunk := range want {
		s.enqueueFrame(frames, chunk)
	}
	s.stopFrameDispatch()

	mu.Lock()
	defer mu.Unlock()
	if len(received) != len(want) {
		t.Fatalf("received %d frames, want %d", len(received), len(want))
	}
	for i := range want {
		if !bytes.Equal(received[i], want[i]) {
			t.Fatalf("frame %d = %v, want %v", i, received[i], want[i])
		}
	}
	if len(levels) != len(want) {
		t.Fatalf("level callbacks = %d, want %d", len(levels), len(want))
	}
	if s.overruns.Load() != 0 {
		t.Fatalf("overruns = %d, want 0", s.overruns.Load())
	}
}

func TestFrameDispatchPooledHandlerWinsAndReleases(t *testing.T) {
	s := newDispatchTestSession()

	var mu sync.Mutex
	var pooledFrames int
	var legacyFrames int
	var releases int
	s.SetPCMHandler(func([]byte) {
		mu.Lock()
		legacyFrames++
		mu.Unlock()
	})
	s.SetPooledPCMHandler(func(buf []byte, release func()) {
		mu.Lock()
		pooledFrames++
		mu.Unlock()
		release()
		release() // double release must be a no-op
		mu.Lock()
		releases++
		mu.Unlock()
	})

	frames := s.startFrameDispatch()
	s.enqueueFrame(frames, []byte{9, 0, 9, 0})
	s.stopFrameDispatch()

	mu.Lock()
	defer mu.Unlock()
	if pooledFrames != 1 || releases != 1 {
		t.Fatalf("pooled=%d releases=%d, want 1/1", pooledFrames, releases)
	}
	if legacyFrames != 0 {
		t.Fatalf("legacy handler invoked %d times despite pooled handler", legacyFrames)
	}
}

func TestFrameDispatchOverflowDropsAndCounts(t *testing.T) {
	s := newDispatchTestSession()

	// No drain goroutine: a tiny channel fills immediately.
	frames := make(chan []byte, 2)
	for i := 0; i < 5; i++ {
		s.enqueueFrame(frames, []byte{byte(i), 0})
	}

	if got := s.overruns.Load(); got != 3 {
		t.Fatalf("overruns = %d, want 3", got)
	}
	select {
	case event := <-s.events:
		if event.Type != EventOverrun {
			t.Fatalf("event type = %s, want %s", event.Type, EventOverrun)
		}
	default:
		t.Fatal("expected an EventOverrun to be emitted")
	}
	// The two enqueued frames are still intact and ordered.
	first := <-frames
	if !bytes.Equal(first, []byte{0, 0}) {
		t.Fatalf("first queued frame = %v, want [0 0]", first)
	}
}

func TestFrameDispatchStopWithoutStartIsSafe(t *testing.T) {
	s := newDispatchTestSession()
	s.stopFrameDispatch() // must not panic or block
}

func TestCaptureStallWatchdogEmitsOncePerEpisode(t *testing.T) {
	s := newDispatchTestSession()
	s.running.Store(true)
	// Last frame far in the past — the first tick must flag a stall.
	s.lastFrameNano.Store(time.Now().Add(-time.Minute).UnixNano())

	done := make(chan struct{})
	go s.watchCaptureStall(done)
	defer close(done)

	// The watchdog ticks every stallCheckInterval (2s); wait for one
	// emission and verify no duplicate follows on the next tick.
	select {
	case event := <-s.events:
		if event.Type != EventStalled {
			t.Fatalf("event type = %s, want %s", event.Type, EventStalled)
		}
	case <-time.After(2 * stallCheckInterval * 2):
		t.Fatal("watchdog did not emit EventStalled")
	}
	select {
	case event := <-s.events:
		t.Fatalf("unexpected second event during same stall episode: %v", event)
	case <-time.After(stallCheckInterval + stallCheckInterval/2):
	}
}
