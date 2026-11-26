package capture

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
