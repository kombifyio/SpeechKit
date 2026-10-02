package testutil

import (
	"testing"
	"time"
)

// Eventually polls fn every interval until it returns true or timeout elapses,
// in which case the test fails.
func Eventually(t testing.TB, timeout, interval time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if fn() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition was not met within %s", timeout)
		}
		time.Sleep(interval)
	}
}

// EventuallyNoError polls fn until it returns nil or timeout elapses.
func EventuallyNoError(t testing.TB, timeout, interval time.Duration, fn func() error) {
	t.Helper()
	Eventually(t, timeout, interval, func() bool {
		return fn() == nil
	})
}
