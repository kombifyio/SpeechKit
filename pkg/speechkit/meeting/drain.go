package meeting

import (
	"context"
	"fmt"
	"time"
)

const (
	defaultDrainTimeout = 20 * time.Second
	defaultDrainPoll    = 100 * time.Millisecond
	// drainStallWindow is how long the end of a meeting waits with nothing
	// landing before it accepts that the outstanding segments are not coming.
	drainStallWindow = 8 * time.Second
)

// NoteSegmentSubmitted records that a segment of this meeting entered the
// transcription queue, so Stop knows what it is waiting for.
func (r *Runtime) NoteSegmentSubmitted(sessionID int64) {
	r.adjustPending(sessionID, 1)
}

// NoteSegmentCommitted records that a submitted segment has been persisted.
func (r *Runtime) NoteSegmentCommitted(sessionID int64) {
	r.adjustPending(sessionID, -1)
}

func (r *Runtime) adjustPending(sessionID int64, delta int) {
	if r == nil || sessionID <= 0 {
		return
	}
	r.mu.Lock()
	if r.active == nil || r.active.sessionID != sessionID {
		r.mu.Unlock()
		return
	}
	r.active.pending += delta
	if r.active.pending < 0 {
		r.active.pending = 0
	}
	snapshot := r.snapshotLocked()
	subscribers := r.subscriberList()
	r.mu.Unlock()
	broadcast(subscribers, snapshot)
}

// waitForDrain blocks until every submitted segment has been committed, the
// caller gives up, or waiting stops being worthwhile.
//
// Two bounds, because a segment can go missing rather than slow: a transcript
// the worker drops never reports back at all, and waiting the full timeout for
// it would stall the end of every meeting it happens in. So the wait also gives
// up once nothing has landed for drainStallWindow, while a provider that keeps
// delivering gets the whole timeout.
func (r *Runtime) waitForDrain(ctx context.Context, capture *meetingCapture) {
	deadline := r.now().Add(r.drainTimeout)
	lastProgress := r.now()
	previous := -1
	for {
		r.mu.Lock()
		pending := capture.pending
		r.mu.Unlock()
		if pending <= 0 {
			return
		}
		now := r.now()
		if pending != previous {
			previous = pending
			lastProgress = now
		}
		stalled := now.Sub(lastProgress) >= drainStallWindow
		if stalled || !now.Before(deadline) {
			reason := "still transcribing when the meeting ended"
			if stalled {
				reason = "never came back from transcription"
			}
			r.log(fmt.Sprintf("Meeting capture: %d segment(s) %s", pending, reason), "warn")
			r.markDrainDegraded(capture)
			return
		}
		select {
		case <-ctx.Done():
			r.markDrainDegraded(capture)
			return
		case <-time.After(r.drainPoll):
		}
	}
}

func (r *Runtime) markDrainDegraded(capture *meetingCapture) {
	r.mu.Lock()
	capture.degraded = capture.pending > 0
	r.mu.Unlock()
}
