package output

import (
	"context"
	"testing"
	"time"
)

func TestCaptureSelectedTextRestoresClipboard(t *testing.T) {
	originalGet := selectionGetClipboardText
	originalSet := selectionSetClipboardText
	originalCopy := selectionTriggerCopy
	originalSleep := selectionPause
	defer func() {
		selectionGetClipboardText = originalGet
		selectionSetClipboardText = originalSet
		selectionTriggerCopy = originalCopy
		selectionPause = originalSleep
	}()

	calls := 0
	selectionGetClipboardText = func() (string, bool) {
		calls++
		if calls == 1 {
			return "clipboard", true
		}
		return "selected text", true
	}
	var restored string
	selectionSetClipboardText = func(text string) error {
		restored = text
		return nil
	}
	selectionTriggerCopy = func() {}
	selectionPause = func(time.Duration) {}

	got, err := CaptureSelectedText(context.Background())
	if err != nil {
		t.Fatalf("CaptureSelectedText() error = %v", err)
	}
	if got != "selected text" {
		t.Fatalf("CaptureSelectedText() = %q, want %q", got, "selected text")
	}
	if restored != "clipboard" {
		t.Fatalf("restored clipboard = %q, want %q", restored, "clipboard")
	}
}
