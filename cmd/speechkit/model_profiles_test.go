package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
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
	if cfg.Routing.Strategy != "local-only" {
		t.Fatalf("routing strategy = %q, want %q", cfg.Routing.Strategy, "local-only")
	}
	if sttRouter.Local() == nil {
		t.Fatal("expected local provider to be configured on router")
	}
	if called != 1 {
		t.Fatalf("launchLocalProvider calls = %d, want 1", called)
	}
}

func TestApplySTTProfileHuggingFaceForcesCloudOnlyAndClearsLocalProvider(t *testing.T) {
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaultTestConfig()
	cfg.Local.Enabled = true
	cfg.Local.Model = "ggml-small.bin"
	cfg.Routing.Strategy = "local-only"
	cfg.HuggingFace.TokenEnv = "HF_TOKEN"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	t.Setenv("HF_TOKEN", "test-token")

	state := &appState{activeProfiles: map[string]string{}}
	sttRouter := &router.Router{Strategy: router.StrategyLocalOnly}
	sttRouter.SetLocal(&fakeProvider{name: "local"})
	profile := models.Profile{
		ID:            "stt.routed.whisper-large-v3",
		Name:          "Whisper Large v3 (Hugging Face)",
		Modality:      models.ModalitySTT,
		ExecutionMode: models.ExecutionModeHFRouted,
		ModelID:       "openai/whisper-large-v3",
	}

	if err := applySTTProfile(context.Background(), cfgPath, cfg, state, sttRouter, profile); err != nil {
		t.Fatalf("applySTTProfile: %v", err)
	}

	if got := cfg.Routing.Strategy; got != "cloud-only" {
		t.Fatalf("routing strategy = %q, want %q", got, "cloud-only")
	}
	if sttRouter.Local() != nil {
		t.Fatal("expected local provider to be cleared when a cloud STT profile is selected")
	}
	if sttRouter.HuggingFace() == nil {
		t.Fatal("expected hugging face provider to be configured on router")
	}
	if got := state.activeProfiles["stt"]; got != "stt.routed.whisper-large-v3" {
		t.Fatalf("active stt profile = %q, want %q", got, "stt.routed.whisper-large-v3")
	}
}
