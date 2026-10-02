package config

import "testing"

func TestNormalizeVoiceAgentWarmLinger(t *testing.T) {
	cases := []struct {
		name     string
		value    int
		fallback int
		want     int
	}{
		{"zero disables", 0, 60, 0},
		{"in range", 45, 60, 45},
		{"max", 120, 60, 120},
		{"clamped above max", 500, 60, 120},
		{"negative falls back", -1, 60, 60},
		{"negative with negative fallback", -1, -5, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeVoiceAgentWarmLinger(tc.value, tc.fallback); got != tc.want {
				t.Fatalf("NormalizeVoiceAgentWarmLinger(%d, %d) = %d, want %d", tc.value, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestNormalizeVoiceAgentPauseTolerance(t *testing.T) {
	cases := []struct {
		name     string
		value    int
		fallback int
		want     int
	}{
		{"zero disables", 0, 800, 0},
		{"in range", 1200, 800, 1200},
		{"max", 3000, 800, 3000},
		{"clamped above max", 9000, 800, 3000},
		{"negative falls back", -1, 800, 800},
		{"negative with negative fallback", -1, -5, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeVoiceAgentPauseTolerance(tc.value, tc.fallback); got != tc.want {
				t.Fatalf("NormalizeVoiceAgentPauseTolerance(%d, %d) = %d, want %d", tc.value, tc.fallback, got, tc.want)
			}
		})
	}
}

// TestVoiceAgentDialogDefaults pins the out-of-the-box dialog behaviour: a
// 10 s hold-to-talk resume window and an 800 ms mic pause tolerance.
//
// The resume window was 60 s until 2026-08-13. Releasing the key ends the
// conversation, so this window exists only to let an immediate re-press skip
// the provider handshake — a minute of warm session outlives any sense that
// the dialog is over and kept the overlay feeling alive after it ended.
func TestVoiceAgentDialogDefaults(t *testing.T) {
	cfg := defaults()
	if cfg.VoiceAgent.WarmSessionLingerSec != 10 {
		t.Fatalf("default WarmSessionLingerSec = %d, want 10", cfg.VoiceAgent.WarmSessionLingerSec)
	}
	if cfg.VoiceAgent.PauseToleranceMs != 800 {
		t.Fatalf("default PauseToleranceMs = %d, want 800", cfg.VoiceAgent.PauseToleranceMs)
	}
}

func TestInstallStateHintDismissal(t *testing.T) {
	state := &InstallState{}
	if state.HintDismissed("summary_model_download") {
		t.Fatal("fresh state must not report a dismissed hint")
	}
	if !state.DismissHint("summary_model_download") {
		t.Fatal("first dismissal must report a change")
	}
	if state.DismissHint("summary_model_download") {
		t.Fatal("second dismissal of the same hint must be a no-op")
	}
	if !state.HintDismissed("summary_model_download") {
		t.Fatal("hint must be reported dismissed after DismissHint")
	}
	if state.DismissHint("") {
		t.Fatal("empty hint id must be rejected")
	}

	var nilState *InstallState
	if nilState.HintDismissed("x") || nilState.DismissHint("x") {
		t.Fatal("nil state must be a safe no-op")
	}
}
