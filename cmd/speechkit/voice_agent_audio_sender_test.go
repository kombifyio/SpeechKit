package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
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
