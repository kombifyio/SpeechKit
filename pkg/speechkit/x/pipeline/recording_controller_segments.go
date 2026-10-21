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
	c.streamQueue.enqueue(sessionID, segments)
	if _, err := c.flushPendingStreamSegments(sessionID, current, "dictation segment", time.Time{}); err != nil {
		if errors.Is(err, ErrWorkerQueueFull) {
			c.onLog("STT queue busy; dictation segment retained for retry", "warn")
			return
		}
		c.onLog(fmt.Sprintf("Queue error: %v", err), "error")
	}
}

func (c *RecordingController) flushPendingStreamSegments(sessionID uint64, current speechkit.RecordingStartOptions, label string, retryUntil time.Time) (int, error) {
	if c == nil {
		return 0, nil
	}
	for {
		acquired, active := c.streamQueue.beginFlush(sessionID)
		if !active {
			return 0, nil
		}
		if acquired {
			break
		}
		if retryUntil.IsZero() || !c.clockNow().Before(retryUntil) {
			return 0, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer c.streamQueue.endFlush(sessionID)

	c.mu.Lock()
	epoch := captureEpoch(current, c.startedAt)
	c.mu.Unlock()

	submitted := 0
	for {
		segment, ok := c.streamQueue.next(sessionID, c.minPCMBytes)
		if !ok {
			return submitted, nil
		}
		submission := submissionFromAudioSegment(segment, current, epoch, c.clockNow())
		if !c.streamQueue.beginSubmission(sessionID, &submission) {
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
		c.streamQueue.acknowledge(sessionID)
		submitted++
		c.onLog(fmt.Sprintf("Queued %s: %.1fs audio", label, submission.DurationSecs), "info")
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
