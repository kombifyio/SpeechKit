//go:build !windows && !darwin

package procguard

import (
	"errors"
	"os/exec"
	"testing"
)

// The point of this file: on a platform with no guard, every entry point must
// say so. Adopt used to return nil here, which the
// callers could not distinguish from a successful adoption.

func TestAdoptReportsTheUnsupportedPlatformSentinel(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "true")
	if err := Adopt(cmd); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("Adopt error = %v, want ErrUnsupportedPlatform", err)
	}
}

func TestAdoptOrReportReportsTheUnsupportedPlatformSentinel(t *testing.T) {
	var reported []error
	AdoptOrReport(exec.Command("/bin/sh", "-c", "true"), func(err error) { reported = append(reported, err) })
	if len(reported) != 1 || !errors.Is(reported[0], ErrUnsupportedPlatform) {
		t.Fatalf("reported = %v, want exactly one ErrUnsupportedPlatform", reported)
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
