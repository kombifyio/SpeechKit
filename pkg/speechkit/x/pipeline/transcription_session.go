package pipeline

import (
	"sync"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// TranscriptSessionLedger suppresses duplicate final commits for progressive
// transcription. It tracks both in-flight and completed segments so repeated
// Stop/Finalize/provider events cannot paste the same text twice.
type TranscriptSessionLedger struct {
	mu        sync.Mutex
	inFlight  map[speechkit.TranscriptSegmentKey]struct{}
	committed map[speechkit.TranscriptSegmentKey]struct{}
}

// NewTranscriptSessionLedger returns an empty ledger. A nil
// *TranscriptSessionLedger is also usable: every call admits the segment.
func NewTranscriptSessionLedger() *TranscriptSessionLedger {
	return &TranscriptSessionLedger{}
}

// Begin reserves key for an in-flight commit and reports whether the caller
// may proceed. It returns false when the key was already committed or is
// being committed by another job. Zero keys (see
// [speechkit.TranscriptSegmentKey.IsZero]) and nil ledgers always admit.
func (l *TranscriptSessionLedger) Begin(key speechkit.TranscriptSegmentKey) bool {
	if l == nil || key.IsZero() {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight == nil {
		l.inFlight = map[speechkit.TranscriptSegmentKey]struct{}{}
	}
	if l.committed == nil {
		l.committed = map[speechkit.TranscriptSegmentKey]struct{}{}
	}
	if _, ok := l.committed[key]; ok {
		return false
	}
	if _, ok := l.inFlight[key]; ok {
		return false
	}
	l.inFlight[key] = struct{}{}
	return true
}

// Commit marks key as committed and drops its in-flight reservation; later
// Begin calls for the key return false.
func (l *TranscriptSessionLedger) Commit(key speechkit.TranscriptSegmentKey) {
	if l == nil || key.IsZero() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.committed == nil {
		l.committed = map[speechkit.TranscriptSegmentKey]struct{}{}
	}
	delete(l.inFlight, key)
	l.committed[key] = struct{}{}
}

// Release drops the in-flight reservation taken by Begin without committing,
// so a segment whose transcription failed can be retried.
func (l *TranscriptSessionLedger) Release(key speechkit.TranscriptSegmentKey) {
	if l == nil || key.IsZero() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.inFlight, key)
}

// EndRecordingSession releases deduplication state once a durable recording
// session has ended. Controllers reconstructed while the recording is active
// still share the ledger; completed meetings cannot grow it without bound.
func (l *TranscriptSessionLedger) EndRecordingSession(recordingSessionID int64) {
	if l == nil || recordingSessionID <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for key := range l.inFlight {
		if key.RecordingSessionID == recordingSessionID {
			delete(l.inFlight, key)
		}
	}
	for key := range l.committed {
		if key.RecordingSessionID == recordingSessionID {
			delete(l.committed, key)
		}
	}
}

func transcriptSegmentKey(submission speechkit.Submission) speechkit.TranscriptSegmentKey {
	if submission.RecordingSessionID == 0 && submission.SessionID == 0 && submission.SegmentID == 0 && submission.ProviderItemID == "" {
		return speechkit.TranscriptSegmentKey{}
	}
	return speechkit.TranscriptSegmentKey{
		RecordingSessionID: submission.RecordingSessionID,
		CaptureChannel:     submission.CaptureChannel,
		SessionID:          submission.SessionID,
		SegmentID:          submission.SegmentID,
		ProviderItemID:     submission.ProviderItemID,
	}
}

func applyTranscriptSessionMetadata(transcript *speechkit.Transcript, submission speechkit.Submission) {
	if transcript == nil {
		return
	}
	transcript.SessionID = submission.SessionID
	transcript.SegmentID = submission.SegmentID
	transcript.ProviderItemID = submission.ProviderItemID
	transcript.SegmentFinal = submission.SegmentFinal
	transcript.RecordingSessionID = submission.RecordingSessionID
	transcript.CaptureChannel = submission.CaptureChannel
	transcript.CapturedStartMs = submission.CapturedStartMs
	transcript.CapturedEndMs = submission.CapturedEndMs
}
