package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/downloads"
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

func TestSelectDownloadedLocalModelUsesDefaultModelDownloadDir(t *testing.T) {
	modelsDir := t.TempDir()
	legacyDir := t.TempDir()
	installTestWhisperBinary(t)
	writeValidWhisperModelFile(t, filepath.Join(modelsDir, "ggml-large-v3.bin"))

	cfg := defaultTestConfig()
	cfg.General.ModelDownloadDir = modelsDir
	cfg.Local.Enabled = true
	cfg.Local.Port = 8080
	cfg.Local.Model = "ggml-small.bin"
	cfg.Local.ModelPath = filepath.Join(legacyDir, "ggml-small.bin")
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
	wantPath := filepath.Join(modelsDir, "ggml-large-v3.bin")
	if got := cfg.Local.ModelPath; got != wantPath {
		t.Fatalf("local model path = %q, want %q", got, wantPath)
	}
	if called != 1 {
		t.Fatalf("launchLocalProvider calls = %d, want 1", called)
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

func TestSelectDownloadedOllamaModelActivatesLocalProviderProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{{"name": "gemma4:e4b"}},
		})
	}))
	defer srv.Close()

	oldOllamaBaseURL := downloads.OllamaBaseURL
	downloads.OllamaBaseURL = srv.URL
	defer func() { downloads.OllamaBaseURL = oldOllamaBaseURL }()

	cfg := defaultTestConfig()
	cfg.Providers.Ollama.BaseURL = srv.URL
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{
		activeProfiles: map[string]string{},
		sttRouter:      &router.Router{},
	}
	handler := assetHandler(cfg, cfgPath, state, state.sttRouter, nil, &config.InstallState{Mode: config.InstallModeLocal})

	form := url.Values{"model_id": {"ollama.gemma4-e4b-assist"}}
	req := httptest.NewRequest(http.MethodPost, "/models/downloads/select", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !cfg.Providers.Ollama.Enabled {
		t.Fatal("expected Ollama provider to be enabled")
	}
	if cfg.LocalLLM.Enabled {
		t.Fatal("Ollama selection must not enable built-in local LLM")
	}
	if got := cfg.Providers.Ollama.AssistModel; got != "gemma4:e4b" {
		t.Fatalf("ollama assist model = %q, want %q", got, "gemma4:e4b")
	}
	if got := state.activeProfiles["assist"]; got != "assist.ollama.gemma4-e4b" {
		t.Fatalf("active assist profile = %q, want %q", got, "assist.ollama.gemma4-e4b")
	}
	if state.genkitRT == nil {
		t.Fatal("expected AI runtime to be reloaded")
	}
}

func TestSelectDownloadedOllamaDictationModelActivatesSTTProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{{"name": "gemma4:e4b"}},
		})
	}))
	defer srv.Close()

	oldOllamaBaseURL := downloads.OllamaBaseURL
	downloads.OllamaBaseURL = srv.URL
	defer func() { downloads.OllamaBaseURL = oldOllamaBaseURL }()

	cfg := defaultTestConfig()
	cfg.Providers.Ollama.BaseURL = srv.URL
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{
		activeProfiles: map[string]string{},
		sttRouter:      &router.Router{},
	}
	handler := assetHandler(cfg, cfgPath, state, state.sttRouter, nil, &config.InstallState{Mode: config.InstallModeLocal})

	form := url.Values{"model_id": {"ollama.gemma4-e4b-dictate"}}
	req := httptest.NewRequest(http.MethodPost, "/models/downloads/select", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !cfg.Providers.Ollama.Enabled {
		t.Fatal("expected Ollama provider to be enabled")
	}
	if got := cfg.Providers.Ollama.STTModel; got != "gemma4:e4b" {
		t.Fatalf("ollama stt model = %q, want %q", got, "gemma4:e4b")
	}
	if got := cfg.Routing.Strategy; got != "cloud-only" {
		t.Fatalf("routing strategy = %q, want %q", got, "cloud-only")
	}
	if provider := state.sttRouter.Cloud("ollama"); provider == nil {
		t.Fatal("expected ollama STT provider to be configured on router")
	}
	if got := state.activeProfiles["stt"]; got != "stt.ollama.gemma4-e4b-transcribe" {
		t.Fatalf("active stt profile = %q, want %q", got, "stt.ollama.gemma4-e4b-transcribe")
	}
}

