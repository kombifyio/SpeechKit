package capture

import "strings"

// DeviceInfo describes a capture device that can be presented to the user.
type DeviceInfo struct {
	ID        string `json:"deviceId"`
	Name      string `json:"label"`
	IsDefault bool   `json:"isDefault"`
}

// OnCaptureDeviceRebound, when set, is called after a configured capture
// device ID was not found but the device was recovered via its persisted
// name (USB/UAC re-enumeration). The host app can persist the new ID.
var OnCaptureDeviceRebound func(oldID, newID, name string)

var captureDeviceLister = func(Config) ([]DeviceInfo, error) {
	return nil, ErrBackendUnavailable
}

var outputDeviceLister = func(Config) ([]DeviceInfo, error) {
	return nil, ErrBackendUnavailable
}

// ListCaptureDevices returns the available microphone devices for the selected backend.
func ListCaptureDevices(cfg Config) ([]DeviceInfo, error) {
	return captureDeviceLister(normalizeConfig(cfg))
}

// ListOutputDevices returns the available speaker devices for the selected backend.
func ListOutputDevices(cfg Config) ([]DeviceInfo, error) {
	return outputDeviceLister(normalizeConfig(cfg))
}

func selectCaptureDeviceID(requestedID, requestedName string, devices []DeviceInfo) string {
	return selectDeviceID(requestedID, requestedName, devices)
}

func selectOutputDeviceID(requested string, devices []DeviceInfo) string {
	return selectDeviceID(requested, "", devices)
}

// selectDeviceID resolves the persisted device selection against the current
// enumeration. It prefers an exact ID match; if the ID is gone (USB/UAC
// devices re-enumerate with new endpoint IDs), it falls back to a
// case-insensitive name match and returns that device's new ID.
func selectDeviceID(requestedID, requestedName string, devices []DeviceInfo) string {
	requestedID = strings.TrimSpace(requestedID)
	requestedName = strings.TrimSpace(requestedName)

	if requestedID != "" {
		for _, device := range devices {
			if strings.EqualFold(strings.TrimSpace(device.ID), requestedID) {
				return device.ID
			}
		}
	}

	if requestedName != "" {
		for _, device := range devices {
			if strings.EqualFold(strings.TrimSpace(device.Name), requestedName) {
				return device.ID
			}
		}
	}

	return ""
}
