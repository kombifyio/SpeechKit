package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// TranscriptionRunner transcribes audio submissions and persists results.
// Create one with [NewTranscriptionRunner].
type TranscriptionRunner struct {
	transcriber speechkit.Transcriber
	store       speechkit.Persistence
	observer    speechkit.CommitObserver
}

// NewTranscriptionRunner creates a TranscriptionRunner backed by the given
// transcriber and persistence store. Either argument may be nil.
func NewTranscriptionRunner(transcriber speechkit.Transcriber, store speechkit.Persistence) *TranscriptionRunner {
	return &TranscriptionRunner{
		transcriber: transcriber,
		store:       store,
	}
}

func (r *TranscriptionRunner) WithObserver(observer speechkit.CommitObserver) *TranscriptionRunner {
	if r == nil {
		return nil
	}
	r.observer = observer
	return r
}

func (r *TranscriptionRunner) Commit(ctx context.Context, submission speechkit.Submission, transcript speechkit.Transcript) (speechkit.Completion, error) {
	if r == nil {
		return speechkit.Completion{}, ErrMissingRunner
	}

	transcript.Text = normalizeTranscriptText(transcript.Text, submission.Prefix)
	completion := speechkit.Completion{
		Transcript:      transcript,
		AudioDurationMs: int64(submission.DurationSecs * 1000),
	}

	durationMs := completion.AudioDurationMs
	latencyMs := transcript.Duration.Milliseconds()
	if submission.QuickNote && r.store != nil {
		if submission.QuickNoteID > 0 {
			existing, err := r.store.GetQuickNoteText(ctx, submission.QuickNoteID)
			if err != nil {
				return speechkit.Completion{}, fmt.Errorf("lookup quick note %d: %w", submission.QuickNoteID, err)
			}

			nextText := mergeStoredQuickNoteText(existing, transcript.Text, submission.Prefix != "")
			if err := r.store.UpdateQuickNoteCapture(ctx, submission.QuickNoteID, nextText, transcript.Provider, durationMs, latencyMs, submission.WAV); err != nil {
				return speechkit.Completion{}, fmt.Errorf("update quick note %d: %w", submission.QuickNoteID, err)
			}

			completion.QuickNoteCommitted = true
			completion.QuickNoteID = submission.QuickNoteID
			r.notifyCommit(completion)
			return completion, nil
		}

		noteID, err := r.store.SaveQuickNote(ctx, transcript.Text, transcript.Language, transcript.Provider, durationMs, latencyMs, submission.WAV)
		if err != nil {
			return speechkit.Completion{}, fmt.Errorf("save quick note: %w", err)
		}

		completion.QuickNoteCommitted = true
		completion.QuickNoteCreated = true
		completion.QuickNoteID = noteID
		r.notifyCommit(completion)
		return completion, nil
	}

	if r.store != nil {
		if err := r.store.SaveTranscription(ctx, transcript.Text, transcript.Language, transcript.Provider, transcript.Model, durationMs, latencyMs, persistableAudio(submission)); err != nil {
			return speechkit.Completion{}, fmt.Errorf("save transcription: %w", err)
		}
		completion.TranscriptionPersisted = true
	}

	r.notifyCommit(completion)
	return completion, nil
}

func (r *TranscriptionRunner) notifyCommit(completion speechkit.Completion) {
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

// persistableAudio returns the audio a transcript may be stored with.
//
// A capture that names a channel comes from a recording with several sources at
// once, which today means a meeting - and a meeting never keeps its audio. It
// records other people, often without them being in a position to consent to a
// recording, so what is kept is what was said, not the sound of them saying it.
//
// The rule lives on this path rather than in a host setting because it is not a
// preference: every persistence route runs through here, so none of them can
// quietly opt out of it.
func persistableAudio(submission speechkit.Submission) []byte {
	if submission.CaptureChannel != "" {
		return nil
	}
	return submission.WAV
}

// deliverableAsOutput reports whether a transcript should be typed into the
// user's focused application.
//
// Dictation is text the user is writing, so it goes where they are typing. A
// recording is not: a capture that names a channel comes from a meeting, and
// meeting speech belongs in the meeting, not pasted into whatever window
// happens to have focus - which in practice was the note window, so the other
// side of the call ended up inside the user's own notes.
func deliverableAsOutput(submission speechkit.Submission) bool {
	return submission.CaptureChannel == ""
}
