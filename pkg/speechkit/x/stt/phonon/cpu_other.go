//go:build !amd64

package phonon

// x86v3Extras is never consulted outside amd64.
func x86v3Extras() bool { return false }
