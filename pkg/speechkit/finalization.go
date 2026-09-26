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

// OutputBlockReason is implemented by ErrOutputBlocked wrappers that can say
// why no text was sent, as a short phrase that is safe to show and to log: it
// never carries transcript text, window titles or clipboard content. Hosts
// surface it next to the "output was not confirmed" notice so a user can tell
// a window that closed from a window that was not in front.
type OutputBlockReason interface {
	OutputBlockReason() string
}

// OutputBlockReasonOf returns the phrase carried by err, or "" when err is
// nil or names no reason.
func OutputBlockReasonOf(err error) string {
	var reason OutputBlockReason
	if errors.As(err, &reason) {
		return reason.OutputBlockReason()
	}
	return ""
}

// RecognitionState says whether a transcription attempt produced text.
type RecognitionState string

// OutputState tracks delivery of recognized text to the host output.
type OutputState string

// PersistenceState tracks the history write of recognized text.
type PersistenceState string

// States of the three finalization axes. A [TranscriptionFinalization] starts
// as recognized / not_requested / not_requested; [NewTranscriptionFinalization]
// sets the recognition verdict and the requested and pending states, and the
// WithOutputResult and WithPersistenceResult methods record the outcomes.
const (
	RecognitionRecognized RecognitionState = "recognized"
	// RecognitionEmpty means the provider succeeded but returned no text, so
	// output is never requested.
	RecognitionEmpty  RecognitionState = "empty"
	RecognitionFailed RecognitionState = "failed"

	OutputNotRequested OutputState = "not_requested"
	// OutputRequested means the text will be handed to the output adapter.
	OutputRequested OutputState = "requested"
	// OutputSubmitted only acknowledges the adapter's return, not receipt by
	// an application. In particular, SendInput cannot confirm insertion.
	OutputSubmitted OutputState = "submitted"
	// OutputBlocked means the adapter refused before sending any text (see
	// [ErrOutputBlocked]); OutputFailed covers every other delivery error.
	OutputBlocked OutputState = "blocked"
	OutputFailed  OutputState = "failed"

	PersistenceNotRequested PersistenceState = "not_requested"
	// PersistencePending means a history write was requested and has not
	// completed yet.
	PersistencePending PersistenceState = "pending"
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

// NewTranscriptionFinalization starts a finalization record with a fresh
// process-local ID. A non-nil recognitionErr yields RecognitionFailed with
// nothing requested; blank transcript text yields RecognitionEmpty. Otherwise
// Output becomes OutputRequested when outputRequested, and Persistence becomes
// PersistencePending when persistenceRequested (also for empty transcripts).
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

// WithOutputResult returns a copy recording the output adapter's result:
// OutputBlocked for an error wrapping [ErrOutputBlocked], OutputFailed for any
// other error, OutputSubmitted for nil.
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

// WithPersistenceResult returns a copy recording the history write result:
// PersistenceFailed for a non-nil err, PersistenceSaved otherwise.
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
