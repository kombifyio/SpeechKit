//go:build !((windows || darwin) && cgo)

package capture

import "fmt"

// Without the cgo malgo session (a host other than Windows or macOS, or a
// CGO_ENABLED=0 build) both platform backend names stay registered so that
// Open reports ErrBackendUnavailable at construction time instead of
// panicking or silently recording nothing.
func init() {
	registerUnavailableBackend(BackendWindowsWASAPIMalgo, "Windows")
	registerUnavailableBackend(BackendDarwinCoreAudioMalgo, "macOS")
}

func registerUnavailableBackend(name Backend, platform string) {
	if err := RegisterBackend(name, func(Config) (Session, error) {
		return nil, fmt.Errorf("%w: backend %q requires a %s cgo build", ErrBackendUnavailable, name, platform)
	}); err != nil {
		panic(err)
	}
}
