package speechkit

import "sync"

// TranscriptSegmentKey uniquely identifies a final transcript unit inside a
// progressive dictation or meeting session.
//
// CaptureChannel is part of the identity because a meeting records several
// sources at once and each source numbers its own segments from one. Without it
// the two channels collide on their first segment and one of them is discarded
// as a duplicate of the other. RecordingSessionID separates fresh controllers,
// whose process-local counters restart from one, across durable meetings.
type TranscriptSegmentKey struct {
	RecordingSessionID int64
	CaptureChannel     string
	SessionID          uint64
	SegmentID          uint64
	ProviderItemID     string
}

func (k TranscriptSegmentKey) IsZero() bool {
	return k.RecordingSessionID == 0 && k.SessionID == 0 && k.SegmentID == 0 && k.ProviderItemID == ""
}

// TranscriptSessionLedger suppresses duplicate final commits for progressive
// transcription. It tracks both in-flight and completed segments so repeated
// Stop/Finalize/provider events cannot paste the same text twice.
type TranscriptSessionLedger struct {
	mu        sync.Mutex
	inFlight  map[TranscriptSegmentKey]struct{}
	committed map[TranscriptSegmentKey]struct{}
}

func NewTranscriptSessionLedger() *TranscriptSessionLedger {
	return &TranscriptSessionLedger{}
}

func (l *TranscriptSessionLedger) Begin(key TranscriptSegmentKey) bool {
	if l == nil || key.IsZero() {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight == nil {
		l.inFlight = map[TranscriptSegmentKey]struct{}{}
	}
	if l.committed == nil {
		l.committed = map[TranscriptSegmentKey]struct{}{}
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

func (l *TranscriptSessionLedger) Commit(key TranscriptSegmentKey) {
	if l == nil || key.IsZero() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.committed == nil {
		l.committed = map[TranscriptSegmentKey]struct{}{}
	}
	delete(l.inFlight, key)
	l.committed[key] = struct{}{}
}

func (l *TranscriptSessionLedger) Release(key TranscriptSegmentKey) {
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

func transcriptSegmentKey(submission Submission) TranscriptSegmentKey {
	if submission.RecordingSessionID == 0 && submission.SessionID == 0 && submission.SegmentID == 0 && submission.ProviderItemID == "" {
		return TranscriptSegmentKey{}
	}
	return TranscriptSegmentKey{
		RecordingSessionID: submission.RecordingSessionID,
		CaptureChannel:     submission.CaptureChannel,
		SessionID:          submission.SessionID,
		SegmentID:          submission.SegmentID,
		ProviderItemID:     submission.ProviderItemID,
	}
}

func applyTranscriptSessionMetadata(transcript *Transcript, submission Submission) {
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
