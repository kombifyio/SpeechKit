package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

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

// TestIsAppStartedReturnsFalseForNilState confirms the nil-receiver guard
// stays in place; callers must keep working even when state plumbing has
// not been initialised yet.
func TestIsAppStartedReturnsFalseForNilState(t *testing.T) {
	var s *appState
	if s.isAppStarted() {
		t.Fatal("nil appState must report not-started")
	}
}

// TestIsAppStartedReturnsTrueWithoutWailsApp documents the test-friendly
// path that drives most hotkey/gate tests: when no Wails app is wired,
// no main-thread dispatcher exists, application.InvokeSync is never
// reached, and the started flag is meaningless — treat as ready.
func TestIsAppStartedReturnsTrueWithoutWailsApp(t *testing.T) {
	state := &appState{}
	if state.wailsApp != nil {
		t.Fatalf("precondition: wailsApp must be nil, got %T", state.wailsApp)
	}
	if !state.isAppStarted() {
		t.Fatal("without a Wails app there is no dispatcher to wait for; isAppStarted must return true")
	}

	state.markAppStarted()
	if !state.isAppStarted() {
		t.Fatal("markAppStarted on a wailsApp-less state must remain true")
	}
}

// TestIsAppStartedGatesOnAppStartedWithWailsApp is the explicit
// regression guard for beads kombify-SpeechKit-0s6: when a Wails app is
// wired (production path) but app.Run() has not yet entered its main
// thread, isAppStarted must return false. Otherwise any caller that uses
// application.InvokeSync — overlay positioning, dashboard show, screen-
// aware window placement — will dereference an uninitialised dispatcher
// and crash the desktop client at offset 0x60 inside dispatchOnMainThread.
//
// This test would have caught the original race if it had existed before
// 2026-05-17. Keep it as the canonical proof of fix.
func TestIsAppStartedGatesOnAppStartedWithWailsApp(t *testing.T) {
	state := &appState{wailsApp: &application.App{}}
	if state.isAppStarted() {
		t.Fatal("with a Wails app present and appStarted=false, isAppStarted must report not-started — see beads kombify-SpeechKit-0s6")
	}

	state.markAppStarted()
	if !state.isAppStarted() {
		t.Fatal("after markAppStarted(), isAppStarted must report started so UI callers can proceed")
	}
}

// TestOverlayLifecycleNoOpsDuringShutdown is the regression guard for beads
// kombify-SpeechKit-u8b: once beginShutdown() has flipped the state, every
// overlay-touching method must short-circuit so a parallel goroutine cannot
// drive Show/Hide/SetPosition into Wails while the framework is destroying
// its windows. Without the guards the desktop process hangs after the tray
// "Quit" because the 900 ms overlay sync tick and the audio level handler
// keep calling into windows that no longer have a backing dispatcher.
func TestOverlayLifecycleNoOpsDuringShutdown(t *testing.T) {
	state := &appState{overlayEnabled: true}
	state.beginShutdown()

	// None of these should reach the underlying window APIs. We rely on
	// the methods being nil-safe (no overlay windows wired here) when the
	// shutdown guard short-circuits; a panic indicates a guard regression.
	state.syncOverlayToActiveScreen()
	state.positionOverlay()
	state.showActiveOverlayWindow()
	state.showPillPanel()
	state.showRadialMenu()
	state.showAssistBubble("ignored")
	state.showAssistPanel("ignored", "ignored")
	state.primeOverlayForCapture("dictate")
	state.setOverlayFeedbackMessage("user", "ignored", true)
	state.refreshOverlayWindows()
}
