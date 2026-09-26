// Package meeting owns the runtime behind Meeting 2.0 capture.
//
// A meeting is recorded from two sources at once: the microphone carries the
// local speaker and a host-provided system channel carries everyone else on the
// call. The two are deliberately never mixed into one stream — mixing would
// need clock-drift compensation and echo cancellation, while two independent
// pipelines give the same result plus a free speaker split, and interleave on a
// shared wall clock afterwards.
//
// The runtime owns the lifecycle of those pipelines and is the single source of
// truth for what capture is doing right now. Hosts subscribe to snapshots
// rather than inferring state from the outcome of the last command.
package meeting

import (
	"context"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// Runtime drives meeting capture. The zero value is not usable; call New.
type Runtime struct {
	newPipeline  PipelineFactory
	log          func(string, string)
	now          func() time.Time
	drainTimeout time.Duration
	drainPoll    time.Duration
	onEnded      func(int64)

	mu          sync.Mutex
	active      *meetingCapture
	subscribers map[int]chan Snapshot
	nextSubID   int
}

type meetingCapture struct {
	sessionID int64
	title     string
	epoch     time.Time
	state     State
	recording speechkit.RecordingStartOptions
	pipelines []Pipeline
	channels  map[string]*ChannelSnapshot
	watchStop context.CancelFunc
	pending   int
	degraded  bool
}

// New creates a meeting runtime.
func New(opts Options) *Runtime {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Log == nil {
		opts.Log = func(string, string) {}
	}
	if opts.DrainTimeout <= 0 {
		opts.DrainTimeout = defaultDrainTimeout
	}
	if opts.DrainPoll <= 0 {
		opts.DrainPoll = defaultDrainPoll
	}
	return &Runtime{
		newPipeline:  opts.NewPipeline,
		log:          opts.Log,
		now:          opts.Now,
		drainTimeout: opts.DrainTimeout,
		drainPoll:    opts.DrainPoll,
		onEnded:      opts.OnEnded,
		subscribers:  map[int]chan Snapshot{},
	}
}

// SetEndedHook installs the terminal hook after construction, for hosts whose
// hook needs the runtime it belongs to.
func (r *Runtime) SetEndedHook(hook func(sessionID int64)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onEnded = hook
}

// SetPipelineFactory installs the factory after construction. Hosts need this
// because the pipelines report their in-flight transcription back to the very
// runtime they belong to, so one of the two has to exist first.
func (r *Runtime) SetPipelineFactory(factory PipelineFactory) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.newPipeline = factory
}

// ActiveSessionID returns the recording session being captured, or 0.
func (r *Runtime) ActiveSessionID() int64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		return 0
	}
	return r.active.sessionID
}

// Snapshot returns the current runtime view. The zero SessionID with StateIdle
// means nothing is being recorded.
func (r *Runtime) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{State: StateIdle}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked()
}

// SnapshotFor returns the runtime view for one session, or false when that
// session is not the one being recorded.
func (r *Runtime) SnapshotFor(sessionID int64) (Snapshot, bool) {
	if r == nil {
		return Snapshot{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil || r.active.sessionID != sessionID {
		return Snapshot{}, false
	}
	return r.snapshotLocked(), true
}

// ElapsedMs returns the session being captured and the current offset on its
// transcript timeline, in milliseconds. The offset is wall-clock time since
// the capture epoch — the same time base transcript segments are stamped with
// — so a screen capture taken now lands next to the words spoken now. Pauses
// do not stop this clock, because they do not stop the segment clock either.
// ok is false when nothing is being captured.
func (r *Runtime) ElapsedMs() (sessionID int64, elapsedMs int64, ok bool) {
	if r == nil {
		return 0, 0, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil || r.active.state == StateEnded {
		return 0, 0, false
	}
	ms := r.now().Sub(r.active.epoch).Milliseconds()
	if ms < 0 {
		ms = 0
	}
	return r.active.sessionID, ms, true
}
