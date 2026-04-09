package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
)

func TestApplySTTProfileLocalLaunchesLocalProvider(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Local.Port = 8080
	cfg.Local.Model = "ggml-small.bin"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	state := &appState{activeProfiles: map[string]string{}}
	sttRouter := &router.Router{}
	profile := models.Profile{
		ID:            "stt.local.whispercpp",
		Name:          "Whisper.cpp (Bundled Local)",
		Modality:      models.ModalitySTT,
		ExecutionMode: models.ExecutionModeLocal,
		ModelID:       "whisper.cpp",
	}

	called := 0
	previousLauncher := launchLocalProvider
	launchLocalProvider = func(ctx context.Context, state *appState, r *router.Router, provider localProviderStarter) {
		called++
	}
	defer func() { launchLocalProvider = previousLauncher }()

	if err := applySTTProfile(context.Background(), cfgPath, cfg, state, sttRouter, profile); err != nil {
		t.Fatalf("applySTTProfile: %v", err)
	}

	if !cfg.Local.Enabled {
		t.Fatal("expected local provider to be enabled")
	}
	if cfg.Routing.Strategy != "dynamic" {
		t.Fatalf("routing strategy = %q, want %q", cfg.Routing.Strategy, "dynamic")
	}
	if sttRouter.Local() == nil {
		t.Fatal("expected local provider to be configured on router")
	}
	if called != 1 {
		t.Fatalf("launchLocalProvider calls = %d, want 1", called)
	}
}
