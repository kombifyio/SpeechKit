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
	sentText  []string
}

type reconnectingSessionTestProvider struct {
	*sessionTestProvider
	reconnects int
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

func (p *sessionTestProvider) SendText(text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sentText = append(p.sentText, text)
	return nil
}
func (p *sessionTestProvider) SendToolResponse(_ ToolResponse) error { return nil }
func (p *sessionTestProvider) Name() string                          { return "session-test" }
func (p *sessionTestProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func (p *reconnectingSessionTestProvider) Reconnect(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reconnects++
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

func TestSessionShowsRecoveringStateDuringGoAwayReconnect(t *testing.T) {
	provider := &reconnectingSessionTestProvider{sessionTestProvider: newSessionTestProvider()}
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

	provider.messages <- &LiveMessage{GoAway: true}
	waitForState(t, stateChanges, StateRecovering)
	waitForState(t, stateChanges, StateListening)

	if provider.reconnects != 1 {
		t.Fatalf("reconnects = %d, want 1", provider.reconnects)
	}
}

func TestSessionHandlesMultiTurnTextDialog(t *testing.T) {
	provider := newSessionTestProvider()
	events := make(chan dialogueEvent, 16)
	session := NewSession(provider, Callbacks{
		OnInputTranscript: func(text string, done bool) {
			if done {
				events <- dialogueEvent{side: "user", text: text}
			}
		},
		OnOutputTranscript: func(text string, done bool) {
			if done {
				events <- dialogueEvent{side: "agent", text: text}
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()

	dialog := []struct {
		user  string
		agent string
	}{
		{user: "Ping one: collect three naming ideas.", agent: "Pong one: I have three concise options."},
		{user: "Ping two: make the strongest one warmer.", agent: "Pong two: the warm version is ready."},
		{user: "Ping three: compare it with the direct variant.", agent: "Pong three: the warmer option is friendlier."},
		{user: "Ping four: give me the final choice.", agent: "Pong four: choose the warm concise version."},
	}

	for _, turn := range dialog {
		if err := session.SendText(turn.user); err != nil {
			t.Fatalf("send text %q: %v", turn.user, err)
		}
		provider.messages <- &LiveMessage{InputTranscript: turn.user, InputTranscriptDone: true}
		waitForDialogueEvent(t, events, "user", turn.user)

		provider.messages <- &LiveMessage{OutputTranscript: turn.agent, OutputTranscriptDone: true}
		waitForDialogueEvent(t, events, "agent", turn.agent)
	}

	sentText := providerSentText(provider)
	if len(sentText) != len(dialog) {
		t.Fatalf("sent text turns = %d, want %d: %#v", len(sentText), len(dialog), sentText)
	}
	for i, turn := range dialog {
		if sentText[i] != turn.user {
			t.Fatalf("sent text[%d] = %q, want %q", i, sentText[i], turn.user)
		}
	}
	if totalInteractions := len(dialog) * 2; totalInteractions <= 6 {
		t.Fatalf("dialog interactions = %d, want > 6", totalInteractions)
	}
	if session.CurrentState() != StateListening {
		t.Fatalf("current state = %s, want %s", session.CurrentState(), StateListening)
	}
}

type dialogueEvent struct {
	side string
	text string
}

func waitForDialogueEvent(t *testing.T, ch <-chan dialogueEvent, side, text string) {
	t.Helper()
	select {
	case event := <-ch:
		if event.side != side || event.text != text {
			t.Fatalf("dialogue event = %#v, want side=%q text=%q", event, side, text)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s dialogue event %q", side, text)
	}
}

func providerSentText(provider *sessionTestProvider) []string {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]string(nil), provider.sentText...)
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
