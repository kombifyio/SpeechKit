package speechkit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// Transcriber converts raw WAV audio into a [Transcript].
type Transcriber interface {
	Transcribe(ctx context.Context, audio []byte, durationSecs float64, language string) (Transcript, error)
}

// WordConfidence is a recognized word with the provider's per-word acoustic
// confidence in [0,1]. It mirrors stt.WordConfidence (the kernel keeps Transcript
// decoupled from the stt package; stt.AsTranscriber is the public bridge that
// maps between them). nil when the provider does not expose word-level
// confidence.
type WordConfidence struct {
	Text       string
	Confidence float64
	StartMs    int64
	EndMs      int64
}

// CustomizationAction describes a command/snippet/template action produced by
// Words and Replacements v2. Known command intents can be executed by hosts;
// unknown intents remain structured metadata for event/API consumers.
type CustomizationAction struct {
	ReplacementID string         `json:"replacement_id,omitempty"`
	Kind          string         `json:"kind,omitempty"`
	Intent        string         `json:"intent,omitempty"`
	Text          string         `json:"text,omitempty"`
	Template      string         `json:"template,omitempty"`
	Payload       map[string]any `json:"payload,omitempty"`
	MatchedText   string         `json:"matched_text,omitempty"`
	Count         int            `json:"count,omitempty"`
}

// Transcript holds the result of a single transcription call.
type Transcript struct {
	Text       string
	Language   string
	Duration   time.Duration
	Provider   string
	Model      string
	Confidence float64
	// Session metadata is set for progressive dictation/meeting pipelines.
	// Draft transcripts may be replaced by later revisions; only final
	// segment IDs are committed by the worker ledger.
	SessionID      uint64
	SegmentID      uint64
	ProviderItemID string
	SegmentFinal   bool
	// RecordingSessionID links this transcript to a persisted long-running
	// dictation or meeting session in the host store.
	RecordingSessionID int64
	// CaptureChannel names the capture source this transcript came from (see
	// CaptureChannel*). Sessions that record more than one source at once —
	// meeting capture runs the microphone and the system loopback in parallel —
	// use it to keep the two apart. Empty for single-source captures.
	CaptureChannel string
	// CapturedStartMs and CapturedEndMs place this transcript on the capture
	// session's wall-clock timeline, relative to RecordingStartOptions.
	// CaptureEpoch. Both are zero when the host did not request a timeline.
	CapturedStartMs int64
	CapturedEndMs   int64
	// Words carries per-word acoustic confidence when available (Deepgram,
	// AssemblyAI). Used to surface likely-misrecognized terms; nil otherwise.
	Words                []WordConfidence
	Speakers             *speaker.DiarizationResult
	CustomizationActions []CustomizationAction `json:"customization_actions,omitempty"`
}

// LowConfidenceWords returns the distinct word texts whose per-word confidence
// is below threshold, together with the minimum confidence observed across all
// words. A threshold <= 0 disables detection. Words without per-word data
// (providers that do not expose it) yield (nil, 0). The returned terms are the
// raw STT tokens, so callers can match them against the (possibly rewritten)
// display text without depending on character offsets.
func LowConfidenceWords(words []WordConfidence, threshold float64) (terms []string, minConfidence float64) {
	if threshold <= 0 || len(words) == 0 {
		return nil, 0
	}
	minConfidence = words[0].Confidence
	seen := map[string]struct{}{}
	for _, w := range words {
		if w.Confidence < minConfidence {
			minConfidence = w.Confidence
		}
		text := strings.TrimSpace(w.Text)
		if text == "" || w.Confidence >= threshold {
			continue
		}
		key := strings.ToLower(text)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		terms = append(terms, text)
	}
	return terms, minConfidence
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
	SessionID    uint64
	SegmentID    uint64
	// RecordingSessionID is copied into the final Transcript and Completion so
	// host observers can attach committed text to a long-running session after
	// persistence/output succeeds.
	RecordingSessionID int64
	// CaptureChannel, CapturedStartMs and CapturedEndMs carry the capture
	// source and wall-clock placement through to the Transcript. See the
	// matching Transcript fields.
	CaptureChannel  string
	CapturedStartMs int64
	CapturedEndMs   int64
	// ProviderItemID carries provider-native turn/item IDs for realtime
	// streams. Segment-batch jobs leave it empty and rely on SessionID+SegmentID.
	ProviderItemID string
	SegmentFinal   bool
	// QueuedAt is set by TranscriptionWorker.Submit when the segment enters
	// the worker queue. Hosts can prefill it when replaying externally queued
	// work, but ordinary callers should leave it zero.
	QueuedAt time.Time
}

// Completion describes the outcome of a [TranscriptionRunner.Commit] call.
type Completion struct {
	Transcript             Transcript
	QuickNoteCommitted     bool
	QuickNoteCreated       bool
	QuickNoteID            int64
	TranscriptionPersisted bool
	AudioDurationMs        int64
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
		Transcript:      transcript,
		AudioDurationMs: int64(submission.DurationSecs * 1000),
	}

	durationMs := completion.AudioDurationMs
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
		if err := r.store.SaveTranscription(ctx, transcript.Text, transcript.Language, transcript.Provider, transcript.Model, durationMs, latencyMs, persistableAudio(submission)); err != nil {
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
func persistableAudio(submission Submission) []byte {
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
func deliverableAsOutput(submission Submission) bool {
	return submission.CaptureChannel == ""
}
