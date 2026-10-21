package pipeline

import (
	"sync"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// streamSegmentQueue owns pending audio and flush progress for one recording
// generation. Its mutex is never held while submitting a job or notifying a host.
type streamSegmentQueue struct {
	mu        sync.Mutex
	sessionID uint64
	pending   []speechkit.AudioSegment
	flushing  bool
	sequence  uint64
	streamed  int
}

func (q *streamSegmentQueue) reset(sessionID uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.sessionID = sessionID
	q.pending = nil
	q.flushing = false
	q.sequence = 0
	q.streamed = 0
}

func (q *streamSegmentQueue) enqueue(sessionID uint64, segments []speechkit.AudioSegment) {
	if len(segments) == 0 {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.sessionID == sessionID {
		q.pending = append(q.pending, cloneAudioSegments(segments)...)
	}
}

func (q *streamSegmentQueue) beginFlush(sessionID uint64) (acquired, active bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.sessionID != sessionID {
		return false, false
	}
	if q.flushing {
		return false, true
	}
	q.flushing = true
	return true, true
}

// next skips undersized segments and returns an owned copy of the pending audio.
func (q *streamSegmentQueue) next(sessionID uint64, minPCMBytes int) (speechkit.AudioSegment, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.sessionID != sessionID {
		return speechkit.AudioSegment{}, false
	}
	for len(q.pending) > 0 {
		if len(q.pending[0].PCM) < minPCMBytes {
			q.dropFirst()
			continue
		}
		return cloneAudioSegments(q.pending[:1])[0], true
	}
	return speechkit.AudioSegment{}, false
}

// beginSubmission rechecks the generation after submission construction.
// A queue-full retry gets a new ID, matching the controller's existing behavior.
func (q *streamSegmentQueue) beginSubmission(sessionID uint64, submission *speechkit.Submission) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.sessionID != sessionID {
		return false
	}
	q.sequence++
	submission.SessionID = sessionID
	submission.SegmentID = q.sequence
	submission.SegmentFinal = true
	return true
}

func (q *streamSegmentQueue) acknowledge(sessionID uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.sessionID == sessionID {
		if len(q.pending) > 0 {
			q.dropFirst()
		}
		q.streamed++
	}
}

// dropFirst requires q.mu. Release the PCM reference before advancing the slice.
func (q *streamSegmentQueue) dropFirst() {
	q.pending[0].PCM = nil
	q.pending = q.pending[1:]
	if len(q.pending) == 0 {
		q.pending = nil
	}
}

func (q *streamSegmentQueue) endFlush(sessionID uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.sessionID == sessionID {
		q.flushing = false
	}
}

func (q *streamSegmentQueue) streamedCount(sessionID uint64) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.sessionID != sessionID {
		return 0
	}
	return q.streamed
}

func (q *streamSegmentQueue) discard(sessionID uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.sessionID == sessionID {
		q.pending = nil
		q.flushing = false
	}
}
