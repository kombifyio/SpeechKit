package pipeline

import (
	"errors"
	"fmt"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func (c *RecordingController) drainAndSubmitReadySegments(sessionID uint64, current speechkit.RecordingStartOptions, collector speechkit.SegmentCollector) {
	readyCollector, ok := collector.(ReadySegmentCollector)
	if !ok {
		return
	}
	segments := readyCollector.DrainReadySegments()
	c.queueStreamSegments(sessionID, segments)
	if _, err := c.flushPendingStreamSegments(sessionID, current, "dictation segment", time.Time{}); err != nil {
		if errors.Is(err, ErrWorkerQueueFull) {
			c.onLog("STT queue busy; dictation segment retained for retry", "warn")
			return
		}
		c.onLog(fmt.Sprintf("Queue error: %v", err), "error")
	}
}

func (c *RecordingController) queueStreamSegments(sessionID uint64, segments []speechkit.AudioSegment) {
	if c == nil || len(segments) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionID != sessionID {
		return
	}
	c.streamPending = append(c.streamPending, cloneAudioSegments(segments)...)
}

func (c *RecordingController) flushPendingStreamSegments(sessionID uint64, current speechkit.RecordingStartOptions, label string, retryUntil time.Time) (int, error) {
	if c == nil {
		return 0, nil
	}
	for {
		c.mu.Lock()
		if !c.streamFlush {
			c.streamFlush = true
			c.mu.Unlock()
			break
		}
		c.mu.Unlock()
		if retryUntil.IsZero() || !c.clockNow().Before(retryUntil) {
			return 0, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer func() {
		c.mu.Lock()
		c.streamFlush = false
		c.mu.Unlock()
	}()

	c.mu.Lock()
	epoch := captureEpoch(current, c.startedAt)
	c.mu.Unlock()

	submitted := 0
	for {
		c.mu.Lock()
		if c.sessionID != sessionID {
			c.streamPending = nil
			c.mu.Unlock()
			return submitted, nil
		}
		if len(c.streamPending) == 0 {
			c.mu.Unlock()
			return submitted, nil
		}
		segment := cloneAudioSegments(c.streamPending[:1])[0]
		c.mu.Unlock()

		if len(segment.PCM) < c.minPCMBytes {
			c.dropFirstPendingStreamSegment(sessionID)
			continue
		}

		submission := submissionFromAudioSegment(segment, current, epoch, c.clockNow())
		sessionActive := true
		c.mu.Lock()
		if c.sessionID == sessionID {
			c.streamSegmentSeq++
			submission.SessionID = sessionID
			submission.SegmentID = c.streamSegmentSeq
			submission.SegmentFinal = true
		} else {
			sessionActive = false
		}
		c.mu.Unlock()
		if !sessionActive {
			return submitted, nil
		}
		if err := c.submitter.Submit(speechkit.TranscriptionJob{
			Submission: submission,
			Target:     current.Target,
		}); err != nil {
			if errors.Is(err, ErrWorkerQueueFull) && !retryUntil.IsZero() && c.clockNow().Before(retryUntil) {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return submitted, err
		}
		c.dropFirstPendingStreamSegment(sessionID)
		submitted++
		c.mu.Lock()
		if c.sessionID == sessionID {
			c.streamedCount++
		}
		c.mu.Unlock()
		c.onLog(fmt.Sprintf("Queued %s: %.1fs audio", label, submission.DurationSecs), "info")
	}
}

func (c *RecordingController) dropFirstPendingStreamSegment(sessionID uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionID != sessionID || len(c.streamPending) == 0 {
		return
	}
	c.streamPending[0].PCM = nil
	c.streamPending = c.streamPending[1:]
	if len(c.streamPending) == 0 {
		c.streamPending = nil
	}
}

// submissionFromAudioSegment converts one pause-bounded segment into a
// submission. capturedEnd is the wall clock the segment was closed at, which is
// the best available anchor for a VAD segment: its audio ends there, so the
// timeline offsets run backwards from it by the segment duration.
func submissionFromAudioSegment(segment speechkit.AudioSegment, current speechkit.RecordingStartOptions, epoch, capturedEnd time.Time) speechkit.Submission {
	prefix := ""
	if segment.Paragraph {
		prefix = "\n\n"
	}
	durationSecs := segment.Duration.Seconds()
	if durationSecs <= 0 {
		durationSecs = speechkit.PCMDurationSecs(segment.PCM)
	}
	endMs := elapsedCaptureMs(epoch, capturedEnd)
	startMs := endMs - int64(durationSecs*1000)
	if startMs < 0 {
		startMs = 0
	}
	return speechkit.Submission{
		PCM:                append([]byte(nil), segment.PCM...),
		DurationSecs:       durationSecs,
		Language:           current.Language,
		Prefix:             prefix,
		QuickNote:          current.QuickNote,
		QuickNoteID:        current.QuickNoteID,
		RecordingSessionID: current.RecordingSessionID,
		CaptureChannel:     current.CaptureChannel,
		CapturedStartMs:    startMs,
		CapturedEndMs:      endMs,
	}
}

// captureEpoch resolves the wall clock a session's transcript timeline is
// measured from, defaulting to the start of this recording.
func captureEpoch(current speechkit.RecordingStartOptions, startedAt time.Time) time.Time {
	if !current.CaptureEpoch.IsZero() {
		return current.CaptureEpoch
	}
	return startedAt
}

// elapsedCaptureMs places at on the timeline that starts at epoch. A zero epoch
// or timestamp means no timeline was requested, which keeps every offset at 0.
func elapsedCaptureMs(epoch, at time.Time) int64 {
	if epoch.IsZero() || at.IsZero() {
		return 0
	}
	ms := at.Sub(epoch).Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}
