package meeting

import (
	"context"
	"time"
)

// EndWatcher turns a stream of microphone readings into "the call just ended".
//
// It is the mirror of Watcher: where Watcher waits for a calling application
// to take the microphone, EndWatcher waits — only while a meeting is being
// recorded — for every calling application to let go of it and stay gone. The
// grace period matters more than the detection: calls drop the microphone for
// a few seconds when a headset reconnects or the user switches devices, and
// ending the recording on such a blip would truncate a meeting that is still
// going. A recording that runs a minute long is a small annoyance; one that
// cut off the decisions at the end of the meeting is useless.
type EndWatcher struct {
	// Interval is how often the microphone is read.
	Interval time.Duration
	// Grace is how long the microphone must stay free of calling applications
	// before the call is considered over.
	Grace time.Duration
	// Apps is the detection allowlist, same as Watcher's.
	Apps []string
	// Read returns the applications currently recording.
	Read func() ([]MicrophoneUser, error)
	// Recording reports whether a meeting is currently being recorded. The
	// watcher only ever acts while this is true.
	Recording func() bool
	// End is called at most once per recording, when the call is over. It runs
	// on the watch loop, so anything slow belongs in a goroutine.
	End func()
	// Now is overridable for tests.
	Now func() time.Time

	sawCall  bool
	lastCall time.Time
	ended    bool
}

const defaultEndGrace = 45 * time.Second

// Watch polls until the context ends.
func (w *EndWatcher) Watch(ctx context.Context) {
	if w == nil || w.Read == nil || w.End == nil || w.Recording == nil {
		return
	}
	interval := w.Interval
	if interval <= 0 {
		interval = defaultDetectInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Poll()
		}
	}
}

// Poll performs one reading. Exported so the behaviour can be driven directly
// in tests without waiting on a ticker.
func (w *EndWatcher) Poll() {
	now := w.now()

	if w.Recording == nil || !w.Recording() {
		// No recording, nothing to end. Forget the previous recording so the
		// next one starts with a clean slate.
		w.sawCall = false
		w.ended = false
		return
	}
	if w.ended {
		// Already ended this recording; stopping drains asynchronously, so the
		// capture may report active for a while longer. Don't end it twice.
		return
	}

	users, err := w.Read()
	if err != nil {
		return
	}
	if _, found := DetectCall(users, CallApps(w.Apps)); found {
		w.sawCall = true
		w.lastCall = now
		return
	}
	if !w.sawCall {
		// The recording never overlapped with a recognized call — an in-person
		// meeting recorded by hand, most likely. Never end those automatically.
		return
	}

	grace := w.Grace
	if grace <= 0 {
		grace = defaultEndGrace
	}
	if now.Sub(w.lastCall) < grace {
		return
	}
	w.ended = true
	w.End()
}

func (w *EndWatcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}
