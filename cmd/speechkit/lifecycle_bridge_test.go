package main

import (
	"context"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/lifecycle"
)

// TestLifecycleBridgeTargetFromConfig asserts the cfg → Target mapping
// matches the existing applyModeEnabled semantics (enabled AND non-empty
// hotkey).
func TestLifecycleBridgeTargetFromConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := newDesktopLifecycleBridge(ctx)
	defer func() { _ = b.Shutdown(context.Background()) }()

	cases := []struct {
		name string
		cfg  *config.Config
		want lifecycle.Target
	}{
		{
			name: "all enabled with hotkeys",
			cfg: &config.Config{General: config.GeneralConfig{
				DictateEnabled: true, DictateHotkey: "ctrl+shift+d",
				AssistEnabled: true, AssistHotkey: "ctrl+shift+a",
				VoiceAgentEnabled: true, VoiceAgentHotkey: "ctrl+shift+v",
			}},
			want: lifecycle.Target{
				lifecycle.ModeDictation:  true,
				lifecycle.ModeAssist:     true,
				lifecycle.ModeVoiceAgent: true,
			},
		},
		{
			name: "enabled but empty hotkey reads as off",
			cfg: &config.Config{General: config.GeneralConfig{
				DictateEnabled: true, DictateHotkey: "",
				AssistEnabled: true, AssistHotkey: "ctrl+shift+a",
				VoiceAgentEnabled: false, VoiceAgentHotkey: "ctrl+shift+v",
			}},
			want: lifecycle.Target{
				lifecycle.ModeDictation:  false,
				lifecycle.ModeAssist:     true,
				lifecycle.ModeVoiceAgent: false,
			},
		},
		{
			name: "all off",
			cfg:  &config.Config{General: config.GeneralConfig{}},
			want: lifecycle.Target{
				lifecycle.ModeDictation:  false,
				lifecycle.ModeAssist:     false,
				lifecycle.ModeVoiceAgent: false,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := b.targetFromConfig(tc.cfg)
			for mode, wantOn := range tc.want {
				if got[mode] != wantOn {
					t.Errorf("target[%s] = %v; want %v", mode, got[mode], wantOn)
				}
			}
		})
	}
}

// TestLifecycleBridgeApplyTogglesRuntimes verifies that calling Apply
// against changing cfg states correctly transitions ModeRuntimes.
func TestLifecycleBridgeApplyTogglesRuntimes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := newDesktopLifecycleBridge(ctx)
	defer func() { _ = b.Shutdown(context.Background()) }()

	// Initially all stopped.
	snap := b.Snapshot()
	for _, mode := range []lifecycle.ModeKey{lifecycle.ModeDictation, lifecycle.ModeAssist, lifecycle.ModeVoiceAgent} {
		if got := snap.Modes[mode]; got != lifecycle.StatusStopped {
			t.Errorf("initial %s status = %s; want stopped", mode, got)
		}
	}

	// Turn Dictation on.
	cfg := &config.Config{General: config.GeneralConfig{
		DictateEnabled: true, DictateHotkey: "ctrl+shift+d",
	}}
	if err := b.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("Apply on: %v", err)
	}
	snap = b.Snapshot()
	if got := snap.Modes[lifecycle.ModeDictation]; got != lifecycle.StatusRunning {
		t.Errorf("after dict-on: dictation = %s; want running", got)
	}
	if got := snap.Modes[lifecycle.ModeAssist]; got != lifecycle.StatusStopped {
		t.Errorf("after dict-on: assist = %s; want stopped", got)
	}

	// Turn Assist on too.
	cfg.General.AssistEnabled = true
	cfg.General.AssistHotkey = "ctrl+shift+a"
	if err := b.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("Apply assist-on: %v", err)
	}
	snap = b.Snapshot()
	if got := snap.Modes[lifecycle.ModeAssist]; got != lifecycle.StatusRunning {
		t.Errorf("after assist-on: assist = %s; want running", got)
	}

	// Turn Dictation off.
	cfg.General.DictateEnabled = false
	if err := b.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("Apply dict-off: %v", err)
	}
	snap = b.Snapshot()
	if got := snap.Modes[lifecycle.ModeDictation]; got != lifecycle.StatusStopped {
		t.Errorf("after dict-off: dictation = %s; want stopped", got)
	}
	if got := snap.Modes[lifecycle.ModeAssist]; got != lifecycle.StatusRunning {
		t.Errorf("after dict-off: assist = %s; want still running", got)
	}
}

