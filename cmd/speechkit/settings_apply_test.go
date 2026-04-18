package main

import (
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type fakeHotkeyReconfigurer struct {
	combos [][]uint32
}

func (f *fakeHotkeyReconfigurer) Reconfigure(combo []uint32) {
	cloned := append([]uint32(nil), combo...)
	f.combos = append(f.combos, cloned)
}

func (f *fakeHotkeyReconfigurer) ReconfigureModes(bindings map[string][]uint32) {
	for _, mode := range orderedRuntimeModes() {
		combo, ok := bindings[mode]
		if !ok {
			continue
		}
		cloned := append([]uint32(nil), combo...)
		f.combos = append(f.combos, cloned)
	}
}

func TestAppStateApplyRuntimeSettingsUpdatesSnapshot(t *testing.T) {
	state := &appState{
		hotkey:          "win+alt",
		providers:       []string{"local"},
		overlayPosition: "top",
	}
	state.engine = newSpeechKitRuntime(state, speechkit.Hooks{})

	oldHotkey := state.applyRuntimeSettings(
		true,
		true,
		true,
		"ctrl+shift+d",
		"ctrl+win+j",
		"win+alt+k",
		config.HotkeyBehaviorToggle,
		config.HotkeyBehaviorPushToTalk,
		config.HotkeyBehaviorToggle,
		"dictate",
		"mic-1",
		[]string{"local", "hf"},
		"pill",
		"default",
		"bottom",
		"kombi fire => Kombify",
		true,
		720,
		360,
		map[string]config.OverlayFreePosition{
			"100,100,1920,1080": {X: 720, Y: 360},
		},
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
	if !runtime.overlayMovable {
		t.Fatal("runtime.overlayMovable = false, want true")
	}
	if got, want := runtime.overlayFreeX, 720; got != want {
		t.Fatalf("runtime.overlayFreeX = %d, want %d", got, want)
	}
	if got, want := runtime.overlayFreeY, 360; got != want {
		t.Fatalf("runtime.overlayFreeY = %d, want %d", got, want)
	}
	if got, want := runtime.vocabularyDictionary, "kombi fire => Kombify"; got != want {
		t.Fatalf("runtime.vocabularyDictionary = %q, want %q", got, want)
	}
	if got, want := len(runtime.providers), 2; got != want {
		t.Fatalf("len(runtime.providers) = %d, want %d", got, want)
	}
	if got, want := runtime.dictateHotkeyBehavior, config.HotkeyBehaviorToggle; got != want {
		t.Fatalf("runtime.dictateHotkeyBehavior = %q, want %q", got, want)
	}
	if got, want := runtime.assistHotkeyBehavior, config.HotkeyBehaviorPushToTalk; got != want {
		t.Fatalf("runtime.assistHotkeyBehavior = %q, want %q", got, want)
	}
	if got, want := runtime.voiceAgentHotkeyBehavior, config.HotkeyBehaviorToggle; got != want {
		t.Fatalf("runtime.voiceAgentHotkeyBehavior = %q, want %q", got, want)
	}

	snapshot := state.engine.State()
	if got, want := snapshot.Hotkey, "ctrl+shift+d"; got != want {
		t.Fatalf("snapshot.Hotkey = %q, want %q", got, want)
	}
}

func TestAppStateApplyDesktopSettingsReconfiguresHotkey(t *testing.T) {
	hk := &fakeHotkeyReconfigurer{}
	state := &appState{
		dictateEnabled:    true,
		assistEnabled:     true,
		voiceAgentEnabled: true,
		hotkey:            "win+alt",
		dictateHotkey:     "win+alt",
		assistHotkey:      "ctrl+win+j",
		voiceAgentHotkey:  "ctrl+shift",
		audioDeviceID:     "mic-1",
		hkManager:         hk,
	}

	state.applyDesktopSettings(
		true,
		true,
		true,
		"win+alt",
		"ctrl+win+j",
		"ctrl+shift",
		true,
		true,
		true,
		"ctrl+shift+d",
		"ctrl+win+j",
		"ctrl+shift",
		"mic-1",
		"mic-1",
		true,
	)

	if got, want := len(hk.combos), 3; got != want {
		t.Fatalf("len(hk.combos) = %d, want %d", got, want)
	}
	if !state.overlayEnabled {
		t.Fatal("state.overlayEnabled = false, want true")
	}
}
