//go:build windows && cgo

package audio

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gen2brain/malgo"
)

func init() {
	captureDeviceLister = listCaptureDevicesFromContext
}

func listCaptureDevicesFromContext(cfg Config) ([]DeviceInfo, error) {
	backends, err := malgoBackendsForConfig(cfg)
	if err != nil {
		return nil, err
	}

	ctx, err := malgo.InitContext(backends, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}
	defer ctx.Free()

	devices, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, err
	}

	out := make([]DeviceInfo, 0, len(devices))
	for _, device := range devices {
		out = append(out, DeviceInfo{
			ID:        device.ID.String(),
			Name:      device.Name(),
			IsDefault: device.IsDefault != 0,
		})
	}
	return out, nil
}

func malgoBackendsForConfig(cfg Config) ([]malgo.Backend, error) {
	switch cfg.Backend {
	case "", BackendAuto, BackendWindowsWASAPIMalgo:
		return []malgo.Backend{malgo.BackendWasapi}, nil
	default:
		return nil, fmt.Errorf("%w: backend %q does not support device enumeration", ErrUnsupportedBackend, cfg.Backend)
	}
}

func resolveCaptureDeviceID(cfg Config) (malgo.DeviceID, bool, error) {
	requested := strings.TrimSpace(cfg.DeviceID)
	if requested == "" {
		return malgo.DeviceID{}, false, nil
	}

	devices, err := ListCaptureDevices(cfg)
	if err != nil {
		return malgo.DeviceID{}, false, err
	}

	selected := selectCaptureDeviceID(requested, devices)
	if selected == "" {
		return malgo.DeviceID{}, false, nil
	}

	return deviceIDFromHexString(selected)
}

func deviceIDFromHexString(value string) (malgo.DeviceID, bool, error) {
	var id malgo.DeviceID

	value = strings.TrimSpace(value)
	if value == "" {
		return id, false, nil
	}

	decoded, err := hex.DecodeString(value)
	if err != nil {
		return id, false, err
	}
	if len(decoded) > len(id) {
		return id, false, fmt.Errorf("device id too long: %d bytes", len(decoded))
	}

	copy(id[:], decoded)
	return id, true, nil
}
