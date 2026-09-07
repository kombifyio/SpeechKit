//go:build darwin

package capture

// defaultBackend names the capture backend Open uses for BackendAuto on
// this platform. macOS builds default to the malgo CoreAudio session; a
// CGO_ENABLED=0 build reaches the registered stub, which reports
// ErrBackendUnavailable.
func defaultBackend() Backend {
	return BackendDarwinCoreAudioMalgo
}
