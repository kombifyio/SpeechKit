//go:build !windows || !cgo

package capture_test

import (
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio/capture"
)

// The documented no-cgo contract: builds without the Windows cgo backend
// still compile, and constructing a capturer reports the unavailable
// sentinel instead of panicking.
func TestOpenWithoutNativeBackendReturnsUnavailable(t *testing.T) {
	for name, open := range map[string]func() (capture.Session, error){
		"Open":        func() (capture.Session, error) { return capture.Open(capture.Config{}) },
		"NewCapturer": func() (capture.Session, error) { return capture.NewCapturer() },
	} {
		t.Run(name, func(t *testing.T) {
			session, err := open()
			if session != nil {
				t.Fatalf("%s returned a session without a native backend", name)
			}
			if !errors.Is(err, capture.ErrBackendUnavailable) {
				t.Fatalf("%s error = %v, want ErrBackendUnavailable", name, err)
			}
		})
	}
}
