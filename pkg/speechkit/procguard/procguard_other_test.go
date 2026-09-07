//go:build !windows && !darwin

package procguard

import (
	"errors"
	"os/exec"
	"testing"
)

// The point of this file: on a platform with no guard, every entry point must
// say so. Adopt returned nil here until kombify-SpeechKit-mcos.14, which the
// callers could not distinguish from a successful adoption.

func TestAdoptReportsTheUnsupportedPlatformSentinel(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "true")
	if err := Adopt(cmd); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("Adopt error = %v, want ErrUnsupportedPlatform", err)
	}
}

func TestSweepReportsTheUnsupportedPlatformSentinel(t *testing.T) {
	count, err := Sweep("/Applications/SpeechKit.app/Contents/Helpers")
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("Sweep error = %v, want ErrUnsupportedPlatform", err)
	}
	if count != 0 {
		t.Fatalf("Sweep count = %d, want 0", count)
	}
}

func TestPrepareAndShutdownAreSafeNoOps(t *testing.T) {
	Prepare(nil)
	Prepare(exec.Command("/bin/sh", "-c", "true"))
	Shutdown()
}
