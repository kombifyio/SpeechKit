package hotkey

import (
	"testing"
)

func TestIsKeyDown_NoKeysPressed(t *testing.T) {
	// When no keys are physically pressed, isKeyDown should return false.
	// Skip if a modifier key is detected (CI agents or terminals may hold Ctrl).
	if isKeyDown(VkControl) || isKeyDown(VkShift) || isKeyDown(VkMenu) {
		t.Skip("modifier key detected (likely held by terminal or CI); skipping hardware key-state test")
	}
	if isKeyDown(0x44) { // D
		t.Error("D key should not be pressed during automated test")
	}
}

func TestGetAsyncKeyState_APILoads(t *testing.T) {
	// Verify the Windows API proc loads without error
	if err := procGetAsyncKeyState.Find(); err != nil {
		t.Fatalf("GetAsyncKeyState not available: %v", err)
	}
}

func TestParseCombo(t *testing.T) {
	tests := []struct {
		input string
		want  []uint32
	}{
		{"alt", []uint32{VkLWin, VkMenu}},
		{"ctrl+shift", []uint32{VkControl, VkShift}},
		{"ctrl+shift+d", []uint32{VkControl, VkShift, 0x44}},
		{"win+alt", []uint32{VkLWin, VkMenu}},
		{"f8", []uint32{0x77}},
		{"unknown_combo", []uint32{VkLWin, VkMenu}}, // fallback to win+alt
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			keys := ParseCombo(tt.input)
			if len(keys) != len(tt.want) {
				t.Fatalf("ParseCombo(%q) returned %d keys, want %d", tt.input, len(keys), len(tt.want))
			}
			for i, want := range tt.want {
				if keys[i] != want {
					t.Fatalf("ParseCombo(%q)[%d] = %d, want %d", tt.input, i, keys[i], want)
				}
			}
		})
	}
}

func TestComboPresets(t *testing.T) {
	cs := ComboCtrlShift()
	if len(cs) != 2 || cs[0] != VkControl || cs[1] != VkShift {
		t.Errorf("ComboCtrlShift = %v", cs)
	}

	wa := ComboWinAlt()
	if len(wa) != 2 || wa[0] != VkLWin || wa[1] != VkMenu {
		t.Errorf("ComboWinAlt = %v", wa)
	}

	f8 := ComboF8()
	if len(f8) != 1 || f8[0] != 0x77 {
		t.Errorf("ComboF8 = %v", f8)
	}
}

func TestNewManager(t *testing.T) {
	m := NewManager(ParseCombo("win+alt"))
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if len(m.keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(m.keys))
	}
	if m.events == nil {
		t.Error("events channel is nil")
	}
}

func TestComboTrackerWinAlt(t *testing.T) {
	tracker := newComboTracker(ParseCombo("win+alt"))

	if event, ok := tracker.transition(VkLWin, true); ok {
		t.Fatalf("expected no event on Win down, got %#v", event)
	}

	event, ok := tracker.transition(VkMenu, true)
	if !ok || event.Type != EventKeyDown {
		t.Fatalf("expected EventKeyDown when Win+Alt becomes active, got %#v ok=%v", event, ok)
	}

	event, ok = tracker.transition(VkMenu, false)
	if !ok || event.Type != EventKeyUp {
		t.Fatalf("expected EventKeyUp on Alt release, got %#v ok=%v", event, ok)
	}
}

func TestComboTrackerCtrlShiftD(t *testing.T) {
	tracker := newComboTracker(ParseCombo("ctrl+shift+d"))

	if event, ok := tracker.transition(VkControl, true); ok {
		t.Fatalf("expected no event on ctrl down, got %#v", event)
	}
	if event, ok := tracker.transition(VkShift, true); ok {
		t.Fatalf("expected no event on shift down, got %#v", event)
	}

	event, ok := tracker.transition(0x44, true)
	if !ok || event.Type != EventKeyDown {
		t.Fatalf("expected EventKeyDown when combo becomes active, got %#v ok=%v", event, ok)
	}

	event, ok = tracker.transition(0x44, false)
	if !ok || event.Type != EventKeyUp {
		t.Fatalf("expected EventKeyUp when combo key releases, got %#v ok=%v", event, ok)
	}
}

func TestParseCombo_CtrlWin(t *testing.T) {
	keys := ParseCombo("ctrl+win")
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %v", len(keys), keys)
	}
	want := map[uint32]bool{VkControl: false, VkLWin: false}
	for _, k := range keys {
		if _, ok := want[k]; !ok {
			t.Fatalf("unexpected key 0x%02X in result %v", k, keys)
		}
		want[k] = true
	}
	for k, found := range want {
		if !found {
			t.Fatalf("missing expected key 0x%02X in result %v", k, keys)
		}
	}
}

