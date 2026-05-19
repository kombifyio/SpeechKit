package main

import (
	"context"
	"os/exec"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestStartDesktopWakeword_DisabledIsNoop(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.Enabled = false

	got := startDesktopWakeword(context.Background(), cfg, &appState{}, nil, nil)
	if got != nil {
		t.Fatalf("expected nil runtime when disabled, got %+v", got)
	}
}

func TestStartDesktopWakeword_MissingHotkeyManagerIsNoop(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.Enabled = true

	state := &appState{}
	got := startDesktopWakeword(context.Background(), cfg, state, nil, nil)
	if got != nil {
		t.Fatalf("expected nil runtime when hotkey manager missing, got %+v", got)
	}
}

func TestResolveWakeword_MissingModelDirSurfacesError(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.Enabled = true

	_, err := resolveWakeword(cfg)
	if err == nil {
		t.Fatal("expected error when wakeword-kws bundle directory is absent")
	}
}

// TestSidecarParentContextIsLongLived guards the wiring invariant that the
// sidecar's exec.CommandContext is rooted in a Background-equivalent context
// so callers' short-lived contexts (HTTP request, settings save) cannot kill
// the long-lived sidecar. See launchWakewordSidecar comment for bug history.
func TestSidecarParentContextIsLongLived(t *testing.T) {
	ctx := sidecarParentContext()
	if ctx == nil {
		t.Fatal("sidecarParentContext returned nil")
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		t.Fatal("sidecar parent ctx must not carry a deadline")
	}
	select {
	case <-ctx.Done():
		t.Fatal("sidecar parent ctx is already Done — must be long-lived")
	default:
	}
}

// TestLaunchWakewordSidecar_DecouplesFromShortLivedCallerCtx is the behavioral
// regression test for the v0.35.6 "Wake-word sidecar exited (code 1)" bug.
// Until the fix, launchWakewordSidecar derived its exec.CommandContext from
// the caller's parentCtx — which, for restartDesktopWakeword(r.Context(), ...)
// callers in routes_feature_auth_app.go and routes_settings.go, was an HTTP
// request ctx that cancelled the instant the handler returned. exec then
// killed the sidecar via Windows TerminateProcess(handle, 1).
//
// The fix decouples the sidecar exec ctx from any caller ctx. This test
// proves it by passing an already-cancelled caller ctx into launch and
// asserting the ctx actually handed to exec.CommandContext is NOT cancelled.
func TestLaunchWakewordSidecar_DecouplesFromShortLivedCallerCtx(t *testing.T) {
	var (
		captured    context.Context
		doneAtCall  bool
		hadDeadline bool
	)
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		captured = ctx
		select {
		case <-ctx.Done():
			doneAtCall = true
		default:
		}
		_, hadDeadline = ctx.Deadline()
		// Return a Cmd pointing at a non-existent binary so cmd.Start() fails
		// fast and launchWakewordSidecar returns its early-error path. We
		// asserted on the ctx snapshot above before that error path could run
		// cancel() and pollute the captured ctx.
		return exec.Command("non-existent-wakeword-stub-binary-zzz")
	}

	callerCtx, cancelCaller := context.WithCancel(context.Background())
	cancelCaller()

	state := &appState{}
	cfg := &config.Config{}
	cfg.Wakeword.Threshold = 0.25
	cfg.Wakeword.MinConsecutiveFrames = 1
	cfg.Wakeword.CooldownMs = 1500
	resolved := resolvedWakewordAssets{
		SidecarPath:   "fake-sidecar",
		ModelDir:      "fake-models",
		KeywordsFile:  "fake-keywords.txt",
		DefaultMode:   "voice_agent",
		DisplayPhrase: "Hey Quby",
	}

	_, _ = launchWakewordSidecar(callerCtx, state, nil, cfg, resolved)

	if captured == nil {
		t.Fatal("execCommandContext was never invoked")
	}
	if doneAtCall {
		t.Fatal("sidecar exec ctx was already Done at exec.CommandContext call time — regression: caller ctx cancellation is propagating to the sidecar (this is the v0.35.6 'Wake-word sidecar exited code 1' bug)")
	}
	if hadDeadline {
		t.Fatal("sidecar exec ctx unexpectedly carries a deadline — must be long-lived Background")
	}
}
