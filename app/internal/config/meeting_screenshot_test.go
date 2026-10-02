package config

import (
	"errors"
	"testing"
)

func TestNormalizeMeetingScreenshotHotkey(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"empty falls back to default", "", DefaultMeetingScreenshotHotkey},
		{"whitespace falls back to default", "   ", DefaultMeetingScreenshotHotkey},
		{"none disables", "none", DisabledMeetingScreenshotHotkey},
		{"off disables", "OFF", DisabledMeetingScreenshotHotkey},
		{"valid combo is lowercased", "Ctrl+Alt+S", "ctrl+alt+s"},
		{"another valid combo", "ctrl+shift+p", "ctrl+shift+p"},
		{"garbage falls back to default", "not-a-key+", DefaultMeetingScreenshotHotkey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeMeetingScreenshotHotkey(tc.input); got != tc.want {
				t.Fatalf("NormalizeMeetingScreenshotHotkey(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestDefaultMeetingScreenshotConfig(t *testing.T) {
	cfg := defaults()
	if !cfg.Meeting.ScreenshotEnabled {
		t.Fatal("expected meeting screenshot quick action enabled by default")
	}
	if !cfg.Meeting.ScreenshotHotkeyEnabled {
		t.Fatal("expected meeting screenshot hotkey enabled by default")
	}
	if got := NormalizeMeetingScreenshotHotkey(cfg.Meeting.ScreenshotHotkey); got != DefaultMeetingScreenshotHotkey {
		t.Fatalf("default meeting screenshot hotkey = %q, want %q", got, DefaultMeetingScreenshotHotkey)
	}
}

func TestMeetingScreenshotHotkeyConflictDetection(t *testing.T) {
	cfg := defaults()
	cfg.General.DictateHotkey = "ctrl+win"
	cfg.General.AssistHotkey = "win+alt"
	cfg.Meeting.ScreenshotHotkey = "ctrl+alt+s"
	if conflict := MeetingScreenshotHotkeyConflict(cfg); conflict != "" {
		t.Fatalf("expected no conflict, got %q", conflict)
	}

	// Order-independent collision with the assist hotkey.
	cfg.Meeting.ScreenshotHotkey = "alt+win"
	if conflict := MeetingScreenshotHotkeyConflict(cfg); conflict != "assist" {
		t.Fatalf("expected assist conflict, got %q", conflict)
	}

	// A disabled shortcut never conflicts.
	cfg.Meeting.ScreenshotHotkey = "none"
	if conflict := MeetingScreenshotHotkeyConflict(cfg); conflict != "" {
		t.Fatalf("expected no conflict for disabled shortcut, got %q", conflict)
	}
}

func TestApplyMeetingScreenshotHotkeyRollsBackOnConflict(t *testing.T) {
	cfg := defaults()
	cfg.General.DictateHotkey = "ctrl+win"
	cfg.Meeting.ScreenshotHotkey = "ctrl+alt+s"

	// A non-conflicting change commits.
	applied, err := ApplyMeetingScreenshotHotkey(cfg, "ctrl+alt+p")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if applied != "ctrl+alt+p" || cfg.Meeting.ScreenshotHotkey != "ctrl+alt+p" {
		t.Fatalf("expected applied ctrl+alt+p, got %q (cfg %q)", applied, cfg.Meeting.ScreenshotHotkey)
	}

	// A conflicting change is rejected and the previous value is restored.
	applied, err = ApplyMeetingScreenshotHotkey(cfg, "win+ctrl")
	if !errors.Is(err, ErrMeetingScreenshotHotkeyConflict) {
		t.Fatalf("expected conflict error, got %v", err)
	}
	if applied != "ctrl+alt+p" || cfg.Meeting.ScreenshotHotkey != "ctrl+alt+p" {
		t.Fatalf("expected rollback to ctrl+alt+p, got %q (cfg %q)", applied, cfg.Meeting.ScreenshotHotkey)
	}
}

func TestApplyMeetingScreenshotHotkeyDisableRoundTrips(t *testing.T) {
	cfg := defaults()

	applied, err := ApplyMeetingScreenshotHotkey(cfg, "off")
	if err != nil {
		t.Fatalf("disable shortcut: %v", err)
	}
	if applied != DisabledMeetingScreenshotHotkey || cfg.Meeting.ScreenshotHotkey != DisabledMeetingScreenshotHotkey {
		t.Fatalf("disabled shortcut = %q (cfg %q), want %q", applied, cfg.Meeting.ScreenshotHotkey, DisabledMeetingScreenshotHotkey)
	}
	if got := NormalizeMeetingScreenshotHotkey(cfg.Meeting.ScreenshotHotkey); got != DisabledMeetingScreenshotHotkey {
		t.Fatalf("disabled shortcut normalized after persistence = %q, want %q", got, DisabledMeetingScreenshotHotkey)
	}
	if conflict := MeetingScreenshotHotkeyConflict(cfg); conflict != "" {
		t.Fatalf("disabled shortcut conflict = %q, want none", conflict)
	}
}
