package output

import (
	"context"
	"errors"
	"testing"
	"time"
)

// installSelectionStubs replaces the selection package-level hooks with
// test-controlled values and returns a restore function. Call `defer restore()`.
func installSelectionStubs(t *testing.T) (
	getCalls *int,
	getFn *func() (string, bool),
	setFn *func(string) error,
	copyFn *func(),
	pauseFn *func(time.Duration),
) {
	t.Helper()
	origGet := selectionGetClipboardText
	origSet := selectionSetClipboardText
	origCopy := selectionTriggerCopy
	origPause := selectionPause
	t.Cleanup(func() {
		selectionGetClipboardText = origGet
		selectionSetClipboardText = origSet
		selectionTriggerCopy = origCopy
		selectionPause = origPause
	})
	calls := 0
	get := func() (string, bool) { return "", false }
	set := func(string) error { return nil }
	copy := func() {}
	pause := func(time.Duration) {}
	selectionGetClipboardText = func() (string, bool) { calls++; return get() }
	selectionSetClipboardText = func(s string) error { return set(s) }
	selectionTriggerCopy = func() { copy() }
	selectionPause = func(d time.Duration) { pause(d) }
	return &calls, &get, &set, &copy, &pause
}

func TestCaptureSelectedTextNoClipboardBackup(t *testing.T) {
	// Not parallel: replaces package vars.
	calls, get, set, _, _ := installSelectionStubs(t)
	*get = func() (string, bool) {
		if *calls == 1 {
			return "", false // no existing clipboard content
		}
		return "selection text", true
	}
	setCalls := 0
	*set = func(text string) error {
		setCalls++
		return nil
	}
	got, err := CaptureSelectedText(context.Background())
	if err != nil {
		t.Fatalf("CaptureSelectedText = %v, want nil", err)
	}
	if got != "selection text" {
		t.Errorf("got %q, want %q", got, "selection text")
	}
	if setCalls != 0 {
		t.Errorf("setClipboardText called %d times, want 0 (no backup to restore)", setCalls)
	}
}

func TestCaptureSelectedTextTrimsWhitespace(t *testing.T) {
	// Not parallel: replaces package vars.
	calls, get, _, _, _ := installSelectionStubs(t)
	*get = func() (string, bool) {
		if *calls == 1 {
			return "", false
		}
		return "   padded text\n\t", true
	}
	got, err := CaptureSelectedText(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "padded text" {
		t.Errorf("got %q, want %q (whitespace should be trimmed)", got, "padded text")
	}
}

func TestCaptureSelectedTextEmptySelection(t *testing.T) {
	// Not parallel: replaces package vars.
	calls, get, _, _, _ := installSelectionStubs(t)
	*get = func() (string, bool) {
		if *calls == 1 {
			return "", false
		}
		return "", true // Ctrl+C produced no selection
	}
	got, err := CaptureSelectedText(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestCaptureSelectedTextRestoreFailureIsSwallowed(t *testing.T) {
	// Not parallel: replaces package vars.
	// setClipboardText returning an error during restore must not propagate —
	// the goal is to return the captured selection; restore best-effort only.
	_, get, set, _, _ := installSelectionStubs(t)
	*get = func() (string, bool) { return "original", true }
	*set = func(string) error { return errors.New("clipboard busy") }
	got, err := CaptureSelectedText(context.Background())
	if err != nil {
		t.Fatalf("restore error should be swallowed, got: %v", err)
	}
	if got != "original" {
		// Both get-calls return "original" in this stub; selected text equals backup.
		t.Errorf("got %q, want %q", got, "original")
	}
}

func TestCaptureSelectedTextTriggersCopyExactlyOnce(t *testing.T) {
	// Not parallel: replaces package vars.
	_, get, _, copyFn, _ := installSelectionStubs(t)
	*get = func() (string, bool) { return "x", true }
	copyCalls := 0
	*copyFn = func() { copyCalls++ }
	if _, err := CaptureSelectedText(context.Background()); err != nil {
		t.Fatal(err)
	}
	if copyCalls != 1 {
		t.Errorf("simulateCtrlC called %d times, want 1", copyCalls)
	}
}
