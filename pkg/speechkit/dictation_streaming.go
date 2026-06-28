package speechkit

import (
	"context"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// DictationStreamOptions configures provider-native live transcription for
// Dictation and meeting transcription. It is intentionally separate from
// speaker.Options because plain dictation must not require diarization.
type DictationStreamOptions struct {
	SessionID         uint64
	ProviderProfileID string
	Language          string
	Model             string
	InterimResults    bool
	EndpointingMs     int
	TurnDetection     bool
	Keyterms          []string
	PromptHint        string
	Diarization       bool
}

// DictationStreamProvider is implemented by STT providers that can consume
// raw PCM frames and emit draft/final transcript events without waiting for a
// completed WAV upload.
type DictationStreamProvider interface {
	StartDictationStream(ctx context.Context, opts DictationStreamOptions, format speaker.AudioFormat) (DictationStream, error)
}

// DictationStreamSink consumes provider-native live dictation events. Drafts
// are allowed to update UI state, but only final events may reach output or
// persistence.
type DictationStreamSink interface {
	HandleDictationStreamEvent(ctx context.Context, event DictationStreamEvent, opts DictationStreamSinkOptions) error
}

// DictationStreamSinkOptions carries host metadata needed to commit final
// provider-stream events through the same path as batch transcription.
type DictationStreamSinkOptions struct {
	Target             any
	QuickNote          bool
	QuickNoteID        int64
	Language           string
	RecordingSessionID int64
}

// DictationStream is a provider-neutral realtime dictation session.
type DictationStream interface {
	SendPCM(ctx context.Context, pcm []byte) error
	Finalize(ctx context.Context) error
	Receive(ctx context.Context) (DictationStreamEvent, error)
	Close() error
}

// DictationStreamEvent is the provider-neutral event emitted by
// DictationStream.Receive.
type DictationStreamEvent struct {
	Sequence       int64
	SessionID      uint64
	SegmentID      uint64
	ProviderItemID string
	Text           string
	IsFinal        bool
	Language       string
	Provider       string
	Model          string
	Confidence     float64
	Words          []WordConfidence
	Speakers       *speaker.DiarizationResult
}

func (e DictationStreamEvent) Transcript() Transcript {
	return Transcript{
		Text:           e.Text,
		Language:       e.Language,
		Provider:       e.Provider,
		Model:          e.Model,
		Confidence:     e.Confidence,
		SessionID:      e.SessionID,
		SegmentID:      e.SegmentID,
		ProviderItemID: e.ProviderItemID,
		SegmentFinal:   e.IsFinal,
		Words:          append([]WordConfidence(nil), e.Words...),
		Speakers:       e.Speakers,
	}
}
