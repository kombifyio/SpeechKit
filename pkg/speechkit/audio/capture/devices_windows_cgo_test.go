//go:build windows && cgo

package capture

import (
	"errors"
	"testing"
)

func TestDeviceIDFromHexStringRoundTrip(t *testing.T) {
	id, ok, err := deviceIDFromHexString("010203")
	if err != nil {
		t.Fatalf("deviceIDFromHexString() error = %v", err)
	}
	if !ok {
		t.Fatal("deviceIDFromHexString() returned ok=false")
	}
	if got := id.String(); got != "010203" {
		t.Fatalf("round trip = %q, want %q", got, "010203")
	}
}

func TestDeviceIDFromHexStringRejectsInvalidInput(t *testing.T) {
	if _, ok, err := deviceIDFromHexString("zz"); err == nil || ok {
		t.Fatalf("expected parse failure, got ok=%v err=%v", ok, err)
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
