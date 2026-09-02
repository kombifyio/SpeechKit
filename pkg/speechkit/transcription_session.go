package speechkit

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
