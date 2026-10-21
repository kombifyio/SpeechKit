package meeting

import (
	"context"
	"strings"
	"time"
)

// Watcher turns a stream of microphone readings into "a call just started".
//
// The debouncing matters more than the detection: a call that is announced
// twice, or announced again the moment the user declines, is worse than one
// that is missed. So a call is announced once, and the same application only
// becomes a candidate again after the microphone has been free for a while —
// which is what makes the second call of the day work without making the first
// one nag.
type Watcher struct {
	// Interval is how often the microphone is read. Reading it is a handful of
	// registry lookups, so this is cheap.
	Interval time.Duration
	// Rearm is how long an application must be off the microphone before a new
	// call from it is announced.
	Rearm time.Duration
	// Apps is the detection allowlist.
	Apps []string
	// Read returns the applications currently recording.
	Read func() ([]MicrophoneUser, error)
	// Announce is called once per detected call. It runs on the watch loop, so
	// anything slow belongs in a goroutine.
	Announce func(Detection)
	// Suspended reports whether detection should stand down — while a meeting is
	// already being recorded, most obviously.
	Suspended func() bool
	// Now is overridable for tests.
	Now func() time.Time

	announced map[string]time.Time
	lastSeen  map[string]time.Time
}

const (
	defaultDetectInterval = 5 * time.Second
	defaultDetectRearm    = 60 * time.Second
)

// Watch polls until the context ends.
func (w *Watcher) Watch(ctx context.Context) {
	if w == nil || w.Read == nil || w.Announce == nil {
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
func (w *Watcher) Poll() {
	if w.announced == nil {
		w.announced = map[string]time.Time{}
		w.lastSeen = map[string]time.Time{}
	}
	now := w.now()

	if w.Suspended != nil && w.Suspended() {
		// A meeting is already being recorded. Detection stands down, and the
		// applications on the call are forgotten so the next one is announced
		// normally.
		w.announced = map[string]time.Time{}
		w.lastSeen = map[string]time.Time{}
		return
	}

	users, err := w.Read()
	if err != nil {
		return
	}
	rearm := w.Rearm
	if rearm <= 0 {
		rearm = defaultDetectRearm
	}

	// Re-arming is measured from when the application was last seen holding the
	// microphone, not from when a poll first noticed it gone. Otherwise a
	// machine that slept, or simply polled less often, would keep an old call
	// pinned as "already announced" long after it ended.
	for _, user := range users {
		w.lastSeen[strings.ToLower(strings.TrimSpace(user.App))] = now
	}
	for app := range w.announced {
		seen, ok := w.lastSeen[app]
		if !ok {
			seen = w.announced[app]
		}
		if now.Sub(seen) >= rearm {
			delete(w.announced, app)
			delete(w.lastSeen, app)
		}
	}

	detection, found := DetectCall(users, CallApps(w.Apps))
	if !found {
		return
	}
	key := strings.ToLower(strings.TrimSpace(detection.App))
	if _, already := w.announced[key]; already {
		return
	}
	w.announced[key] = now
	w.Announce(detection)
}

func (w *Watcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}
