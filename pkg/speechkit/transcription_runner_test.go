package speechkit

import (
	"context"
	"testing"
	"time"
)

type testStore struct {
	quickNoteText           string
	savedTranscriptionText  string
	savedTranscriptionModel string
	updatedQuickNoteText    string
	savedQuickNoteText      string
	savedQuickNoteID        int64
}

type testCommitObserver struct {
	completions []Completion
}

func (o *testCommitObserver) OnCommit(completion Completion) {
	o.completions = append(o.completions, completion)
}

func (t *testStore) SaveQuickNote(_ context.Context, text string, _ string, _ string, _, _ int64, _ []byte) (int64, error) {
	t.savedQuickNoteText = text
	if t.savedQuickNoteID == 0 {
		t.savedQuickNoteID = 42
	}
	return t.savedQuickNoteID, nil
}

func (t *testStore) GetQuickNoteText(_ context.Context, _ int64) (string, error) {
	return t.quickNoteText, nil
}

func (t *testStore) UpdateQuickNote(_ context.Context, _ int64, text string) error {
	t.updatedQuickNoteText = text
	return nil
}

func (t *testStore) UpdateQuickNoteCapture(_ context.Context, _ int64, text, _ string, _, _ int64, _ []byte) error {
	t.updatedQuickNoteText = text
	return nil
}

func (t *testStore) SaveTranscription(_ context.Context, text string, _ string, _ string, model string, _, _ int64, _ []byte) error {
	t.savedTranscriptionText = text
	t.savedTranscriptionModel = model
	return nil
}

func TestTranscriptionRunnerPersistsTranscription(t *testing.T) {
	store := &testStore{}
	runner := NewTranscriptionRunner(nil, store)

	completion, err := runner.Commit(context.Background(), Submission{
		WAV:          []byte{1, 2, 3},
		DurationSecs: 1.2,
		Language:     "en",
	}, Transcript{
		Text:     "  hello world  ",
		Language: "en",
		Provider: "local",
		Model:    "ggml-small.bin",
		Duration: 1200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if got, want := completion.Transcript.Text, "hello world"; got != want {
		t.Fatalf("completion.Transcript.Text = %q, want %q", got, want)
	}
	if !completion.TranscriptionPersisted {
		t.Fatal("completion.TranscriptionPersisted = false, want true")
	}
	if got, want := store.savedTranscriptionText, "hello world"; got != want {
		t.Fatalf("saved transcription = %q, want %q", got, want)
	}
	if got, want := store.savedTranscriptionModel, "ggml-small.bin"; got != want {
		t.Fatalf("saved transcription model = %q, want %q", got, want)
	}
}

func TestTranscriptionRunnerUpdatesQuickNoteWithParagraphBreak(t *testing.T) {
	store := &testStore{quickNoteText: "first section"}
	runner := NewTranscriptionRunner(nil, store)

	completion, err := runner.Commit(context.Background(), Submission{
		WAV:         []byte{1, 2, 3},
		Language:    "en",
		Prefix:      "\n\n",
		QuickNote:   true,
		QuickNoteID: 7,
	}, Transcript{
		Text:     "second section",
		Language: "en",
		Provider: "local",
		Duration: time.Second,
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if !completion.QuickNoteCommitted {
		t.Fatal("completion.QuickNoteCommitted = false, want true")
	}
	if got, want := store.updatedQuickNoteText, "first section\n\nsecond section"; got != want {
		t.Fatalf("updated quick note = %q, want %q", got, want)
	}
}

func TestTranscriptionRunnerCreatesQuickNoteWithoutID(t *testing.T) {
	store := &testStore{}
	runner := NewTranscriptionRunner(nil, store)

	completion, err := runner.Commit(context.Background(), Submission{
		WAV:       []byte{4, 5, 6},
		Language:  "en",
		QuickNote: true,
	}, Transcript{
		Text:     "new note",
		Language: "en",
		Provider: "hf",
		Duration: 900 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if !completion.QuickNoteCommitted {
		t.Fatal("completion.QuickNoteCommitted = false, want true")
	}
	if !completion.QuickNoteCreated {
		t.Fatal("completion.QuickNoteCreated = false, want true")
	}
	if got, want := completion.QuickNoteID, int64(42); got != want {
		t.Fatalf("completion.QuickNoteID = %d, want %d", got, want)
	}
	if got, want := store.savedQuickNoteText, "new note"; got != want {
		t.Fatalf("saved quick note = %q, want %q", got, want)
	}
}

func TestTranscriptionRunnerWithoutStoreLeavesOutputPathOpen(t *testing.T) {
	runner := NewTranscriptionRunner(nil, nil)

	completion, err := runner.Commit(context.Background(), Submission{
		WAV:       []byte{7, 8, 9},
		Language:  "en",
		QuickNote: true,
	}, Transcript{
		Text:     "clipboard result",
		Language: "en",
		Provider: "local",
		Duration: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if completion.QuickNoteCommitted {
		t.Fatal("completion.QuickNoteCommitted = true, want false")
	}
	if completion.TranscriptionPersisted {
		t.Fatal("completion.TranscriptionPersisted = true, want false")
	}
}

func TestTranscriptionRunnerNotifiesObserverAfterPersistedTranscription(t *testing.T) {
	store := &testStore{}
	observer := &testCommitObserver{}
	runner := NewTranscriptionRunner(nil, store).WithObserver(observer)

	completion, err := runner.Commit(context.Background(), Submission{
		WAV:          []byte{1, 2, 3},
		DurationSecs: 1.2,
		Language:     "en",
	}, Transcript{
		Text:     "hello world",
		Language: "en",
		Provider: "local",
		Model:    "ggml-small.bin",
		Duration: 1200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if len(observer.completions) != 1 {
		t.Fatalf("observer completions = %d, want 1", len(observer.completions))
	}
	if got, want := observer.completions[0].Transcript.Text, completion.Transcript.Text; got != want {
		t.Fatalf("observer transcript text = %q, want %q", got, want)
	}
	if !observer.completions[0].TranscriptionPersisted {
		t.Fatal("observer completion.TranscriptionPersisted = false, want true")
	}
}

func TestTranscriptionRunnerNotifiesObserverAfterQuickNoteCommit(t *testing.T) {
	store := &testStore{quickNoteText: "first section"}
	observer := &testCommitObserver{}
	runner := NewTranscriptionRunner(nil, store).WithObserver(observer)

	completion, err := runner.Commit(context.Background(), Submission{
		WAV:         []byte{1, 2, 3},
		Language:    "en",
		Prefix:      "\n\n",
		QuickNote:   true,
		QuickNoteID: 7,
	}, Transcript{
		Text:     "second section",
		Language: "en",
		Provider: "local",
		Duration: time.Second,
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if len(observer.completions) != 1 {
		t.Fatalf("observer completions = %d, want 1", len(observer.completions))
	}
	if got, want := observer.completions[0].QuickNoteID, completion.QuickNoteID; got != want {
		t.Fatalf("observer quick note id = %d, want %d", got, want)
	}
	if !observer.completions[0].QuickNoteCommitted {
		t.Fatal("observer completion.QuickNoteCommitted = false, want true")
	}
}
