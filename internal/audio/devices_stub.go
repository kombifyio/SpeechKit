//go:build !windows || !cgo

package audio

func init() {
	captureDeviceLister = func(Config) ([]DeviceInfo, error) {
		return nil, ErrBackendUnavailable
	}
}
