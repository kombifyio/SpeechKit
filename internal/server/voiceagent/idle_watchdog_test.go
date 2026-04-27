//go:build linux

package voiceagent

import (
	"testing"
	"time"
)

func TestIdleWatchdog_FiresAfterTimeout(t *testing.T) {
	w := newIdleWatchdog(20 * time.Millisecond)
	defer w.Stop()

	select {
	case <-w.Fired():
		// expected
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("watchdog did not fire within 200ms grace period")
	}
}

func TestIdleWatchdog_ResetPreventsFire(t *testing.T) {
	w := newIdleWatchdog(80 * time.Millisecond)
	defer w.Stop()

	// Reset every 30ms for 200ms total — no fire should happen.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		w.Reset()
		time.Sleep(30 * time.Millisecond)
		select {
		case <-w.Fired():
			t.Fatalf("watchdog fired despite frequent Reset")
		default:
		}
	}
}

func TestIdleWatchdog_ResetThenIdleFires(t *testing.T) {
	w := newIdleWatchdog(50 * time.Millisecond)
	defer w.Stop()

	// Reset once, then wait long enough for the next timeout to elapse.
	time.Sleep(20 * time.Millisecond)
	w.Reset()

	select {
	case <-w.Fired():
		// expected — Reset extended but did not stop
	case <-time.After(300 * time.Millisecond):
		t.Fatalf("watchdog should fire after Reset+idle period")
	}
}

func TestIdleWatchdog_StopSuppressesFire(t *testing.T) {
	w := newIdleWatchdog(30 * time.Millisecond)
	w.Stop()

	select {
	case <-w.Fired():
		t.Fatalf("Stop should prevent fire")
	case <-time.After(100 * time.Millisecond):
		// expected
	}
}

func TestIdleWatchdog_ZeroTimeoutNeverFires(t *testing.T) {
	w := newIdleWatchdog(0)
	defer w.Stop()

	select {
	case <-w.Fired():
		t.Fatalf("zero-timeout watchdog must never fire")
	case <-time.After(80 * time.Millisecond):
		// expected
	}
}

func TestIdleWatchdog_StopIsIdempotent(t *testing.T) {
	w := newIdleWatchdog(50 * time.Millisecond)
	w.Stop()
	w.Stop() // must not panic
}

func TestIdleWatchdog_ResetAfterStopIsNoop(t *testing.T) {
	w := newIdleWatchdog(50 * time.Millisecond)
	w.Stop()
	w.Reset() // must not panic and must not arm the timer

	select {
	case <-w.Fired():
		t.Fatalf("Reset after Stop must not re-arm the watchdog")
	case <-time.After(80 * time.Millisecond):
		// expected
	}
}
