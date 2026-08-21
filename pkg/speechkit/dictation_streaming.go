package speechkit

import (
	"context"
	"errors"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// ErrUnsupportedAudioFormat is returned by a DictationStreamProvider when the
// requested speaker.AudioFormat is one its realtime API cannot accept — e.g.
// AssemblyAI's v3 streaming API exposes no channel parameter and decodes the
// socket as a single channel, so stereo would transcribe as braided garbage
// rather than fail. Providers wrap it with %w.
//
// Format support is per-provider, not a property of the format (Deepgram
// serves stereo natively), so this is deliberately not a protocol-level
// validation: the router's fallback loop treats it like any other start
// failure and tries the next candidate. Only when no provider can serve the
// format does it reach a caller, who should errors.Is it to report a format
// problem rather than invite a blind retry of the same doomed format.
var ErrUnsupportedAudioFormat = errors.New("speechkit: unsupported audio format for this dictation stream provider")

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
	// CaptureChannel names the capture source feeding this stream, and
	// CaptureEpoch is the wall clock the session's timeline is measured from.
	// Sinks stamp final events with the elapsed offset so parallel channels of
	// one meeting can be interleaved. A zero epoch disables the timeline.
	CaptureChannel string
	CaptureEpoch   time.Time
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
