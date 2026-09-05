package speechkit

import (
	"errors"
	"strings"
	"sync/atomic"
)

// ErrOutputBlocked means the adapter refused output before sending any text.
// Other output errors may represent partial delivery and are not safe to retry
// automatically.
var ErrOutputBlocked = errors.New("speechkit: output blocked")

type RecognitionState string
type OutputState string
type PersistenceState string

const (
	RecognitionRecognized RecognitionState = "recognized"
	RecognitionEmpty      RecognitionState = "empty"
	RecognitionFailed     RecognitionState = "failed"

	OutputNotRequested OutputState = "not_requested"
	OutputRequested    OutputState = "requested"
	// OutputSubmitted only acknowledges the adapter's return, not receipt by
	// an application. In particular, SendInput cannot confirm insertion.
	OutputSubmitted OutputState = "submitted"
	OutputBlocked   OutputState = "blocked"
	OutputFailed    OutputState = "failed"

	PersistenceNotRequested PersistenceState = "not_requested"
	PersistencePending      PersistenceState = "pending"
	// PersistenceSaved acknowledges SaveTranscription under the host's
	// retention policy, not permanent retention or raw-audio storage.
	PersistenceSaved  PersistenceState = "saved"
	PersistenceFailed PersistenceState = "failed"
)

// TranscriptionFinalization separates recognition, output and history. ID is a
// process-local attempt identifier, stable across asynchronous history updates.
// Text is carried separately by DictationRun or TranscriptionFinalizationObserver.
type TranscriptionFinalization struct {
	ID          uint64           `json:"id"`
	Recognition RecognitionState `json:"recognition"`
	Output      OutputState      `json:"output"`
	Persistence PersistenceState `json:"persistence"`
}

var finalizationSequence atomic.Uint64

func NewTranscriptionFinalization(transcript Transcript, recognitionErr error, outputRequested, persistenceRequested bool) TranscriptionFinalization {
	f := TranscriptionFinalization{
		ID:          finalizationSequence.Add(1),
		Recognition: RecognitionRecognized,
		Output:      OutputNotRequested,
		Persistence: PersistenceNotRequested,
	}
	if recognitionErr != nil {
		f.Recognition = RecognitionFailed
		return f
	}
	if strings.TrimSpace(transcript.Text) == "" {
		f.Recognition = RecognitionEmpty
	} else if outputRequested {
		f.Output = OutputRequested
	}
	if persistenceRequested {
		f.Persistence = PersistencePending
	}
	return f
}

func (f TranscriptionFinalization) WithOutputResult(err error) TranscriptionFinalization {
	switch {
	case errors.Is(err, ErrOutputBlocked):
		f.Output = OutputBlocked
	case err != nil:
		f.Output = OutputFailed
	default:
		f.Output = OutputSubmitted
	}
	return f
}

func (f TranscriptionFinalization) WithPersistenceResult(err error) TranscriptionFinalization {
	if err != nil {
		f.Persistence = PersistenceFailed
	} else {
		f.Persistence = PersistenceSaved
	}
	return f
}

// TranscriptionFinalizationObserver receives recognition before output and a
// terminal output result before history I/O, then an optional history update.
// Observers must return promptly and support concurrent history updates. Target
// is opaque and must not be serialized. Notifications are not retry commands.
type TranscriptionFinalizationObserver interface {
	OnTranscriptionFinalized(transcript Transcript, finalization TranscriptionFinalization, target any)
}
