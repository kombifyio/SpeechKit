package capture

import (
	"errors"
	"runtime"
	"testing"
)

func TestDefaultBackendMatchesHostPlatform(t *testing.T) {
	want := map[string]Backend{
		"windows": BackendWindowsWASAPIMalgo,
		"darwin":  BackendDarwinCoreAudioMalgo,
	}[runtime.GOOS]
	if got := defaultBackend(); got != want {
		t.Fatalf("defaultBackend() on %s = %q, want %q", runtime.GOOS, got, want)
	}
}

// Every platform backend name is a sentinel-reporting constructor on every
// build: registered with the real session (skipped here, the cgo tests
// cover it), with the stub's ErrBackendUnavailable factory, or absent
// (ErrUnsupportedBackend). Never a panic, never a silent no-op.
func TestOpenPlatformBackendNamesReportSentinels(t *testing.T) {
	for _, backend := range []Backend{BackendWindowsWASAPIMalgo, BackendDarwinCoreAudioMalgo} {
		if backend == defaultBackend() {
			continue
		}
		t.Run(string(backend), func(t *testing.T) {
			session, err := Open(Config{Backend: backend})
			if session != nil {
				_ = session.Close()
				t.Fatalf("Open(%q) returned a session on %s", backend, runtime.GOOS)
			}
			if !errors.Is(err, ErrBackendUnavailable) && !errors.Is(err, ErrUnsupportedBackend) {
				t.Fatalf("Open(%q) error = %v, want ErrBackendUnavailable or ErrUnsupportedBackend", backend, err)
			}
		})
	}
}
