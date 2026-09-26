package pipeline

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestRecordingControllerIdleWatcherFiresOnSilence(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	collector := &fakeIdleCollector{}
	// Pin idleSince well into the past so the watcher's first tick
	// already sees the timeout exceeded.
	collector.setIdleSince(time.Now().Add(-5 * time.Second))

	controller := NewRecordingController(recorder, submitter, observer, func() speechkit.SegmentCollector {
		return collector
	})
	controller.SetIdleWatchInterval(5 * time.Millisecond)

	var fired atomic.Int32
	if err := controller.Start(speechkit.RecordingStartOptions{
		Language:    "en",
		IdleTimeout: 100 * time.Millisecond,
		OnIdleTimeoutCallback: func() {
			fired.Add(1)
		},
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fired.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("idle callback fired %d times, want 1", fired.Load())
	}

	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRecordingControllerIdleWatcherSkipsWhileSpeechActive(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	collector := &fakeIdleCollector{}
	// Zero IdleSince === "currently speaking" — watcher must not fire.
	collector.setIdleSince(time.Time{})

	controller := NewRecordingController(recorder, submitter, observer, func() speechkit.SegmentCollector {
		return collector
	})
	controller.SetIdleWatchInterval(5 * time.Millisecond)

	var fired atomic.Int32
	if err := controller.Start(speechkit.RecordingStartOptions{
		Language:    "en",
		IdleTimeout: 50 * time.Millisecond,
		OnIdleTimeoutCallback: func() {
			fired.Add(1)
		},
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Give the watcher a generous window to incorrectly fire.
	time.Sleep(200 * time.Millisecond)

	if fired.Load() != 0 {
		t.Fatalf("idle callback fired while speech active (fired=%d)", fired.Load())
	}

	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRecordingControllerAudioIdleWatcherFiresOnAudioSilence(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	collector := &fakeAudioIdleCollector{}
	collector.setIdleAudio(5*time.Second, time.Now())

	controller := NewRecordingController(recorder, submitter, observer, func() speechkit.SegmentCollector {
		return collector
	})
	controller.SetIdleWatchInterval(5 * time.Millisecond)

	var fired atomic.Int32
	if err := controller.Start(speechkit.RecordingStartOptions{
		Language:    "en",
		IdleTimeout: 100 * time.Millisecond,
		OnIdleTimeoutCallback: func() {
			fired.Add(1)
		},
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fired.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("idle callback fired %d times, want 1", fired.Load())
	}

	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRecordingControllerAudioIdleWatcherIgnoresWallClockStall(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	collector := &fakeAudioIdleCollector{}
	// Frame delivery stalled right after a short pause: audio silence is
	// pinned below the timeout while wall-clock time keeps passing.
	collector.setIdleAudio(10*time.Millisecond, time.Now())

	controller := NewRecordingController(recorder, submitter, observer, func() speechkit.SegmentCollector {
		return collector
	})
	controller.SetIdleWatchInterval(5 * time.Millisecond)

	var fired atomic.Int32
	if err := controller.Start(speechkit.RecordingStartOptions{
		Language:    "en",
		IdleTimeout: 50 * time.Millisecond,
		OnIdleTimeoutCallback: func() {
			fired.Add(1)
		},
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Many wall-clock timeouts pass; the watcher must not fire because
	// the *audio* has not been silent long enough.
	time.Sleep(300 * time.Millisecond)
	if fired.Load() != 0 {
		t.Fatalf("idle callback fired on wall-clock stall (fired=%d)", fired.Load())
	}

	// Frames catch up in a burst and push audio silence past the
	// timeout — now the watcher must fire exactly once.
	collector.setIdleAudio(60*time.Millisecond, time.Now())
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fired.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("idle callback fired %d times after burst, want 1", fired.Load())
	}

	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRecordingControllerAudioIdleWatcherDeadCaptureBackstop(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	collector := &fakeAudioIdleCollector{}

	start := time.Unix(2000, 0)
	collector.setIdleAudio(0, start.Add(-time.Second))

	var clock sync.Mutex
	current := start
	controller := NewRecordingController(recorder, submitter, observer, func() speechkit.SegmentCollector {
		return collector
	})
	controller.now = func() time.Time {
		clock.Lock()
		defer clock.Unlock()
		return current
	}
	controller.SetIdleWatchInterval(5 * time.Millisecond)

	var fired atomic.Int32
	if err := controller.Start(speechkit.RecordingStartOptions{
		Language:    "en",
		IdleTimeout: 100 * time.Millisecond,
		OnIdleTimeoutCallback: func() {
			fired.Add(1)
		},
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Below the 30s floor nothing may fire, even though the timeout is
	// long exceeded in wall-clock terms.
	time.Sleep(100 * time.Millisecond)
	if fired.Load() != 0 {
		t.Fatalf("backstop fired below the floor (fired=%d)", fired.Load())
	}

	clock.Lock()
	current = start.Add(31 * time.Second)
	clock.Unlock()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fired.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("dead-capture backstop fired %d times, want 1", fired.Load())
	}

	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRecordingControllerStopClearsIdleWatcher(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	collector := &fakeIdleCollector{}
	collector.setIdleSince(time.Now().Add(-5 * time.Second))

	controller := NewRecordingController(recorder, submitter, observer, func() speechkit.SegmentCollector {
		return collector
	})
	controller.SetIdleWatchInterval(5 * time.Millisecond)

	var fired atomic.Int32
	if err := controller.Start(speechkit.RecordingStartOptions{
		Language:    "en",
		IdleTimeout: 5 * time.Second, // long enough that Stop wins the race
		OnIdleTimeoutCallback: func() {
			fired.Add(1)
		},
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Stop immediately — the watcher should not get a chance to fire.
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Wait a few watcher ticks past the stop to make sure a stale fire
	// did not leak through.
	time.Sleep(100 * time.Millisecond)
	if fired.Load() != 0 {
		t.Fatalf("idle callback fired after Stop (fired=%d)", fired.Load())
	}
}
