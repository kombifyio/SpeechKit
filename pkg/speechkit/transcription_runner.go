package speechkit

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Transcriber converts raw WAV audio into a [Transcript].
type Transcriber interface {
	Transcribe(ctx context.Context, audio []byte, durationSecs float64, language string) (Transcript, error)
}

// Transcript holds the result of a single transcription call.
type Transcript struct {
	Text       string
	Language   string
	Duration   time.Duration
	Provider   string
	Model      string
	Confidence float64
}

// QuickNoteStore persists and retrieves Quick Note records.
type QuickNoteStore interface {
	SaveQuickNote(ctx context.Context, text, language, provider string, durationMs, latencyMs int64, audioData []byte) (int64, error)
	GetQuickNoteText(ctx context.Context, id int64) (string, error)
	UpdateQuickNote(ctx context.Context, id int64, text string) error
	UpdateQuickNoteCapture(ctx context.Context, id int64, text, provider string, durationMs, latencyMs int64, audioData []byte) error
}

// TranscriptionStore persists completed dictation transcriptions.
type TranscriptionStore interface {
	SaveTranscription(ctx context.Context, text, language, provider, model string, durationMs, latencyMs int64, audioData []byte) error
}

// Persistence combines [QuickNoteStore] and [TranscriptionStore].
type Persistence interface {
	QuickNoteStore
	TranscriptionStore
}

// CommitObserver is notified after each successful [TranscriptionRunner.Commit].
type CommitObserver interface {
	OnCommit(completion Completion)
}

// Submission carries a single audio segment and its metadata into the
// transcription pipeline.
type Submission struct {
	PCM          []byte
	WAV          []byte
	DurationSecs float64
	Language     string
	Prefix       string
	QuickNote    bool
	QuickNoteID  int64
}

// Completion describes the outcome of a [TranscriptionRunner.Commit] call.
type Completion struct {
	Transcript             Transcript
	QuickNoteCommitted     bool
	QuickNoteCreated       bool
	QuickNoteID            int64
	TranscriptionPersisted bool
}

// TranscriptionRunner transcribes audio submissions and persists results.
// Create one with [NewTranscriptionRunner].
type TranscriptionRunner struct {
	transcriber Transcriber
	store       Persistence
	observer    CommitObserver
}

// NewTranscriptionRunner creates a TranscriptionRunner backed by the given
// transcriber and persistence store. Either argument may be nil.
func NewTranscriptionRunner(transcriber Transcriber, store Persistence) *TranscriptionRunner {
	return &TranscriptionRunner{
		transcriber: transcriber,
		store:       store,
	}
}

func (r *TranscriptionRunner) WithObserver(observer CommitObserver) *TranscriptionRunner {
	if r == nil {
		return nil
	}
	r.observer = observer
	return r
}

func (r *TranscriptionRunner) Commit(ctx context.Context, submission Submission, transcript Transcript) (Completion, error) {
	if r == nil {
		return Completion{}, ErrMissingRunner
	}

	transcript.Text = normalizeTranscriptText(transcript.Text, submission.Prefix)
	completion := Completion{
		Transcript: transcript,
	}

	durationMs := int64(submission.DurationSecs * 1000)
	latencyMs := transcript.Duration.Milliseconds()
	if submission.QuickNote && r.store != nil {
		if submission.QuickNoteID > 0 {
			existing, err := r.store.GetQuickNoteText(ctx, submission.QuickNoteID)
			if err != nil {
				return Completion{}, fmt.Errorf("lookup quick note %d: %w", submission.QuickNoteID, err)
			}

			nextText := mergeStoredQuickNoteText(existing, transcript.Text, submission.Prefix != "")
			if err := r.store.UpdateQuickNoteCapture(ctx, submission.QuickNoteID, nextText, transcript.Provider, durationMs, latencyMs, submission.WAV); err != nil {
				return Completion{}, fmt.Errorf("update quick note %d: %w", submission.QuickNoteID, err)
			}

			completion.QuickNoteCommitted = true
			completion.QuickNoteID = submission.QuickNoteID
			r.notifyCommit(completion)
			return completion, nil
		}

		noteID, err := r.store.SaveQuickNote(ctx, transcript.Text, transcript.Language, transcript.Provider, durationMs, latencyMs, submission.WAV)
		if err != nil {
			return Completion{}, fmt.Errorf("save quick note: %w", err)
		}

		completion.QuickNoteCommitted = true
		completion.QuickNoteCreated = true
		completion.QuickNoteID = noteID
		r.notifyCommit(completion)
		return completion, nil
	}

	if r.store != nil {
		if err := r.store.SaveTranscription(ctx, transcript.Text, transcript.Language, transcript.Provider, transcript.Model, durationMs, latencyMs, submission.WAV); err != nil {
			return Completion{}, fmt.Errorf("save transcription: %w", err)
		}
		completion.TranscriptionPersisted = true
	}

	r.notifyCommit(completion)
	return completion, nil
}

func (r *TranscriptionRunner) notifyCommit(completion Completion) {
	if r == nil || r.observer == nil {
		return
	}
	r.observer.OnCommit(completion)
}

func normalizeTranscriptText(text, prefix string) string {
	text = strings.TrimSpace(text)
	if prefix != "" && text != "" {
		return prefix + text
	}
	return text
}

func mergeStoredQuickNoteText(existing, addition string, paragraph bool) string {
	existing = strings.TrimSpace(existing)
	addition = strings.TrimSpace(addition)

	if addition == "" {
		return existing
	}
	if existing == "" {
		return addition
	}
	if paragraph {
		return existing + "\n\n" + addition
	}
	return existing + " " + addition
}
