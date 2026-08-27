//go:build windows && cgo

package capture

import (
	"errors"
	"testing"
)

func TestMalgoSessionRejectsMicAndSystemUntilMixerExists(t *testing.T) {
	session, err := newMalgoSession(Config{InputSource: InputSourceMicAndSystem})
	if session != nil {
		t.Fatal("newMalgoSession returned a session for mic_and_system, want nil")
	}
	if !errors.Is(err, ErrUnsupportedSource) {
		t.Fatalf("newMalgoSession error = %v, want ErrUnsupportedSource", err)
	}
}
