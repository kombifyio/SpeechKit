package output

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/kombifyio/SpeechKit/internal/stt"
)

func TestChoosePasteWindowPrefersCapturedTarget(t *testing.T) {
	target := windows.Handle(42)
	fallback := windows.Handle(99)
	if got := choosePasteWindow(target, fallback); got != target {
		t.Fatalf("choosePasteWindow() = %v, want %v", got, target)
	}
}

func TestChoosePasteWindowFallsBackToCurrentFocus(t *testing.T) {
	fallback := windows.Handle(99)
	if got := choosePasteWindow(0, fallback); got != fallback {
		t.Fatalf("choosePasteWindow() = %v, want %v", got, fallback)
	}
}

func TestClipboardHandlerPastesAndRestoresClipboardBackup(t *testing.T) {
	installClipboardHandlerStubs(t)

	getCalls := 0
	clipboardGetText = func() (string, bool) {
		getCalls++
		return "previous clipboard", true
	}
	setValues := []string{}
	clipboardSetText = func(text string) error {
		setValues = append(setValues, text)
		return nil
	}
	var restored windows.Handle
	clipboardRestoreForegroundWindow = func(hwnd windows.Handle) { restored = hwnd }
	pasteCalls := 0
	clipboardSimulateCtrlV = func() { pasteCalls++ }
	var pauseDuration time.Duration
	clipboardPause = func(d time.Duration) { pauseDuration = d }

	err := NewClipboardHandler().Handle(context.Background(), &stt.Result{Text: "new transcript"}, Target{HWND: windows.Handle(42)})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if getCalls != 1 {
		t.Fatalf("clipboard backup read calls = %d, want 1", getCalls)
	}
	if got, want := setValues, []string{"new transcript", "previous clipboard"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("set clipboard values = %#v, want %#v", got, want)
	}
	if restored != windows.Handle(42) {
		t.Fatalf("restored foreground hwnd = %v, want 42", restored)
	}
	if pasteCalls != 1 {
		t.Fatalf("paste calls = %d, want 1", pasteCalls)
	}
	if pauseDuration != restoreDelay {
		t.Fatalf("pause duration = %s, want %s", pauseDuration, restoreDelay)
	}
}

func TestClipboardHandlerDoesNotRestoreWhenNoBackupExists(t *testing.T) {
	installClipboardHandlerStubs(t)

	clipboardGetText = func() (string, bool) { return "", false }
	setValues := []string{}
	clipboardSetText = func(text string) error {
		setValues = append(setValues, text)
		return nil
	}

	err := NewClipboardHandler().Handle(context.Background(), &stt.Result{Text: "new transcript"}, Target{})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got, want := setValues, []string{"new transcript"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("set clipboard values = %#v, want %#v", got, want)
	}
}

func TestClipboardHandlerReturnsSetClipboardErrorBeforePasting(t *testing.T) {
	installClipboardHandlerStubs(t)

	wantErr := errors.New("clipboard busy")
	clipboardSetText = func(string) error { return wantErr }
	pasteCalls := 0
	clipboardSimulateCtrlV = func() { pasteCalls++ }

	err := NewClipboardHandler().Handle(context.Background(), &stt.Result{Text: "new transcript"}, Target{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Handle() error = %v, want %v", err, wantErr)
	}
	if pasteCalls != 0 {
		t.Fatalf("paste calls = %d, want 0", pasteCalls)
	}
}

func installClipboardHandlerStubs(t *testing.T) {
	t.Helper()

	originalGet := clipboardGetText
	originalSet := clipboardSetText
	originalCurrent := clipboardCurrentForegroundWindow
	originalRestore := clipboardRestoreForegroundWindow
	originalPaste := clipboardSimulateCtrlV
	originalPause := clipboardPause

	clipboardGetText = func() (string, bool) { return "", false }
	clipboardSetText = func(string) error { return nil }
	clipboardCurrentForegroundWindow = func() windows.Handle { return windows.Handle(7) }
	clipboardRestoreForegroundWindow = func(windows.Handle) {}
	clipboardSimulateCtrlV = func() {}
	clipboardPause = func(time.Duration) {}

	t.Cleanup(func() {
		clipboardGetText = originalGet
		clipboardSetText = originalSet
		clipboardCurrentForegroundWindow = originalCurrent
		clipboardRestoreForegroundWindow = originalRestore
		clipboardSimulateCtrlV = originalPaste
		clipboardPause = originalPause
	})
}
