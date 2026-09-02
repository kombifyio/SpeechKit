package speechkit

import (
	"context"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// Transcriber converts raw WAV audio into a [Transcript].
//
// It is the host-facing contract: the dictation runtime, TranscriptionWorker
// and pipelines consume a Transcriber and nothing more specific. Provider
// implementations satisfy stt.STTProvider instead and are bridged with
// stt.AsTranscriber; a host never implements both.
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
