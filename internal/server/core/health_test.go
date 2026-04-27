//go:build linux

package core

import (
	"testing"
)

func TestHealthRegistry_InitialOverallIsStarting(t *testing.T) {
	r := NewHealthRegistry()
	overall, components, _ := r.Snapshot()
	if overall != StatusStarting {
		t.Fatalf("empty registry should report StatusStarting, got %q", overall)
	}
	if len(components) != 0 {
		t.Fatalf("empty registry should have zero components, got %d", len(components))
	}
}

func TestHealthRegistry_SetReady(t *testing.T) {
	r := NewHealthRegistry()
	r.SetReady("server", StatusOK, "listening")
	overall, components, _ := r.Snapshot()
	if overall != StatusOK {
		t.Fatalf("single OK component should make overall OK, got %q", overall)
	}
	if got := components["server"].Status; got != StatusOK {
		t.Fatalf("server component should be OK, got %q", got)
	}
	if got := components["server"].Detail; got != "listening" {
		t.Fatalf("detail should be propagated, got %q", got)
	}
}

func TestHealthRegistry_WorstStatusWins(t *testing.T) {
	r := NewHealthRegistry()
	r.SetReady("a", StatusOK, "")
	r.SetReady("b", StatusDegraded, "slow")
	r.SetReady("c", StatusUnavailable, "down")
	overall, _, _ := r.Snapshot()
	if overall != StatusUnavailable {
		t.Fatalf("expected worst status (unavailable) to win, got %q", overall)
	}
}

func TestResolveModes_EmptyMeansAll(t *testing.T) {
	modes := resolveModes(nil)
	for _, want := range []Mode{ModeDictation, ModeAssist, ModeVoiceAgent} {
		if !modes[want] {
			t.Fatalf("mode %q should default to enabled", want)
		}
	}
}

func TestResolveModes_Subset(t *testing.T) {
	modes := resolveModes([]string{"dictation", "voiceagent"})
	if !modes[ModeDictation] || !modes[ModeVoiceAgent] {
		t.Fatalf("configured modes should be enabled")
	}
	if modes[ModeAssist] {
		t.Fatalf("assist should stay disabled when not listed")
	}
}

func TestResolveModes_Normalizes(t *testing.T) {
	modes := resolveModes([]string{"DICTATION", "  Assist "})
	if !modes[ModeDictation] || !modes[ModeAssist] {
		t.Fatalf("case and whitespace should be tolerated")
	}
}

func TestApp_ModeEnabled(t *testing.T) {
	app := &App{Modes: map[Mode]bool{ModeAssist: true, ModeDictation: false}}
	if !app.ModeEnabled(ModeAssist) {
		t.Fatalf("expected ModeEnabled(assist) to be true")
	}
	if app.ModeEnabled(ModeDictation) {
		t.Fatalf("expected ModeEnabled(dictation) to be false")
	}
	var nilApp *App
	if nilApp.ModeEnabled(ModeAssist) {
		t.Fatalf("nil App should always report false")
	}
}
