//go:build linux

package voiceagent

import "testing"

// TestGuardPump_RecoversAndSignalsDone proves a panic inside a spawned session
// pump is contained (does not crash the process) and triggers session teardown
// via the done channel, mirroring the pumps' normal exit path.
func TestGuardPump_RecoversAndSignalsDone(t *testing.T) {
	a := &Adapter{Session: &ManagedSession{ID: "panic-session"}}
	done := make(chan struct{}, 2)

	func() {
		defer a.guardPump("read", done)
		panic("simulated pump panic")
	}()

	select {
	case <-done:
	default:
		t.Fatal("guardPump did not signal done after recovering a panic")
	}
}

// TestGuardPump_NoPanic_NoSignal ensures the guard is inert on the happy path.
func TestGuardPump_NoPanic_NoSignal(t *testing.T) {
	a := &Adapter{Session: &ManagedSession{ID: "ok-session"}}
	done := make(chan struct{}, 1)

	func() {
		defer a.guardPump("write", done)
	}()

	select {
	case <-done:
		t.Fatal("guardPump signalled done without a panic")
	default:
	}
}
