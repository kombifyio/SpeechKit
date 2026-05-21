package main

import (
	"testing"

	"go.uber.org/goleak"
)

func TestDesktopCleanupStackRunsInReverseRegistrationOrder(t *testing.T) {
	var calls []string
	var cleanup desktopCleanupStack
	cleanup.Add(func() { calls = append(calls, "first") })
	cleanup.Add(func() { calls = append(calls, "second") })

	cleanup.Close()

	if len(calls) != 2 || calls[0] != "second" || calls[1] != "first" {
		t.Fatalf("cleanup calls = %v, want [second first]", calls)
	}
}

// TestDesktopCleanupStackEmitsShutdownSummary verifies the
// `desktop.shutdown.complete` log line that kombify-SpeechKit-hfj
// added. Named and unnamed cleanup callbacks must both contribute to
// cleanup_calls; named_calls must count only the named ones; the
// duration_ms attribute must be present and non-negative.
func TestDesktopCleanupStackEmitsShutdownSummary(t *testing.T) {
	buf := captureSlog(t)

	var cleanup desktopCleanupStack
	cleanup.AddNamed("audio", func() {})
	cleanup.Add(func() {}) // unnamed for backward compatibility
	cleanup.AddNamed("router", func() {})

	cleanup.Close()

	rec := findLog(t, buf, "desktop.shutdown.complete")
	if rec == nil {
		t.Fatalf("expected desktop.shutdown.complete summary log, got:\n%s", buf.String())
	}
	if got, ok := rec["cleanup_calls"].(float64); !ok || int(got) != 3 {
		t.Errorf("cleanup_calls = %v, want 3", rec["cleanup_calls"])
	}
	if got, ok := rec["named_calls"].(float64); !ok || int(got) != 2 {
		t.Errorf("named_calls = %v, want 2", rec["named_calls"])
	}
	if got, ok := rec["errors"].(float64); !ok || int(got) != 0 {
		t.Errorf("errors = %v, want 0", rec["errors"])
	}
	if _, ok := rec["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms attribute missing: %v", rec)
	}
}

// TestDesktopCleanupStackSurvivesPanic verifies that one panicking
// cleanup callback does not orphan the later ones — kombify-SpeechKit-hfj
// requires "any error count" to be reported, not for the stack to
// short-circuit. The post-panic survivor must still run, and the
// summary log line must report errors >= 1.
func TestDesktopCleanupStackSurvivesPanic(t *testing.T) {
	buf := captureSlog(t)

	survivorRan := false
	var cleanup desktopCleanupStack
	cleanup.AddNamed("survivor", func() { survivorRan = true })
	cleanup.AddNamed("crasher", func() { panic("oops") })

	cleanup.Close()

	if !survivorRan {
		t.Fatalf("expected 'survivor' callback to run after 'crasher' panicked")
	}
	rec := findLog(t, buf, "desktop.shutdown.complete")
	if rec == nil {
		t.Fatalf("expected desktop.shutdown.complete summary log, got:\n%s", buf.String())
	}
	if got, ok := rec["errors"].(float64); !ok || int(got) < 1 {
		t.Errorf("errors = %v, want >= 1 (the panic)", rec["errors"])
	}
}

// TestDesktopCleanupStackEmptyStillEmitsSummary covers the operator-
// facing contract that the summary log line ALWAYS fires, even when
// no cleanup callbacks were registered. Operators reading post-
// shutdown logs use this line to confirm the stack ran.
func TestDesktopCleanupStackEmptyStillEmitsSummary(t *testing.T) {
	buf := captureSlog(t)

	var cleanup desktopCleanupStack
	cleanup.Close()

	rec := findLog(t, buf, "desktop.shutdown.complete")
	if rec == nil {
		t.Fatalf("expected desktop.shutdown.complete summary log on empty stack, got:\n%s", buf.String())
	}
	if got, ok := rec["cleanup_calls"].(float64); !ok || int(got) != 0 {
		t.Errorf("cleanup_calls = %v on empty stack, want 0", rec["cleanup_calls"])
	}
}

// TestDesktopCleanupStackNoGoroutineLeak verifies the goleak contract
// from the kombify-SpeechKit-hfj acceptance: the cleanup stack itself
// must not leak goroutines on its own surface. The full app.Shutdown
// path needs a Wails event loop and cannot run as a unit test, but
// the stack semantics are the load-bearing piece that future
// regressions are most likely to break.
func TestDesktopCleanupStackNoGoroutineLeak(t *testing.T) {
	defer goleak.VerifyNone(t)

	var cleanup desktopCleanupStack
	for i := 0; i < 5; i++ {
		cleanup.AddNamed("dummy", func() {})
	}
	cleanup.Close()
}
