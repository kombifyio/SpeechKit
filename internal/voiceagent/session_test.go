package voiceagent

import (
	"context"
	"sync"
	"testing"
	"time"
)

type sessionTestProvider struct {
	mu        sync.Mutex
	connected bool
	closed    bool
	messages  chan *LiveMessage
}

func newSessionTestProvider() *sessionTestProvider {
	return &sessionTestProvider{
		messages: make(chan *LiveMessage, 8),
	}
}

func (p *sessionTestProvider) Connect(_ context.Context, _ LiveConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connected = true
	return nil
}

func (p *sessionTestProvider) SendAudio(_ []byte) error { return nil }

func (p *sessionTestProvider) SendAudioStreamEnd() error { return nil }

func (p *sessionTestProvider) Receive(ctx context.Context) (*LiveMessage, error) {
	select {
	case msg := <-p.messages:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *sessionTestProvider) SendText(_ string) error               { return nil }
func (p *sessionTestProvider) SendToolResponse(_ ToolResponse) error { return nil }
func (p *sessionTestProvider) Name() string                          { return "session-test" }
func (p *sessionTestProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func TestSessionTransitionsToProcessingAfterFinalInputTranscript(t *testing.T) {
	provider := newSessionTestProvider()
	stateChanges := make(chan State, 8)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) {
			stateChanges <- state
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer session.Stop()

	provider.messages <- &LiveMessage{
		InputTranscript:     "hello there",
		InputTranscriptDone: true,
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case state := <-stateChanges:
			if state == StateProcessing {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s state", StateProcessing)
		}
	}
}

func TestSessionTransitionsToSpeakingWhenAudioArrives(t *testing.T) {
	provider := newSessionTestProvider()
	stateChanges := make(chan State, 8)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) {
			stateChanges <- state
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer session.Stop()

	provider.messages <- &LiveMessage{
		Audio: []byte{1, 2, 3, 4},
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case state := <-stateChanges:
			if state == StateSpeaking {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s state", StateSpeaking)
		}
	}
}

func TestSessionReturnsToListeningWhenOutputTranscriptFinishesWithoutTurnComplete(t *testing.T) {
	provider := newSessionTestProvider()
	stateChanges := make(chan State, 16)
	outputDone := make(chan struct{}, 1)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) {
			stateChanges <- state
		},
		OnOutputTranscript: func(_ string, done bool) {
			if done {
				outputDone <- struct{}{}
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer session.Stop()
	drainStateChanges(stateChanges)

	provider.messages <- &LiveMessage{
		OutputTranscript:     "finished answer",
		OutputTranscriptDone: true,
	}

	select {
	case <-outputDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for output transcript callback")
	}
	if session.CurrentState() != StateListening {
		t.Fatalf("current state = %s, want %s", session.CurrentState(), StateListening)
	}
}

func TestSessionDoesNotEmitDuplicateStateChangesForConsecutiveAudioChunks(t *testing.T) {
	provider := newSessionTestProvider()
	stateChanges := make(chan State, 16)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) {
			stateChanges <- state
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer session.Stop()
	drainStateChanges(stateChanges)

	provider.messages <- &LiveMessage{Audio: []byte{1, 2, 3, 4}}
	waitForState(t, stateChanges, StateSpeaking)

	provider.messages <- &LiveMessage{Audio: []byte{5, 6, 7, 8}}
	time.Sleep(50 * time.Millisecond)

	if got := countBufferedState(stateChanges, StateSpeaking); got != 0 {
		t.Fatalf("duplicate %s state changes = %d, want 0", StateSpeaking, got)
	}
}

func TestSessionKeepsSpeakingStateForTranscriptPartialsDuringAudioTurn(t *testing.T) {
	provider := newSessionTestProvider()
	stateChanges := make(chan State, 16)
	outputTranscript := make(chan struct{}, 1)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) {
			stateChanges <- state
		},
		OnOutputTranscript: func(_ string, _ bool) {
			outputTranscript <- struct{}{}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()
	drainStateChanges(stateChanges)

	provider.messages <- &LiveMessage{Audio: []byte{1, 2, 3, 4}}
	waitForState(t, stateChanges, StateSpeaking)
	drainStateChanges(stateChanges)

	provider.messages <- &LiveMessage{OutputTranscript: "partial answer"}

	select {
	case <-outputTranscript:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for output transcript callback")
	}
	time.Sleep(50 * time.Millisecond)

	if session.CurrentState() != StateSpeaking {
		t.Fatalf("current state = %s, want %s", session.CurrentState(), StateSpeaking)
	}
	if got := countBufferedState(stateChanges, StateProcessing); got != 0 {
		t.Fatalf("unexpected %s state changes during speaking turn = %d, want 0", StateProcessing, got)
	}
}

func TestSessionReturnsToListeningWhenAudioTurnDoesNotSendDone(t *testing.T) {
	provider := newSessionTestProvider()
	stateChanges := make(chan State, 16)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) {
			stateChanges <- state
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()
	drainStateChanges(stateChanges)

	provider.messages <- &LiveMessage{Audio: []byte{1, 2, 3, 4}}
	waitForState(t, stateChanges, StateSpeaking)
	waitForState(t, stateChanges, StateListening)
}

func drainStateChanges(ch <-chan State) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func waitForState(t *testing.T, ch <-chan State, want State) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case state := <-ch:
			if state == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", want)
		}
	}
}

func countBufferedState(ch <-chan State, want State) int {
	count := 0
	for {
		select {
		case state := <-ch:
			if state == want {
				count++
			}
		default:
			return count
		}
	}
}
