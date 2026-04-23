package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/downloads"
	"github.com/kombifyio/SpeechKit/internal/localllm"
)

const localLLMRuntimeStartRetries = 4

var localLLMRuntimeRetryDelay = 5 * time.Second

var (
	localLLMRuntimeStartMu sync.Mutex
	localLLMRuntimeStarts  = map[string]struct{}{}
)

type localLLMRuntimeStarter interface {
	StartServer(context.Context) error
	StopServer()
	IsReady() bool
	VerifyInstallation() localllm.InstallStatus
	RuntimeKey() string
}

var newLocalLLMRuntime = func(port int, modelPath, gpu string) localLLMRuntimeStarter {
	return localllm.NewServer(port, modelPath, gpu)
}

var launchLocalLLMRuntime = startLocalLLMRuntimeAsync

func syncConfiguredLocalLLMRuntime(ctx context.Context, cfg *config.Config, state *appState) {
	if state == nil || cfg == nil {
		return
	}

	modelPath := configuredLocalLLMModelPath(cfg)
	if !cfg.LocalLLM.Enabled || strings.TrimSpace(modelPath) == "" {
		stopConfiguredLocalLLMRuntime(state)
		return
	}

	port := cfg.LocalLLM.Port
	if port == 0 {
		port = 8082
	}
	runtime := newLocalLLMRuntime(port, modelPath, cfg.LocalLLM.GPU)
	key := runtime.RuntimeKey()

	state.mu.Lock()
	existing := state.localLLMRuntime
	if existing != nil && existing.RuntimeKey() == key {
		state.mu.Unlock()
		launchLocalLLMRuntime(context.WithoutCancel(ctx), state, existing)
		return
	}
	state.localLLMRuntime = runtime
	state.mu.Unlock()

	if existing != nil {
		existing.StopServer()
	}

	launchLocalLLMRuntime(context.WithoutCancel(ctx), state, runtime)
}

func stopConfiguredLocalLLMRuntime(state *appState) {
	if state == nil {
		return
	}
	state.mu.Lock()
	runtime := state.localLLMRuntime
	state.localLLMRuntime = nil
	state.mu.Unlock()
	if runtime != nil {
		runtime.StopServer()
	}
}

func startLocalLLMRuntimeAsync(ctx context.Context, state *appState, runtime localLLMRuntimeStarter) {
	if runtime == nil {
		return
	}
	key := runtime.RuntimeKey()
	if key == "" {
		return
	}

	localLLMRuntimeStartMu.Lock()
	if _, exists := localLLMRuntimeStarts[key]; exists {
		localLLMRuntimeStartMu.Unlock()
		return
	}
	localLLMRuntimeStarts[key] = struct{}{}
	localLLMRuntimeStartMu.Unlock()

	go func() {
		defer func() {
			localLLMRuntimeStartMu.Lock()
			delete(localLLMRuntimeStarts, key)
			localLLMRuntimeStartMu.Unlock()
		}()
		startLocalLLMRuntimeWithRetry(ctx, state, runtime, localLLMRuntimeStartRetries)
	}()
}

func startLocalLLMRuntimeWithRetry(ctx context.Context, state *appState, runtime localLLMRuntimeStarter, maxAttempts int) {
	if runtime == nil || maxAttempts <= 0 {
		return
	}
	if runtime.IsReady() {
		return
	}

	status := runtime.VerifyInstallation()
	if len(status.Problems) > 0 {
		for _, problem := range status.Problems {
			runtimeLog(state, fmt.Sprintf("Local LLM: %s", problem), "warn")
		}
		if !status.BinaryFound {
			runtimeLog(state, "Local LLM unavailable: llama-server binary missing. Re-install SpeechKit to restore the bundled local server.", "error")
			return
		}
		if !status.ModelFound {
			runtimeLog(state, "Local LLM unavailable: GGUF model file missing. Download and select a model from Settings.", "error")
			return
		}
	} else {
		runtimeLog(state, fmt.Sprintf("Local LLM verified: binary=%s model=%s (%d MB)", status.BinaryPath, status.ModelPath, status.ModelBytes/1_000_000), "info")
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt == 1 {
			runtimeLog(state, "Waiting for local LLM startup...", "info")
		} else {
			runtimeLog(state, fmt.Sprintf("Retrying local LLM startup (%d/%d)...", attempt, maxAttempts), "warn")
		}

		if err := runtime.StartServer(ctx); err != nil {
			runtimeLog(state, fmt.Sprintf("Local LLM startup attempt %d/%d failed: %v", attempt, maxAttempts, err), "warn")
		} else {
			runtimeLog(state, "Local LLM ready", "success")
			return
		}

		if attempt == maxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(localLLMRuntimeRetryDelay):
		}
	}

	runtimeLog(state, "Local LLM unavailable after retries", "error")
}

func configuredLocalLLMModelPath(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if modelPath := strings.TrimSpace(cfg.LocalLLM.ModelPath); modelPath != "" {
		return modelPath
	}
	for _, model := range []string{
		cfg.LocalLLM.Model,
		cfg.LocalLLM.AssistModel,
		cfg.LocalLLM.AgentModel,
		cfg.LocalLLM.UtilityModel,
	} {
		model = strings.TrimSpace(model)
		if model == "" || strings.Contains(model, ":") || !strings.EqualFold(filepath.Ext(model), ".gguf") {
			continue
		}
		if filepath.IsAbs(model) {
			return filepath.Clean(model)
		}
		return filepath.Join(downloads.ResolveLocalLLMModelsDir(cfg), filepath.Base(model))
	}
	return ""
}
