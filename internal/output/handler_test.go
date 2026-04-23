package output

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/stt"
)

// TestClipboardHandlerImplementsOutputHandler is a compile-time guard via
// interface assignment. If the ClipboardHandler's signature drifts from the
// OutputHandler contract, this test file fails to compile.
func TestClipboardHandlerImplementsOutputHandler(t *testing.T) {
	t.Parallel()
	var _ OutputHandler = (*ClipboardHandler)(nil)
	var _ OutputHandler = NewClipboardHandler()
}

func TestNewClipboardHandlerReturnsNonNil(t *testing.T) {
	t.Parallel()
	if NewClipboardHandler() == nil {
		t.Fatal("NewClipboardHandler() returned nil")
	}
}

func TestClipboardHandlerHandleNilResult(t *testing.T) {
	t.Parallel()
	// Guard path: nil *stt.Result must short-circuit before any Windows
	// clipboard API is touched. Safe to run cross-platform.
	h := NewClipboardHandler()
	if err := h.Handle(context.Background(), nil, Target{}); err != nil {
		t.Errorf("Handle(nil) = %v, want nil (no-op)", err)
	}
}

func TestClipboardHandlerHandleEmptyText(t *testing.T) {
	t.Parallel()
	// Guard path: empty-text Result must short-circuit before Windows APIs.
	h := NewClipboardHandler()
	if err := h.Handle(context.Background(), &stt.Result{Text: ""}, Target{}); err != nil {
		t.Errorf("Handle(empty) = %v, want nil (no-op)", err)
	}
}

func TestTargetZeroValue(t *testing.T) {
	t.Parallel()
	var tgt Target
	if tgt.HWND != 0 {
		t.Errorf("Target{}.HWND = %v, want 0", tgt.HWND)
	}
}
