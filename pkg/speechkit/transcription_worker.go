package speechkit

import (
	"context"
)

const DefaultProcessingMessage = "Recording stopped · Transcribing"

// EmptyFinalTranscriptMessage is shown when a provider returns a successful
// final transcript containing no text.
//
// This is a named outcome rather than a silent drop because it is the visible
// half of a real data-loss bug: a provider answers HTTP 200 with a zero-length
// transcript when the pinned language does not match the speech, so the user's
// words disappear with nothing to alert on. The message names the most likely
// cause, since that is the one the user can act on.
const EmptyFinalTranscriptMessage = "No speech recognized · check the configured language"

// TranscriptOutput delivers a completed [Transcript] to the host application
// (e.g. clipboard injection or text-field paste).
type TranscriptOutput interface {
	Deliver(ctx context.Context, transcript Transcript, target any) error
}

// TranscriptInterceptor can handle a transcript before it reaches the normal
// output path. Return (true, nil) to signal that the transcript was consumed.
type TranscriptInterceptor interface {
	Intercept(ctx context.Context, transcript Transcript, target any) (bool, error)
}

// TranscriptTransformer can apply final post-STT changes after all audio
// segments have been transcribed and merged, but before command routing or
// user-visible output.
type TranscriptTransformer interface {
	Transform(ctx context.Context, transcript Transcript) (Transcript, error)
}

// TranscriptionObserver receives real-time status and log updates from a
// [TranscriptionWorker] during processing.
type TranscriptionObserver interface {
	OnState(status, text string)
	OnLog(message, kind string)
	OnTranscriptCommitted(transcript Transcript, quickNote bool)
}

// TranscriptionDraftObserver is optionally implemented by observers that can
// surface live provider draft text. Drafts are never passed to output handlers.
type TranscriptionDraftObserver interface {
	OnTranscriptDraft(transcript Transcript)
}

// TranscriptionJob pairs a [Submission] with its delivery target.
type TranscriptionJob struct {
	Submission
	Segments []Submission
	Target   any
}

func (j TranscriptionJob) Clone() TranscriptionJob {
	clone := j
	clone.Submission = cloneSubmission(j.Submission)
	if j.Segments != nil {
		clone.Segments = make([]Submission, 0, len(j.Segments))
		for _, segment := range j.Segments {
			clone.Segments = append(clone.Segments, cloneSubmission(segment))
		}
	}
	return clone
}

func cloneSubmission(submission Submission) Submission {
	clone := submission
	if submission.PCM != nil {
		clone.PCM = append([]byte(nil), submission.PCM...)
	}
	if submission.WAV != nil {
		clone.WAV = append([]byte(nil), submission.WAV...)
	}
	return clone
}

// EffectiveSegments returns the submissions the worker should transcribe:
// Segments when the recorder produced several, otherwise the single Submission.
func (j TranscriptionJob) EffectiveSegments() []Submission {
	if len(j.Segments) > 0 {
		return j.Segments
	}
	return []Submission{j.Submission}
}