// TestLifecycleBridgeShutdownClosesAllRunningRuntimes verifies Shutdown
// stops every running runtime — the cleanup-on-exit guarantee.
func TestLifecycleBridgeShutdownClosesAllRunningRuntimes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := newDesktopLifecycleBridge(ctx)

	cfg := &config.Config{General: config.GeneralConfig{
		DictateEnabled: true, DictateHotkey: "ctrl+shift+d",
		AssistEnabled: true, AssistHotkey: "ctrl+shift+a",
		VoiceAgentEnabled: true, VoiceAgentHotkey: "ctrl+shift+v",
	}}
	if err := b.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("Apply all-on: %v", err)
	}

	if err := b.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	snap := b.Snapshot()
	for _, mode := range []lifecycle.ModeKey{lifecycle.ModeDictation, lifecycle.ModeAssist, lifecycle.ModeVoiceAgent} {
		if got := snap.Modes[mode]; got != lifecycle.StatusStopped {
			t.Errorf("after Shutdown: %s = %s; want stopped", mode, got)
		}
	}
}

// TestLifecycleBridgeIsNilSafe ensures the bridge tolerates nil-receiver
// calls so cleanup paths that ran before bridge construction don't panic.
func TestLifecycleBridgeIsNilSafe(t *testing.T) {
	var b *desktopLifecycleBridge
	if err := b.Apply(context.Background(), nil); err != nil {
		t.Errorf("nil-receiver Apply = %v; want nil", err)
	}
	if snap := b.Snapshot(); snap.Modes != nil {
		t.Errorf("nil-receiver Snapshot.Modes = %v; want nil", snap.Modes)
	}
	if err := b.Shutdown(context.Background()); err != nil {
		t.Errorf("nil-receiver Shutdown = %v; want nil", err)
	}
}

// TestLifecycleBridgeSubscriberDeliversTerminalTransitions makes sure
// the registry-subscriber goroutine receives terminal Running and
// Stopped events. The audit emitter consumes these; this test
// validates the wiring without depending on auditlog file I/O.
func TestLifecycleBridgeSubscriberDeliversTerminalTransitions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := newDesktopLifecycleBridge(ctx)
	defer func() { _ = b.Shutdown(context.Background()) }()

	// Tap a second subscriber to observe events independently of the
	// audit emitter. Use a generous buffer because we don't drain
	// until after the Apply completes.
	events, unsub := b.registry.Subscribe(64)
	defer unsub()

	cfg := &config.Config{General: config.GeneralConfig{
		DictateEnabled: true, DictateHotkey: "ctrl+shift+d",
	}}
	if err := b.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var sawRunning, sawStopped bool
	deadline := time.After(500 * time.Millisecond)
	for !sawRunning {
		select {
		case ev := <-events:
			if ev.Mode == lifecycle.ModeDictation && ev.To == lifecycle.StatusRunning {
				sawRunning = true
			}
		case <-deadline:
			t.Fatalf("did not receive Dictation→Running transition within deadline")
		}
	}

	cfg.General.DictateEnabled = false
	if err := b.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("Apply off: %v", err)
	}
	deadline = time.After(500 * time.Millisecond)
	for !sawStopped {
		select {
		case ev := <-events:
			if ev.Mode == lifecycle.ModeDictation && ev.To == lifecycle.StatusStopped {
				sawStopped = true
			}
		case <-deadline:
			t.Fatalf("did not receive Dictation→Stopped transition within deadline")
		}
	}
}
