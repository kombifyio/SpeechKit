package main

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestStartDesktopWakeword_DisabledIsNoop(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.Enabled = false

	got := startDesktopWakeword(context.Background(), cfg, &appState{}, nil, nil)
	if got != nil {
		t.Fatalf("expected nil runtime when disabled, got %+v", got)
	}
}

func TestStartDesktopWakeword_MissingHotkeyManagerIsNoop(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.Enabled = true

	state := &appState{}
	got := startDesktopWakeword(context.Background(), cfg, state, nil, nil)
	if got != nil {
		t.Fatalf("expected nil runtime when hotkey manager missing, got %+v", got)
	}
}

func TestResolveWakeword_MissingModelDirSurfacesError(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.Enabled = true

	_, err := resolveWakeword(cfg)
	if err == nil {
		t.Fatal("expected error when wakeword-kws bundle directory is absent")
	}
}
