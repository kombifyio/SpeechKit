package audio

import "strings"

// DeviceInfo describes a capture device that can be presented to the user.
type DeviceInfo struct {
	ID        string `json:"deviceId"`
	Name      string `json:"label"`
	IsDefault bool   `json:"isDefault"`
}

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

func selectCaptureDeviceID(requested string, devices []DeviceInfo) string {
	return selectDeviceID(requested, devices)
}

func selectOutputDeviceID(requested string, devices []DeviceInfo) string {
	return selectDeviceID(requested, devices)
}

func selectDeviceID(requested string, devices []DeviceInfo) string {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return ""
	}

	for _, device := range devices {
		if strings.EqualFold(strings.TrimSpace(device.ID), requested) {
			return device.ID
		}
	}

	return ""
}
