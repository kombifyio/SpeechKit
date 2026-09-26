package store

import (
	"strings"
	"time"
)

func normalizeRecordingSession(session RecordingSession) RecordingSession {
	now := time.Now().UTC()
	session.ExternalID = strings.TrimSpace(session.ExternalID)
	session.Title = strings.TrimSpace(session.Title)
	session.Language = strings.TrimSpace(session.Language)
	session.Provider = strings.TrimSpace(session.Provider)
	session.Model = strings.TrimSpace(session.Model)
	session.InputSource = strings.TrimSpace(session.InputSource)
	session.ProcessingMode = strings.TrimSpace(session.ProcessingMode)
	session.Summary = strings.TrimSpace(session.Summary)
	switch session.Kind {
	case RecordingSessionKindMeeting:
	default:
		session.Kind = RecordingSessionKindDictation
	}
	switch session.Status {
	case RecordingSessionStatusFinished, RecordingSessionStatusFailed:
	default:
		session.Status = RecordingSessionStatusActive
	}
	session.CaptureStatus = normalizeRecordingSessionCaptureStatus(session.CaptureStatus)
	session.SummaryStatus = normalizeRecordingSessionSummaryStatus(session.SummaryStatus)
	session.SummaryError = strings.TrimSpace(session.SummaryError)
	if session.StartedAt.IsZero() {
		session.StartedAt = now
	}
	return session
}

func normalizeRecordingSessionCaptureStatus(status RecordingSessionCaptureStatus) RecordingSessionCaptureStatus {
	switch status {
	case RecordingSessionCaptureRecording, RecordingSessionCapturePaused, RecordingSessionCaptureStopped:
		return status
	default:
		return RecordingSessionCaptureIdle
	}
}

func normalizeRecordingSessionSummaryStatus(status RecordingSessionSummaryStatus) RecordingSessionSummaryStatus {
	switch status {
	case RecordingSessionSummaryRunning, RecordingSessionSummaryReady, RecordingSessionSummaryFailed:
		return status
	default:
		return RecordingSessionSummaryIdle
	}
}

func nullableRecordingTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}
