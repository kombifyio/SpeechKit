package speechkit

import "sync"

// TranscriptSegmentKey uniquely identifies a final transcript unit inside a
// progressive dictation or meeting session.
type TranscriptSegmentKey struct {
	SessionID      uint64
	SegmentID      uint64
	ProviderItemID string
}

func (k TranscriptSegmentKey) IsZero() bool {
	return k.SessionID == 0 && k.SegmentID == 0 && k.ProviderItemID == ""
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

func transcriptSegmentKey(submission Submission) TranscriptSegmentKey {
	if submission.SessionID == 0 && submission.SegmentID == 0 && submission.ProviderItemID == "" {
		return TranscriptSegmentKey{}
	}
	return TranscriptSegmentKey{
		SessionID:      submission.SessionID,
		SegmentID:      submission.SegmentID,
		ProviderItemID: submission.ProviderItemID,
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
}
