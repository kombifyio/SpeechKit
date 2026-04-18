package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
)

func TestSelectDownloadedLocalModelUpdatesConfigAndReloadsLocalProvider(t *testing.T) {
	modelsDir := t.TempDir()
	installTestWhisperBinary(t)
	writeValidWhisperModelFile(t, filepath.Join(modelsDir, "ggml-small.bin"))
	writeValidWhisperModelFile(t, filepath.Join(modelsDir, "ggml-large-v3.bin"))

	cfg := defaultTestConfig()
	cfg.Local.Enabled = true
	cfg.Local.Port = 8080
	cfg.Local.Model = "ggml-small.bin"
	cfg.Local.ModelPath = filepath.Join(modelsDir, "ggml-small.bin")
	cfg.Routing.Strategy = "dynamic"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	state := &appState{
		activeProfiles: map[string]string{},
		sttRouter:      &router.Router{},
	}
	handler := assetHandler(cfg, cfgPath, state, state.sttRouter, nil, &config.InstallState{Mode: config.InstallModeLocal})

	called := 0
	previousLauncher := launchLocalProvider
	launchLocalProvider = func(ctx context.Context, state *appState, r *router.Router, provider localProviderStarter) {
		called++
	}
	defer func() { launchLocalProvider = previousLauncher }()

	form := url.Values{"model_id": {"whisper.ggml-large-v3"}}
	req := httptest.NewRequest(http.MethodPost, "/models/downloads/select", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := cfg.Local.Model; got != "ggml-large-v3.bin" {
		t.Fatalf("local model = %q, want %q", got, "ggml-large-v3.bin")
	}
	wantPath := filepath.Join(modelsDir, "ggml-large-v3.bin")
	if got := cfg.Local.ModelPath; got != wantPath {
		t.Fatalf("local model path = %q, want %q", got, wantPath)
	}
	if state.sttRouter.Local() == nil {
		t.Fatal("expected local provider to be configured on router")
	}
	if called != 1 {
		t.Fatalf("launchLocalProvider calls = %d, want 1", called)
	}
	if got := state.activeProfiles["stt"]; got != "stt.local.whispercpp" {
		t.Fatalf("active stt profile = %q, want %q", got, "stt.local.whispercpp")
	}
}

func TestSelectDownloadedLocalModelReactivatesLocalSTTAfterCloudSelection(t *testing.T) {
	modelsDir := t.TempDir()
	installTestWhisperBinary(t)
	writeValidWhisperModelFile(t, filepath.Join(modelsDir, "ggml-small.bin"))
	writeValidWhisperModelFile(t, filepath.Join(modelsDir, "ggml-large-v3.bin"))

	cfg := defaultTestConfig()
	cfg.Local.Enabled = false
	cfg.Local.Port = 8080
	cfg.Local.Model = "ggml-small.bin"
	cfg.Local.ModelPath = filepath.Join(modelsDir, "ggml-small.bin")
	cfg.Routing.Strategy = "cloud-only"
	cfg.HuggingFace.Enabled = true
	cfg.HuggingFace.TokenEnv = "HF_TOKEN"
	cfg.HuggingFace.Model = "openai/whisper-large-v3"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")
	t.Setenv("HF_TOKEN", "test-token")

	state := &appState{
		activeProfiles: map[string]string{},
		sttRouter:      &router.Router{},
	}
	handler := assetHandler(cfg, cfgPath, state, state.sttRouter, nil, &config.InstallState{Mode: config.InstallModeLocal})

	called := 0
	previousLauncher := launchLocalProvider
	launchLocalProvider = func(ctx context.Context, state *appState, r *router.Router, provider localProviderStarter) {
		called++
	}
	defer func() { launchLocalProvider = previousLauncher }()

	form := url.Values{"model_id": {"whisper.ggml-large-v3"}}
	req := httptest.NewRequest(http.MethodPost, "/models/downloads/select", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !cfg.Local.Enabled {
		t.Fatal("expected local STT to be enabled after selecting a local model")
	}
	if got := cfg.Routing.Strategy; got != "local-only" {
		t.Fatalf("routing strategy = %q, want %q", got, "local-only")
	}
	if got := cfg.Local.Model; got != "ggml-large-v3.bin" {
		t.Fatalf("local model = %q, want %q", got, "ggml-large-v3.bin")
	}
	if got := state.activeProfiles["stt"]; got != "stt.local.whispercpp" {
		t.Fatalf("active stt profile = %q, want %q", got, "stt.local.whispercpp")
	}
	if state.sttRouter.Local() == nil {
		t.Fatal("expected local provider to be configured on router")
	}
	if called != 1 {
		t.Fatalf("launchLocalProvider calls = %d, want 1", called)
	}
}

func TestSelectDownloadedLocalModelDetachesCanceledContextForLocalStartup(t *testing.T) {
	modelsDir := t.TempDir()
	installTestWhisperBinary(t)
	writeValidWhisperModelFile(t, filepath.Join(modelsDir, "ggml-large-v3.bin"))

	cfg := defaultTestConfig()
	cfg.Local.Enabled = true
	cfg.Local.Port = 8080
	cfg.Local.ModelPath = filepath.Join(modelsDir, "ggml-large-v3.bin")
	cfg.Routing.Strategy = "local-only"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	state := &appState{
		activeProfiles: map[string]string{},
		sttRouter:      &router.Router{},
	}

	item, ok := downloadCatalogItem(t.Context(), cfg, "whisper.ggml-large-v3")
	if !ok {
		t.Fatal("expected local download catalog item to exist")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var launchErr error
	previousLauncher := launchLocalProvider
	launchLocalProvider = func(ctx context.Context, state *appState, r *router.Router, provider localProviderStarter) {
		launchErr = ctx.Err()
	}
	defer func() { launchLocalProvider = previousLauncher }()

	if err := selectDownloadedLocalModel(ctx, cfgPath, cfg, state, item); err != nil {
		t.Fatalf("selectDownloadedLocalModel: %v", err)
	}

	if launchErr != nil {
		t.Fatalf("launch context err = %v, want nil", launchErr)
	}
}

func TestSelectDownloadedLocalModelRejectsMissingWhisperBinary(t *testing.T) {
	modelsDir := t.TempDir()
	writeValidWhisperModelFile(t, filepath.Join(modelsDir, "ggml-small.bin"))
	writeValidWhisperModelFile(t, filepath.Join(modelsDir, "ggml-large-v3.bin"))

	t.Setenv("LOCALAPPDATA", t.TempDir())

	cfg := defaultTestConfig()
	cfg.Local.Enabled = true
	cfg.Local.Port = 8080
	cfg.Local.Model = "ggml-small.bin"
	cfg.Local.ModelPath = filepath.Join(modelsDir, "ggml-small.bin")
	cfg.Routing.Strategy = "local-only"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	state := &appState{
		activeProfiles: map[string]string{},
		sttRouter:      &router.Router{},
	}
	handler := assetHandler(cfg, cfgPath, state, state.sttRouter, nil, &config.InstallState{Mode: config.InstallModeLocal})

	form := url.Values{"model_id": {"whisper.ggml-large-v3"}}
	req := httptest.NewRequest(http.MethodPost, "/models/downloads/select", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(strings.ToLower(body), "whisper-server binary missing") {
		t.Fatalf("body = %q, want whisper-server binary missing", body)
	}
	if got := cfg.Local.Model; got != "ggml-small.bin" {
		t.Fatalf("local model = %q, want unchanged %q", got, "ggml-small.bin")
	}
}
