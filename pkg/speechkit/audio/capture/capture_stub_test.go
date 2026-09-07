//go:build !((windows || darwin) && cgo)

package capture

import (
	"errors"
	"strings"
	"testing"
)

func TestNewCapturerWithoutNativeBackendReturnsUnavailableError(t *testing.T) {
	_, err := Open(Config{})
	if err == nil {
		t.Fatal("expected constructor error without native backend")
	}
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("expected ErrBackendUnavailable, got %v", err)
	}
}

// Both platform backend names are registered on the stub build and report
// the unavailable sentinel with a message that names the backend and the
// cgo build it needs.
func TestOpenPlatformBackendsWithoutNativeBackendReturnUnavailable(t *testing.T) {
	for _, backend := range []Backend{BackendWindowsWASAPIMalgo, BackendDarwinCoreAudioMalgo} {
		t.Run(string(backend), func(t *testing.T) {
			session, err := Open(Config{Backend: backend})
			if session != nil {
				t.Fatalf("Open(%q) returned a session without a native backend", backend)
			}
			if !errors.Is(err, ErrBackendUnavailable) {
				t.Fatalf("Open(%q) error = %v, want ErrBackendUnavailable", backend, err)
			}
			if !strings.Contains(err.Error(), string(backend)) || !strings.Contains(err.Error(), "cgo build") {
				t.Fatalf("Open(%q) error = %q, want it to name the backend and the cgo build", backend, err)
			}
		})
	}
}
