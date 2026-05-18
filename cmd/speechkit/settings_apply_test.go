package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/auditlog"
	"github.com/kombifyio/SpeechKit/internal/auditlogtest"
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
		config.Config{}, config.Config{},
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
		config.OverlayFeedbackModeBigProductivity,
		config.OverlayFeedbackModeSmallFeedback,
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
	if got, want := runtime.assistOverlayMode, config.OverlayFeedbackModeBigProductivity; got != want {
		t.Fatalf("runtime.assistOverlayMode = %q, want %q", got, want)
	}
	if got, want := runtime.voiceAgentOverlayMode, config.OverlayFeedbackModeSmallFeedback; got != want {
		t.Fatalf("runtime.voiceAgentOverlayMode = %q, want %q", got, want)
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

// TestEmitSettingsDiffEmitsOnePerChangedSection verifies that emitSettingsDiff
// emits exactly one settings.changed event per top-level section that differs,
// and skips unchanged sections.
func TestEmitSettingsDiffEmitsOnePerChangedSection(t *testing.T) {
	dir := t.TempDir()
	auditlog.Configure(true, dir, 90, false)

	old := config.Config{
		Update:    config.UpdateConfig{Enabled: true, CheckIntervalHours: 6, ManifestURL: "https://example.com/x"},
		Telemetry: config.TelemetryConfig{UpdateCheck: true},
		Routing:   config.RoutingConfig{Strategy: "dynamic"},
	}
	newCfg := old
	newCfg.Update.Enabled = false          // changed
	newCfg.Routing.Strategy = "local-only" // changed
	// Telemetry unchanged

	emitSettingsDiff(context.Background(), old, newCfg)

	// Close before reading so the file handle is released for TempDir cleanup.
	auditlogtest.Reset()

	dateKey := time.Now().UTC().Format("2006-01-02")
	data, _ := os.ReadFile(filepath.Join(dir, "audit-"+dateKey+".log"))
	body := string(data)

	if !strings.Contains(body, `"key_path":"update"`) {
		t.Errorf("want key_path=update emit, got: %s", body)
	}
	if !strings.Contains(body, `"key_path":"routing"`) {
		t.Errorf("want key_path=routing emit, got: %s", body)
	}
	if strings.Contains(body, `"key_path":"telemetry"`) {
		t.Errorf("did NOT want telemetry emit (unchanged), got: %s", body)
	}
}

// TestEmitSettingsDiffNoEventsWhenAllUnchanged verifies that emitSettingsDiff
// does not write any audit file when old and new configs are identical.
func TestEmitSettingsDiffNoEventsWhenAllUnchanged(t *testing.T) {
	dir := t.TempDir()
	auditlog.Configure(true, dir, 90, false)

	old := config.Config{
		Update: config.UpdateConfig{Enabled: true},
	}
	same := old

	emitSettingsDiff(context.Background(), old, same)

	// Close before reading dir so the file handle is released for TempDir cleanup.
	auditlogtest.Reset()

	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("want no audit file when nothing changed, got %d entries", len(entries))
	}
}
