//go:build windows && cgo

package capture

import (
	"fmt"
	"strings"

	"github.com/gen2brain/malgo"
)

// Windows seam of the shared malgo capture session (capture_malgo_cgo.go):
// the WASAPI backend name, the malgo context backend list, and the WASAPI
// loopback (system audio) support the macOS seam does not have yet.

func init() {
	if err := RegisterBackend(BackendWindowsWASAPIMalgo, newMalgoSession); err != nil {
		panic(err)
	}
}

// platformMalgoBackend names the backend the shared malgo session registers
// as and reports in its events on this platform.
func platformMalgoBackend() Backend {
	return BackendWindowsWASAPIMalgo
}

// platformMalgoContextBackends is the malgo backend list the session and the
// device enumeration initialise their context with.
func platformMalgoContextBackends() []malgo.Backend {
	return []malgo.Backend{malgo.BackendWasapi}
}

// platformSupportsLoopback reports whether InputSourceSystemLoopback can be
// opened: WASAPI exposes every render endpoint as a loopback capture device.
func platformSupportsLoopback() bool {
	return true
}

func ensureLoopbackOutputDeviceAvailable(cfg Config) error {
	devices, err := ListOutputDevices(Config{Backend: cfg.Backend})
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return fmt.Errorf("%w: system loopback requires at least one active Windows playback/render device", ErrOutputDeviceUnavailable)
	}
	requested := strings.TrimSpace(cfg.OutputDeviceID)
	if requested == "" {
		return nil
	}
	if selected := selectOutputDeviceID(requested, devices); selected == "" {
		return fmt.Errorf("%w: configured system loopback output device %q is not available", ErrOutputDeviceUnavailable, requested)
	}
	return nil
}
