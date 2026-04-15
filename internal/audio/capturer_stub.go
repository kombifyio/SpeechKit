//go:build !windows || !cgo

package audio

import "fmt"

func init() {
	if err := RegisterBackend(BackendWindowsWASAPIMalgo, func(Config) (Session, error) {
		return nil, fmt.Errorf("%w: backend %q requires a Windows cgo build", ErrBackendUnavailable, BackendWindowsWASAPIMalgo)
	}); err != nil {
		panic(err)
	}
}
