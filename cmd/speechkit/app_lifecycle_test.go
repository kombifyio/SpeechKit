package main

import "testing"

func TestAppSurfaceClosePolicyAllowsCloseDuringShutdown(t *testing.T) {
	state := &appState{}

	if !state.shouldHideWindowOnClose() {
		t.Fatal("surface close should hide windows during normal app use")
	}

	state.beginShutdown()

	if state.shouldHideWindowOnClose() {
		t.Fatal("surface close should not cancel native close while app is shutting down")
	}
}