func TestSelectDownloadedLlamaCppAssistModelUpdatesLocalLLMConfig(t *testing.T) {
	modelsDir := t.TempDir()
	modelFile := filepath.Join(modelsDir, "gemma-3-4b-it-Q4_K_M.gguf")
	if err := os.WriteFile(modelFile, []byte("gguf"), 0o600); err != nil {
		t.Fatalf("write model file: %v", err)
	}

	cfg := defaultTestConfig()
	cfg.General.ModelDownloadDir = modelsDir
	cfg.LocalLLM.Enabled = false
	cfg.LocalLLM.Model = "gemma4:e4b"
	cfg.LocalLLM.AssistModel = "gemma4:e4b"
	cfg.Providers.Ollama.Enabled = true
	cfg.Providers.Ollama.AssistModel = "gemma4:e4b"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{
		activeProfiles: map[string]string{},
		sttRouter:      &router.Router{},
	}
	handler := assetHandler(cfg, cfgPath, state, state.sttRouter, nil, &config.InstallState{Mode: config.InstallModeLocal})

	form := url.Values{"model_id": {"llamacpp.gemma-3-4b-it-q4-k-m"}}
	req := httptest.NewRequest(http.MethodPost, "/models/downloads/select", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !cfg.LocalLLM.Enabled {
		t.Fatal("expected built-in local LLM to be enabled")
	}
	if got := cfg.LocalLLM.ModelPath; got != modelFile {
		t.Fatalf("local LLM model path = %q, want %q", got, modelFile)
	}
	if got := cfg.LocalLLM.Model; got != "gemma-3-4b-it-Q4_K_M.gguf" {
		t.Fatalf("local LLM model = %q, want downloaded GGUF filename", got)
	}
	if got := cfg.LocalLLM.AssistModel; got != "gemma-3-4b-it-Q4_K_M.gguf" {
		t.Fatalf("local LLM assist model = %q, want downloaded GGUF filename", got)
	}
	if got := cfg.ModelSelection.Assist.PrimaryProfileID; got != "assist.builtin.gemma4-e4b" {
		t.Fatalf("assist primary profile = %q, want %q", got, "assist.builtin.gemma4-e4b")
	}
	if got := cfg.ModelSelection.Assist.FallbackProfileID; got != "" {
		t.Fatalf("assist fallback profile = %q, want empty", got)
	}
	if got := cfg.Providers.Ollama.AssistModel; got != "" {
		t.Fatalf("ollama assist model = %q, want cleared", got)
	}
	if got := state.activeProfiles["assist"]; got != "assist.builtin.gemma4-e4b" {
		t.Fatalf("active assist profile = %q, want %q", got, "assist.builtin.gemma4-e4b")
	}
	if state.genkitRT == nil {
		t.Fatal("expected AI runtime to be reloaded")
	}
}

func TestSelectDownloadedLlamaCppVoiceModelUpdatesLocalPipelineConfig(t *testing.T) {
	modelsDir := t.TempDir()
	modelFile := filepath.Join(modelsDir, "gemma-3-4b-it-Q4_K_M.gguf")
	if err := os.WriteFile(modelFile, []byte("gguf"), 0o600); err != nil {
		t.Fatalf("write model file: %v", err)
	}

	cfg := defaultTestConfig()
	cfg.General.ModelDownloadDir = modelsDir
	cfg.LocalLLM.Enabled = false
	cfg.LocalLLM.Model = "gemma4:e4b"
	cfg.LocalLLM.AgentModel = "gemma4:e4b"
	cfg.ModelSelection = config.BuiltInPrimaryModelSelectionDefaults()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{
		activeProfiles: map[string]string{},
		sttRouter:      &router.Router{},
	}
	handler := assetHandler(cfg, cfgPath, state, state.sttRouter, nil, &config.InstallState{Mode: config.InstallModeLocal})

	form := url.Values{"model_id": {"llamacpp.gemma-3-4b-it-q4-k-m-voice"}}
	req := httptest.NewRequest(http.MethodPost, "/models/downloads/select", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !cfg.LocalLLM.Enabled {
		t.Fatal("expected built-in local LLM to be enabled")
	}
	if got := cfg.LocalLLM.ModelPath; got != modelFile {
		t.Fatalf("local LLM model path = %q, want %q", got, modelFile)
	}
	if got := cfg.LocalLLM.AgentModel; got != "gemma-3-4b-it-Q4_K_M.gguf" {
		t.Fatalf("local LLM agent model = %q, want downloaded GGUF filename", got)
	}
	if got := cfg.VoiceAgent.Model; got != "speechkit-local-voice-pipeline" {
		t.Fatalf("voice agent model = %q, want stable local pipeline model", got)
	}
	if !cfg.VoiceAgent.PipelineFallback {
		t.Fatal("expected Voice Agent pipeline fallback to be enabled")
	}
	if got := cfg.ModelSelection.VoiceAgent.PrimaryProfileID; got != "realtime.builtin.pipeline" {
		t.Fatalf("voice agent primary profile = %q, want %q", got, "realtime.builtin.pipeline")
	}
	if got := cfg.ModelSelection.VoiceAgent.FallbackProfileID; got != "" {
		t.Fatalf("voice agent fallback profile = %q, want empty", got)
	}
	if got := state.activeProfiles["realtime_voice"]; got != "realtime.builtin.pipeline" {
		t.Fatalf("active realtime voice profile = %q, want %q", got, "realtime.builtin.pipeline")
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
