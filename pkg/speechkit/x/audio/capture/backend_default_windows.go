//go:build windows

package capture

// defaultBackend names the capture backend Open uses for BackendAuto on
// this platform. Windows builds default to the malgo WASAPI session; a
// CGO_ENABLED=0 build reaches the registered stub, which reports
// ErrBackendUnavailable.
func defaultBackend() Backend {
	return BackendWindowsWASAPIMalgo
}
