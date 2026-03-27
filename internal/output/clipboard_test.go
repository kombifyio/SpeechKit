package output

import (
	"testing"

	"golang.org/x/sys/windows"
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
