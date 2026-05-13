//go:build !windows

package main

// tryAcquireSingleton is a no-op on non-Windows builds — the desktop
// client only ships for Windows. Returning isFirst=true keeps test
// builds on other platforms compatible without conditional bootstrap
// logic at the call site.
func tryAcquireSingleton() (release func(), isFirst bool, err error) {
	return func() {}, true, nil
}