func TestParseCombo_CaseInsensitive(t *testing.T) {
	upper := ParseCombo("CTRL+SHIFT")
	lower := ParseCombo("ctrl+shift")

	if len(upper) != len(lower) {
		t.Fatalf("length mismatch: upper=%v lower=%v", upper, lower)
	}
	for i := range upper {
		if upper[i] != lower[i] {
			t.Fatalf("mismatch at index %d: upper=0x%02X lower=0x%02X", i, upper[i], lower[i])
		}
	}
}

func TestParseCombo_EmptyString(t *testing.T) {
	keys := ParseCombo("")
	winAlt := ComboWinAlt()

	if len(keys) != len(winAlt) {
		t.Fatalf("empty string returned %v, want ComboWinAlt %v", keys, winAlt)
	}
	for i := range keys {
		if keys[i] != winAlt[i] {
			t.Fatalf("empty string [%d] = 0x%02X, want 0x%02X", i, keys[i], winAlt[i])
		}
	}
}

func TestParseCombo_FunctionKeys(t *testing.T) {
	tests := []struct {
		input string
		want  uint32
	}{
		{"f1", 0x70},
		{"f8", 0x77},
		{"f12", 0x7B},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			keys := ParseCombo(tt.input)
			if len(keys) != 1 {
				t.Fatalf("ParseCombo(%q) returned %d keys, want 1: %v", tt.input, len(keys), keys)
			}
			if keys[0] != tt.want {
				t.Fatalf("ParseCombo(%q)[0] = 0x%02X, want 0x%02X", tt.input, keys[0], tt.want)
			}
		})
	}
}

func TestParseCombo_SingleLetterKeys(t *testing.T) {
	tests := []struct {
		input string
		want  uint32
	}{
		{"d", 0x44},
		{"a", 0x41},
		{"z", 0x5A},
		{"5", 0x35},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			keys := ParseCombo(tt.input)
			if len(keys) != 1 {
				t.Fatalf("ParseCombo(%q) returned %d keys, want 1: %v", tt.input, len(keys), keys)
			}
			if keys[0] != tt.want {
				t.Fatalf("ParseCombo(%q)[0] = 0x%02X, want 0x%02X", tt.input, keys[0], tt.want)
			}
		})
	}
}

func TestNormalizeVK(t *testing.T) {
	tests := []struct {
		name string
		in   uint32
		want uint32
	}{
		{"VkLShift->VkShift", VkLShift, VkShift},
		{"VkRShift->VkShift", VkRShift, VkShift},
		{"VkLControl->VkControl", VkLControl, VkControl},
		{"VkRControl->VkControl", VkRControl, VkControl},
		{"VkLMenu->VkMenu", VkLMenu, VkMenu},
		{"VkRMenu->VkMenu", VkRMenu, VkMenu},
		{"VkRWin->VkLWin", VkRWin, VkLWin},
		{"VkLWin unchanged", VkLWin, VkLWin},
		{"regular key unchanged", 0x44, 0x44},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeVK(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeVK(0x%02X) = 0x%02X, want 0x%02X", tt.in, got, tt.want)
			}
		})
	}
}

func TestComboTrackerIgnoresUnrelatedKeys(t *testing.T) {
	tracker := newComboTracker(ParseCombo("ctrl+shift"))

	// Pressing 'D' (0x44) should not produce any event because it is not part of the combo.
	if event, ok := tracker.transition(0x44, true); ok {
		t.Fatalf("expected no event for unrelated key down, got %#v", event)
	}
	if event, ok := tracker.transition(0x44, false); ok {
		t.Fatalf("expected no event for unrelated key up, got %#v", event)
	}
}

func TestComboTrackerDoubleDownNoRefire(t *testing.T) {
	tracker := newComboTracker(ParseCombo("win+alt"))

	// Press both keys to activate the combo.
	if _, ok := tracker.transition(VkLWin, true); ok {
		t.Fatal("unexpected event on first Win down")
	}
	event, ok := tracker.transition(VkMenu, true)
	if !ok || event.Type != EventKeyDown {
		t.Fatalf("expected EventKeyDown on combo activation, got %#v ok=%v", event, ok)
	}

	// Send duplicate keydown events (auto-repeat). Should not emit another EventKeyDown.
	if event, ok := tracker.transition(VkLWin, true); ok {
		t.Fatalf("expected no event on duplicate Win down, got %#v", event)
	}
	if event, ok := tracker.transition(VkMenu, true); ok {
		t.Fatalf("expected no event on duplicate Alt down, got %#v", event)
	}
}
