//go:build windows && cgo

package capture

import (
	"errors"
	"testing"

	"github.com/gen2brain/malgo"
)

func TestWindowsMalgoSeamIsWASAPIWithLoopback(t *testing.T) {
	if got := platformMalgoBackend(); got != BackendWindowsWASAPIMalgo {
		t.Fatalf("platformMalgoBackend() = %q, want %q", got, BackendWindowsWASAPIMalgo)
	}
	if !platformSupportsLoopback() {
		t.Fatal("platformSupportsLoopback() = false on windows, want true")
	}
	backends := platformMalgoContextBackends()
	if len(backends) != 1 || backends[0] != malgo.BackendWasapi {
		t.Fatalf("platformMalgoContextBackends() = %v, want [BackendWasapi]", backends)
	}
}

func TestEnsureLoopbackOutputDeviceAvailableRejectsMissingRenderDevice(t *testing.T) {
	original := outputDeviceLister
	t.Cleanup(func() { outputDeviceLister = original })
	outputDeviceLister = func(Config) ([]DeviceInfo, error) {
		return nil, nil
	}

	err := ensureLoopbackOutputDeviceAvailable(Config{InputSource: InputSourceSystemLoopback})
	if !errors.Is(err, ErrOutputDeviceUnavailable) {
		t.Fatalf("ensureLoopbackOutputDeviceAvailable() error = %v, want ErrOutputDeviceUnavailable", err)
	}
}

func TestEnsureLoopbackOutputDeviceAvailableRejectsMissingConfiguredDevice(t *testing.T) {
	original := outputDeviceLister
	t.Cleanup(func() { outputDeviceLister = original })
	outputDeviceLister = func(Config) ([]DeviceInfo, error) {
		return []DeviceInfo{{ID: "speaker-1", Name: "Speaker 1", IsDefault: true}}, nil
	}

	err := ensureLoopbackOutputDeviceAvailable(Config{
		InputSource:    InputSourceSystemLoopback,
		OutputDeviceID: "missing-speaker",
	})
	if !errors.Is(err, ErrOutputDeviceUnavailable) {
		t.Fatalf("ensureLoopbackOutputDeviceAvailable() error = %v, want ErrOutputDeviceUnavailable", err)
	}
}

func TestEnsureLoopbackOutputDeviceAvailableAcceptsDefaultRenderDevice(t *testing.T) {
	original := outputDeviceLister
	t.Cleanup(func() { outputDeviceLister = original })
	outputDeviceLister = func(Config) ([]DeviceInfo, error) {
		return []DeviceInfo{{ID: "speaker-1", Name: "Speaker 1", IsDefault: true}}, nil
	}

	if err := ensureLoopbackOutputDeviceAvailable(Config{InputSource: InputSourceSystemLoopback}); err != nil {
		t.Fatalf("ensureLoopbackOutputDeviceAvailable() error = %v", err)
	}
}
