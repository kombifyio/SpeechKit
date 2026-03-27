package tray

import "testing"

func TestStateTooltipsExistForAllStates(t *testing.T) {
	for _, state := range []State{StateIdle, StateRecording, StateProcessing, StateDone} {
		tooltip, ok := stateTooltips[state]
		if !ok || tooltip == "" {
			t.Fatalf("missing tooltip for state %q", state)
		}
	}
}

func TestIdleTooltipIsSpeechKit(t *testing.T) {
	if stateTooltips[StateIdle] != "SpeechKit" {
		t.Fatalf("idle tooltip = %q, want %q", stateTooltips[StateIdle], "SpeechKit")
	}
}

func TestRecordingTooltipDiffersFromIdle(t *testing.T) {
	if stateTooltips[StateRecording] == stateTooltips[StateIdle] {
		t.Fatal("recording tooltip should differ from idle")
	}
}
