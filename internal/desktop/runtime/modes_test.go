package runtime

import "testing"

func TestNormalizeModeMapsLegacyAgentToSelectedAgentMode(t *testing.T) {
	if got := NormalizeMode(ModeAgent, ModeVoiceAgent); got != ModeVoiceAgent {
		t.Fatalf("legacy agent mode = %q, want %q", got, ModeVoiceAgent)
	}
	if got := NormalizeMode("unknown", ModeVoiceAgent); got != ModeNone {
		t.Fatalf("unknown mode = %q, want %q", got, ModeNone)
	}
}

func TestDeriveLegacyAgentModePrefersActiveBoundMode(t *testing.T) {
	if got := DeriveLegacyAgentMode("ctrl+win+j", "ctrl+win+v", ModeVoiceAgent, ModeAssist); got != ModeVoiceAgent {
		t.Fatalf("derived mode = %q, want %q", got, ModeVoiceAgent)
	}
}

func TestLegacyAgentHotkeyFollowsLegacyAgentMode(t *testing.T) {
	if got := LegacyAgentHotkey(" assist ", " voice ", ModeVoiceAgent); got != "voice" {
		t.Fatalf("legacy hotkey = %q, want voice", got)
	}
}
