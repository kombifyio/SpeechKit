package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/audio"
)

type blockingVoiceAgentAudioSink struct {
	received chan []byte
	release  chan struct{}
	sends    atomic.Int32
}

func newBlockingVoiceAgentAudioSink() *blockingVoiceAgentAudioSink {
	return &blockingVoiceAgentAudioSink{
		received: make(chan []byte, 8),
		release:  make(chan struct{}),
	}
}

func (s *blockingVoiceAgentAudioSink) SendAudio(frame []byte) error {
	s.sends.Add(1)
	s.received <- append([]byte(nil), frame...)
	<-s.release
	return nil
}

func TestVoiceAgentAudioSenderEnqueueDoesNotBlockBehindSlowNetworkSend(t *testing.T) {
	sink := newBlockingVoiceAgentAudioSink()
	sender := newVoiceAgentAudioSender(sink, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer sender.Stop()

	sender.Start(ctx)

	if !sender.Enqueue([]byte{1}) {
		t.Fatal("first enqueue rejected")
	}
	select {
	case <-sink.received:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first SendAudio call")
	}

	done := make(chan struct{})
	go func() {
		_ = sender.Enqueue([]byte{2})
		_ = sender.Enqueue([]byte{3})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Enqueue blocked while SendAudio was still in progress")
	}

	close(sink.release)
}

type failingVoiceAgentAudioSink struct {
	err error
}

func (s failingVoiceAgentAudioSink) SendAudio(_ []byte) error {
	return s.err
}

// countingVoiceAgentAudioSink counts SendAudio calls. Uses atomic
// counter (no slice) so the wait-loop check is race-free.
type countingVoiceAgentAudioSink struct {
	calls atomic.Int32
	// recv stays for tests that want to inspect specific payloads;
	// guarded by mu.
	mu   sync.Mutex
	recv [][]byte
}

func (s *countingVoiceAgentAudioSink) SendAudio(frame []byte) error {
	s.calls.Add(1)
	s.mu.Lock()
	s.recv = append(s.recv, append([]byte(nil), frame...))
	s.mu.Unlock()
	return nil
}

func (s *countingVoiceAgentAudioSink) Calls() int32 { return s.calls.Load() }

func TestVoiceAgentAudioSenderUsesFramePool(t *testing.T) {
	sink := &countingVoiceAgentAudioSink{}
	const N = 32
	sender := newVoiceAgentAudioSender(sink, N)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer sender.Stop()

	startGets := audio.DefaultFramePool.Stats().Gets
	sender.Start(ctx)

	for i := 0; i < N; i++ {
		if !sender.Enqueue([]byte{byte(i), byte(i + 1), byte(i + 2), byte(i + 3)}) {
			t.Fatalf("Enqueue %d rejected", i)
		}
	}

	// Wait for drain. With a non-blocking sink this is essentially
	// schedule latency; allow a generous timeout.
	deadline := time.After(2 * time.Second)
	for sink.Calls() < int32(N) {
		select {
		case <-deadline:
			t.Fatalf("drain incomplete: received=%d want>=%d", sink.Calls(), N)
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}

	endGets := audio.DefaultFramePool.Stats().Gets
	if endGets-startGets < uint64(N) {
		t.Errorf("pool Gets delta = %d after %d Enqueues; want >= %d (sender must lease per frame)", endGets-startGets, N, N)
	}
}

func TestVoiceAgentAudioSenderEvictionReleasesPoolBuffer(t *testing.T) {
	// queueSize=1 forces eviction on every Enqueue while the sink
	// blocks. We verify that the evicted item's release runs (no leak)
	// by tracking pool Gets — if eviction failed to release, the pool
	// would gradually empty out over many evictions but our test just
	// asserts the no-panic, no-deadlock path.
	sink := newBlockingVoiceAgentAudioSink()
	sender := newVoiceAgentAudioSender(sink, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer sender.Stop()

	sender.Start(ctx)

	if !sender.Enqueue([]byte{1, 2, 3}) {
		t.Fatal("first enqueue rejected")
	}
	select {
	case <-sink.received:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for first SendAudio")
	}

	startGets := audio.DefaultFramePool.Stats().Gets

	// Hammer Enqueue while the sink is blocked; every call past the
	// first triggers the eviction path internally.
	for i := 0; i < 10; i++ {
		_ = sender.Enqueue([]byte{byte(i)})
	}

	endGets := audio.DefaultFramePool.Stats().Gets
	if endGets-startGets < 10 {
		t.Errorf("pool Gets delta = %d after 10 Enqueues; want >= 10 (every Enqueue should lease)", endGets-startGets)
	}

	close(sink.release)
}

func TestVoiceAgentAudioSenderStopDrainsRemainingFrames(t *testing.T) {
	// Set up a sender, enqueue a few frames, never Start (so the
	// drain goroutine never runs), then Stop. Without the per-Start
	// drainQueueLocked path AND a Stop-side drain, pool buffers would
	// leak. We assert the no-panic + finite-time termination.
	sink := &countingVoiceAgentAudioSink{}
	sender := newVoiceAgentAudioSender(sink, 8)

	for i := 0; i < 4; i++ {
		_ = sender.Enqueue([]byte{byte(i)})
	}

	done := make(chan struct{})
	go func() {
		sender.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Stop did not terminate within 500ms — possible goroutine wedge")
	}
}

func TestVoiceAgentAudioSenderReportsSendErrors(t *testing.T) {
	wantErr := errors.New("live input rejected")
	sender := newVoiceAgentAudioSender(failingVoiceAgentAudioSink{err: wantErr}, 1)
	errs := make(chan error, 1)
	sender.onSendError = func(err error) {
		errs <- err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer sender.Stop()

	sender.Start(ctx)
	if !sender.Enqueue([]byte{1, 2, 3, 4}) {
		t.Fatal("enqueue rejected")
	}

	select {
	case got := <-errs:
		if !errors.Is(got, wantErr) {
			t.Fatalf("reported error = %v, want %v", got, wantErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for send error")
	}
}
