package main

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type fakeHotkeyReconfigurer struct {
	combos [][]uint32
}

func (f *fakeHotkeyReconfigurer) Reconfigure(combo []uint32) {
	cloned := append([]uint32(nil), combo...)
	f.combos = append(f.combos, cloned)
}

func TestAppStateApplyRuntimeSettingsUpdatesSnapshot(t *testing.T) {
	state := &appState{
		hotkey:          "win+alt",
		providers:       []string{"local"},
		overlayPosition: "top",
	}
	state.engine = newSpeechKitRuntime(state, speechkit.Hooks{})

	oldHotkey := state.applyRuntimeSettings(
		"ctrl+shift+d",
		"ctrl+shift+k",
		"dictate",
		"mic-1",
		[]string{"local", "hf"},
		"pill",
		"default",
		"bottom",
	)

	if got, want := oldHotkey, "win+alt"; got != want {
		t.Fatalf("oldHotkey = %q, want %q", got, want)
	}
	runtime := state.runtimeStateLocked()
	if got, want := runtime.hotkey, "ctrl+shift+d"; got != want {
		t.Fatalf("runtime.hotkey = %q, want %q", got, want)
	}
	if got, want := runtime.overlayPosition, "bottom"; got != want {
		t.Fatalf("runtime.overlayPosition = %q, want %q", got, want)
	}
	if got, want := len(runtime.providers), 2; got != want {
		t.Fatalf("len(runtime.providers) = %d, want %d", got, want)
	}

	snapshot := state.engine.State()
	if got, want := snapshot.Hotkey, "ctrl+shift+d"; got != want {
		t.Fatalf("snapshot.Hotkey = %q, want %q", got, want)
	}
}

func TestAppStateApplyDesktopSettingsReconfiguresHotkey(t *testing.T) {
	hk := &fakeHotkeyReconfigurer{}
	state := &appState{
		hotkey:        "win+alt",
		dictateHotkey: "win+alt",
		agentHotkey:   "ctrl+shift+k",
		audioDeviceID: "mic-1",
		hkManager:     hk,
	}

	state.applyDesktopSettings("win+alt", "ctrl+shift+k", "ctrl+shift+d", "ctrl+shift+k", "mic-1", "mic-1", true)

	if got, want := len(hk.combos), 1; got != want {
		t.Fatalf("len(hk.combos) = %d, want %d", got, want)
	}
	if !state.overlayEnabled {
		t.Fatal("state.overlayEnabled = false, want true")
	}
}
