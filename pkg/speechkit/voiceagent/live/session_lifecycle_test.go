package live

import (
	"context"
	"testing"
	"time"
)

func TestSessionStopReturnsWhenProviderCloseHangs(t *testing.T) {
	provider := &hangingCloseProvider{
		sessionTestProvider: newSessionTestProvider(),
		entered:             make(chan struct{}),
		release:             make(chan struct{}),
	}
	session := NewSession(provider, Callbacks{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{Model: "hang-close"}, IdleConfig{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		session.StopWithTimeout(80 * time.Millisecond)
	}()

	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("provider Close was never entered")
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Stop stayed blocked on provider Close")
	}
	if got := session.CurrentState(); got != StateInactive {
		t.Fatalf("state = %s, want inactive so the next Start can proceed", got)
	}
	close(provider.release)
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
	// The state machine transitions OutputTranscriptDone →
	// StateListening asynchronously. Without a poll the assertion races
	// the transition under -race (caught by CI in run 26278006120). Poll
	// up to 1 s so a slow state-machine tick doesn't flake the test;
	// production transitions complete in microseconds.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && session.CurrentState() != StateListening {
		time.Sleep(2 * time.Millisecond)
	}
	if session.CurrentState() != StateListening {
		t.Fatalf("current state = %s, want %s", session.CurrentState(), StateListening)
	}
}
