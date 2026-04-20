package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

func TestApplySTTProfileLocalLaunchesLocalProvider(t *testing.T) {
	installTestWhisperBinary(t)
	modelPath := filepath.Join(t.TempDir(), "ggml-small.bin")
	writeValidWhisperModelFile(t, modelPath)

	cfg := defaultTestConfig()
	cfg.Local.Port = 8080
	cfg.Local.Model = "ggml-small.bin"
	cfg.Local.ModelPath = modelPath
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

func TestApplySTTProfileOllamaRegistersSelfHostedProvider(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Providers.Ollama.BaseURL = "http://127.0.0.1:11434"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	state := &appState{activeProfiles: map[string]string{}}
	sttRouter := &router.Router{}
	profile := models.Profile{
		ID:            "stt.ollama.gemma4-e4b-transcribe",
		Name:          "Gemma 4 E4B Transcribe (Ollama)",
		Modality:      models.ModalitySTT,
		ExecutionMode: models.ExecutionModeOllama,
		ModelID:       "gemma4:e4b",
	}

	if err := applySTTProfile(context.Background(), cfgPath, cfg, state, sttRouter, profile); err != nil {
		t.Fatalf("applySTTProfile: %v", err)
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
	if provider := sttRouter.Cloud("ollama"); provider == nil {
		t.Fatal("expected ollama STT provider to be configured on router")
	}
	if got := state.activeProfiles["stt"]; got != "stt.ollama.gemma4-e4b-transcribe" {
		t.Fatalf("active stt profile = %q, want %q", got, "stt.ollama.gemma4-e4b-transcribe")
	}
}

func TestApplySTTProfileLocalDetachesCanceledContextForStartup(t *testing.T) {
	installTestWhisperBinary(t)
	modelPath := filepath.Join(t.TempDir(), "ggml-small.bin")
	writeValidWhisperModelFile(t, modelPath)

	cfg := defaultTestConfig()
	cfg.Local.Port = 8080
	cfg.Local.Model = "ggml-small.bin"
	cfg.Local.ModelPath = modelPath
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var launchErr error
	previousLauncher := launchLocalProvider
	launchLocalProvider = func(ctx context.Context, state *appState, r *router.Router, provider localProviderStarter) {
		launchErr = ctx.Err()
	}
	defer func() { launchLocalProvider = previousLauncher }()

	if err := applySTTProfile(ctx, cfgPath, cfg, state, sttRouter, profile); err != nil {
		t.Fatalf("applySTTProfile: %v", err)
	}

	if launchErr != nil {
		t.Fatalf("launch context err = %v, want nil", launchErr)
	}
}

func TestSyncConfiguredLocalProviderReusesMatchingProvider(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Local.Enabled = true
	cfg.Local.Port = 8080
	cfg.Local.GPU = "auto"
	cfg.Local.Model = "ggml-large-v3-turbo.bin"
	cfg.Local.ModelPath = filepath.Join(t.TempDir(), "ggml-large-v3-turbo.bin")

	existing := stt.NewLocalProvider(cfg.Local.Port, cfg.Local.ModelPath, cfg.Local.GPU)
	sttRouter := &router.Router{}
	sttRouter.SetLocal(existing)

	launchCalls := 0
	previousLauncher := launchLocalProvider
	launchLocalProvider = func(ctx context.Context, state *appState, r *router.Router, provider localProviderStarter) {
		launchCalls++
	}
	defer func() { launchLocalProvider = previousLauncher }()

	syncConfiguredLocalProvider(context.Background(), cfg, &appState{}, sttRouter)

	if got := sttRouter.Local(); got != existing {
		t.Fatalf("local provider pointer changed, want existing provider to be reused")
	}
	if launchCalls != 1 {
		t.Fatalf("launchLocalProvider calls = %d, want 1 for reused provider", launchCalls)
	}
}

func TestApplySTTProfileLocalRejectsMissingWhisperBinary(t *testing.T) {
	modelsDir := t.TempDir()
	modelPath := filepath.Join(modelsDir, "ggml-small.bin")
	writeValidWhisperModelFile(t, modelPath)

	t.Setenv("LOCALAPPDATA", t.TempDir())

	cfg := defaultTestConfig()
	cfg.Local.Enabled = true
	cfg.Local.Port = 8080
	cfg.Local.Model = "ggml-small.bin"
	cfg.Local.ModelPath = modelPath
	cfg.Routing.Strategy = "local-only"
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

	previousLauncher := launchLocalProvider
	launchLocalProvider = func(ctx context.Context, state *appState, r *router.Router, provider localProviderStarter) {}
	defer func() { launchLocalProvider = previousLauncher }()

	err := applySTTProfile(context.Background(), cfgPath, cfg, state, sttRouter, profile)
	if err == nil {
		t.Fatal("expected error when whisper-server binary is missing")
	}
	if got := err.Error(); got == "" || !strings.Contains(strings.ToLower(got), "whisper-server binary missing") {
		t.Fatalf("error = %q, want whisper-server binary missing", got)
	}
}

func TestApplyRealtimeVoiceProfileClearsPipelineFallbackWhenSelectingGoogle(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Providers.Google.APIKeyEnv = "GOOGLE_AI_API_KEY"
	cfg.VoiceAgent.PipelineFallback = true
	cfg.VoiceAgent.Model = "openai/whisper-large-v3"
	cfg.HuggingFace.AgentModel = "openai/whisper-large-v3"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	t.Setenv("GOOGLE_AI_API_KEY", "test-google-key")

	profile := models.Profile{
		ID:            "voice.google.gemini-live",
		Name:          "Gemini Live",
		Modality:      models.ModalityRealtimeVoice,
		ExecutionMode: models.ExecutionModeGoogle,
		ModelID:       "gemini-2.5-flash-native-audio-preview-12-2025",
	}

	if err := applyRealtimeVoiceProfile(context.Background(), cfgPath, cfg, nil, profile); err != nil {
		t.Fatalf("applyRealtimeVoiceProfile: %v", err)
	}

	if cfg.VoiceAgent.PipelineFallback {
		t.Fatal("expected google realtime voice profile to clear pipeline fallback")
	}
	if got, want := cfg.VoiceAgent.Model, profile.ModelID; got != want {
		t.Fatalf("cfg.VoiceAgent.Model = %q, want %q", got, want)
	}
	if got := cfg.HuggingFace.AgentModel; got != "" {
		t.Fatalf("cfg.HuggingFace.AgentModel = %q, want empty", got)
	}
}

func TestApplyRealtimeVoiceProfileOllamaUsesPipelineFallback(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Providers.Ollama.BaseURL = "http://127.0.0.1:11434"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	state := &appState{activeProfiles: map[string]string{}}
	profile := models.Profile{
		ID:            "realtime.ollama.gemma4-e4b-pipeline",
		Name:          "Gemma 4 E4B Voice Pipeline (Ollama)",
		Modality:      models.ModalityRealtimeVoice,
		ExecutionMode: models.ExecutionModeOllama,
		ModelID:       "gemma4:e4b",
	}

	reloadCalls := 0
	previousReload := reloadAIRuntime
	reloadAIRuntime = func(ctx context.Context, state *appState, cfg *config.Config) error {
		reloadCalls++
		return nil
	}
	defer func() { reloadAIRuntime = previousReload }()

	if err := applyRealtimeVoiceProfile(context.Background(), cfgPath, cfg, state, profile); err != nil {
		t.Fatalf("applyRealtimeVoiceProfile: %v", err)
	}

	if !cfg.Providers.Ollama.Enabled {
		t.Fatal("expected Ollama provider to be enabled")
	}
	if got := cfg.Providers.Ollama.AgentModel; got != "gemma4:e4b" {
		t.Fatalf("ollama agent model = %q, want %q", got, "gemma4:e4b")
	}
	if !cfg.VoiceAgent.PipelineFallback {
		t.Fatal("expected Ollama voice profile to use pipeline fallback")
	}
	if got := state.activeProfiles["realtime_voice"]; got != "realtime.ollama.gemma4-e4b-pipeline" {
		t.Fatalf("active realtime voice profile = %q, want %q", got, "realtime.ollama.gemma4-e4b-pipeline")
	}
	if reloadCalls != 1 {
		t.Fatalf("reloadAIRuntime calls = %d, want 1", reloadCalls)
	}
}

func TestApplyRealtimeVoiceProfileBuiltInLocalUsesPipelineFallback(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.LocalLLM.BaseURL = ""
	cfg.LocalLLM.Port = 0
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	state := &appState{activeProfiles: map[string]string{}}
	profile := models.Profile{
		ID:            "realtime.builtin.pipeline",
		Name:          "SpeechKit Voice Pipeline (Built-in)",
		Modality:      models.ModalityRealtimeVoice,
		ExecutionMode: models.ExecutionModeLocal,
		ModelID:       "speechkit-local-voice-pipeline",
	}

	reloadCalls := 0
	previousReload := reloadAIRuntime
	reloadAIRuntime = func(ctx context.Context, state *appState, cfg *config.Config) error {
		reloadCalls++
		return nil
	}
	defer func() { reloadAIRuntime = previousReload }()

	if err := applyRealtimeVoiceProfile(context.Background(), cfgPath, cfg, state, profile); err != nil {
		t.Fatalf("applyRealtimeVoiceProfile: %v", err)
	}

	if !cfg.LocalLLM.Enabled {
		t.Fatal("expected built-in local LLM to be enabled")
	}
	if got := cfg.LocalLLM.BaseURL; got != config.DefaultLocalLLMBaseURL {
		t.Fatalf("local llm base url = %q, want %q", got, config.DefaultLocalLLMBaseURL)
	}
	if got := cfg.LocalLLM.AgentModel; got != "speechkit-local-voice-pipeline" {
		t.Fatalf("local llm agent model = %q, want %q", got, "speechkit-local-voice-pipeline")
	}
	if !cfg.VoiceAgent.PipelineFallback {
		t.Fatal("expected built-in local voice profile to use pipeline fallback")
	}
	if got := state.activeProfiles["realtime_voice"]; got != "realtime.builtin.pipeline" {
		t.Fatalf("active realtime voice profile = %q, want %q", got, "realtime.builtin.pipeline")
	}
	if reloadCalls != 1 {
		t.Fatalf("reloadAIRuntime calls = %d, want 1", reloadCalls)
	}
}

func TestApplyRealtimeVoiceProfileReloadsAIRuntimeForGoogleProfiles(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Providers.Google.APIKeyEnv = "GOOGLE_AI_API_KEY"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	t.Setenv("GOOGLE_AI_API_KEY", "test-google-key")

	state := &appState{activeProfiles: map[string]string{}}
	profile := models.Profile{
		ID:            "voice.google.gemini-live",
		Name:          "Gemini Live",
		Modality:      models.ModalityRealtimeVoice,
		ExecutionMode: models.ExecutionModeGoogle,
		ModelID:       "gemini-2.5-flash-native-audio-preview-12-2025",
	}

	reloadCalls := 0
	previousReload := reloadAIRuntime
	reloadAIRuntime = func(ctx context.Context, state *appState, cfg *config.Config) error {
		reloadCalls++
		return nil
	}
	defer func() { reloadAIRuntime = previousReload }()

	if err := applyRealtimeVoiceProfile(context.Background(), cfgPath, cfg, state, profile); err != nil {
		t.Fatalf("applyRealtimeVoiceProfile: %v", err)
	}

	if reloadCalls != 1 {
		t.Fatalf("reloadAIRuntime calls = %d, want 1", reloadCalls)
	}
}
