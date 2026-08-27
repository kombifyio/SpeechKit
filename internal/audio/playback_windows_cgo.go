//go:build windows && cgo

package audio

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gen2brain/malgo"
)

// Playback-side copies of the device-resolution helpers that moved to
// pkg/speechkit/audio/capture with the capture layer. They stay private
// there (and playback stays private here), so the malgo stream player
// keeps small local duplicates instead of the public package exporting
// malgo types.

func malgoBackendsForConfig(cfg Config) ([]malgo.Backend, error) {
	switch cfg.Backend {
	case "", BackendAuto, BackendWindowsWASAPIMalgo:
		return []malgo.Backend{malgo.BackendWasapi}, nil
	default:
		return nil, fmt.Errorf("%w: backend %q does not support device enumeration", ErrUnsupportedBackend, cfg.Backend)
	}
}

// resolveOutputDeviceID resolves the persisted output-device selection
// against the current enumeration. It prefers an exact ID match; if the
// ID is gone (USB/UAC devices re-enumerate with new endpoint IDs), it
// falls back to a case-insensitive name match.
func resolveOutputDeviceID(cfg Config) (malgo.DeviceID, bool, error) {
	requested := strings.TrimSpace(cfg.DeviceID)
	if requested == "" {
		return malgo.DeviceID{}, false, nil
	}

	devices, err := ListOutputDevices(cfg)
	if err != nil {
		return malgo.DeviceID{}, false, err
	}

	var selected string
	for _, device := range devices {
		if strings.EqualFold(strings.TrimSpace(device.ID), requested) {
			selected = device.ID
			break
		}
	}
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
