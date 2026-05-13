package main

import "testing"

// TestRecoverHookSuppressesPanic verifies that a deferred recoverHook
// call prevents a panicking callback from propagating, so a single
// faulty Wails event-loop hook cannot crash the desktop event loop.
func TestRecoverHookSuppressesPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic propagated past recoverHook: %v", r)
		}
	}()

	func() {
		defer recoverHook("test_hook")
		panic("boom")
	}()
}

// TestRecoverHookNoPanicIsNoop confirms recoverHook is safe to defer in
// every callback even when the body completes normally.
func TestRecoverHookNoPanicIsNoop(t *testing.T) {
	called := false
	func() {
		defer recoverHook("test_hook_noop")
		called = true
	}()
	if !called {
		t.Fatal("body did not execute")
	}
}
