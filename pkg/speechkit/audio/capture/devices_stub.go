//go:build !windows || !cgo

package capture

func init() {
	captureDeviceLister = func(Config) ([]DeviceInfo, error) {
		return nil, ErrBackendUnavailable
	}
	outputDeviceLister = func(Config) ([]DeviceInfo, error) {
		return nil, ErrBackendUnavailable
	}
}
