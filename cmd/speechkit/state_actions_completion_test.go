package main

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestApplyTranscriptionCompletionIncrementsCountForPersistedTranscription(t *testing.T) {
	state := &appState{}

	state.applyTranscriptionCompletion(speechkit.Completion{
		TranscriptionPersisted: true,
	})

	state.mu.Lock()
	defer state.mu.Unlock()
	if got, want := state.transcriptions, 1; got != want {
		t.Fatalf("state.transcriptions = %d, want %d", got, want)
	}
}

func TestApplyTranscriptionCompletionPublishesQuickNoteUpdateEvent(t *testing.T) {
	state := &appState{}
	state.engine = newSpeechKitRuntime(state, speechkit.Hooks{})
	defer state.engine.Close()

	state.applyTranscriptionCompletion(speechkit.Completion{
		Transcript: speechkit.Transcript{
			Text:     "updated quick note text",
			Provider: "hf",
		},
		QuickNoteCommitted: true,
		QuickNoteID:        7,
	})

	event, ok := <-state.engine.Events()
	if !ok {
		t.Fatal("engine events channel closed unexpectedly")
	}
	if got, want := event.Type, speechkit.EventQuickNoteUpdated; got != want {
		t.Fatalf("event.Type = %q, want %q", got, want)
	}
	if got, want := event.Text, "updated quick note text"; got != want {
		t.Fatalf("event.Text = %q, want %q", got, want)
	}
	if !event.QuickNote {
		t.Fatal("event.QuickNote = false, want true")
	}
}
