package live

import (
	"context"
	"testing"
	"time"
)

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

// TestSessionKeepsSpeakingUntilTurnCompleteWhenTranscriptPrecedesAudio pins the
// Deepgram Voice Agent ordering: the full agent transcript (OutputTranscriptDone)
// arrives BEFORE the TTS audio has finished streaming. A premature return to
// Listening on transcript-done truncated the spoken answer (companion buffers
// audio until the turn ends, then plays it). The session must stay in the turn
// until an explicit Done, so all audio chunks are captured.
func TestSessionKeepsSpeakingUntilTurnCompleteWhenTranscriptPrecedesAudio(t *testing.T) {
	provider := newSessionTestProvider()
	stateChanges := make(chan State, 16)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) { stateChanges <- state },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{Model: "deepgram-agent-test"}, IdleConfig{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer session.Stop()
	drainStateChanges(stateChanges)

	// Agent text lands first (Deepgram ConversationText), then audio streams.
	provider.messages <- &LiveMessage{
		OutputTranscript:     "die vollständige Antwort",
		OutputTranscriptDone: true,
	}
	provider.messages <- &LiveMessage{Audio: []byte{1, 2, 3, 4}}
	waitForState(t, stateChanges, StateSpeaking)

	// More audio chunks follow the transcript-done; the session must NOT have
	// returned to Listening in between (that would truncate playback).
	provider.messages <- &LiveMessage{Audio: []byte{5, 6, 7, 8}}
	assertNoStateWithin(t, stateChanges, StateListening, 150*time.Millisecond)
	if got := session.CurrentState(); got != StateSpeaking {
		t.Fatalf("state during audio = %s, want %s", got, StateSpeaking)
	}

	// Only the explicit turn-complete (AgentAudioDone -> Done) ends the turn.
	provider.messages <- &LiveMessage{Done: true}
	waitForState(t, stateChanges, StateListening)
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
	assertNoStateWithin(t, stateChanges, StateSpeaking, 50*time.Millisecond)

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
	assertNoStateWithin(t, stateChanges, StateProcessing, 50*time.Millisecond)

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
