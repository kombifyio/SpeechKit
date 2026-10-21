//go:build !windows && !darwin

package capture

// defaultBackend names the capture backend Open uses for BackendAuto on
// this platform. No malgo backend is ported here, so Open reports
// ErrBackendUnavailable ("no default backend for this build") unless the
// host registers its own backend through RegisterBackend and selects it.
func defaultBackend() Backend {
	return ""
}
