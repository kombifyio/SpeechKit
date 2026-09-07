//go:build (windows || darwin) && cgo

package capture

import (
	"errors"
	"runtime"
	"strings"
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

// The platform seam names the backend the shared session registers as, and
// that name is the build's default so Open(Config{}) reaches it.
func TestPlatformMalgoSeamIsTheRegisteredDefaultBackend(t *testing.T) {
	name := platformMalgoBackend()
	if name != defaultBackend() {
		t.Fatalf("platformMalgoBackend() = %q, defaultBackend() = %q", name, defaultBackend())
	}
	registryMu.RLock()
	_, registered := registry[name]
	registryMu.RUnlock()
	if !registered {
		t.Fatalf("backend %q is not registered", name)
	}
	if backends := platformMalgoContextBackends(); len(backends) != 1 {
		t.Fatalf("platformMalgoContextBackends() = %v, want exactly one backend", backends)
	}
}

func TestSourceUnavailableOnPlatformWrapsUnsupportedSource(t *testing.T) {
	err := errSourceUnavailableOnPlatform(InputSourceSystemLoopback)
	if !errors.Is(err, ErrUnsupportedSource) {
		t.Fatalf("error = %v, want ErrUnsupportedSource", err)
	}
	if msg := err.Error(); !strings.Contains(msg, string(InputSourceSystemLoopback)) || !strings.Contains(msg, runtime.GOOS) {
		t.Fatalf("error = %q, want it to name the source and %s", msg, runtime.GOOS)
	}
}
