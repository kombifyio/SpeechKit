package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

const (
	localProviderStartRetries = 4
)

var localProviderRetryDelay = 5 * time.Second

type localProviderStarter interface {
	stt.STTProvider
	StartServer(context.Context) error
	IsReady() bool
	VerifyInstallation() stt.InstallStatus
}

func startLocalProviderAsync(ctx context.Context, state *appState, r *router.Router, provider localProviderStarter) {
	if provider == nil {
		return
	}
	go startLocalProviderWithRetry(ctx, state, r, provider, localProviderStartRetries)
}

func startLocalProviderWithRetry(ctx context.Context, state *appState, r *router.Router, provider localProviderStarter, maxAttempts int) {
	if provider == nil || maxAttempts <= 0 {
		return
	}

	if provider.IsReady() {
		syncRuntimeProviders(ctx, state, r)
		return
	}

	// Pre-flight: verify binary and model before attempting startup.
	status := provider.VerifyInstallation()
	if len(status.Problems) > 0 {
		for _, problem := range status.Problems {
			runtimeLog(state, fmt.Sprintf("Local STT: %s", problem), "warn")
		}
		if !status.BinaryFound {
			runtimeLog(state, "Local STT unavailable: whisper-server binary missing. Re-install SpeechKit or download the model from Settings.", "error")
			syncRuntimeProviders(ctx, state, r)
			return
		}
		if !status.ModelFound {
			runtimeLog(state, "Local STT unavailable: model file missing or corrupt. Download a model from Settings â†’ STT.", "error")
			syncRuntimeProviders(ctx, state, r)
			return
		}
	} else {
		runtimeLog(state, fmt.Sprintf("Local STT verified: binary=%s model=%s (%d MB)", status.BinaryPath, status.ModelPath, status.ModelBytes/1_000_000), "info")
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt == 1 {
			runtimeLog(state, "Waiting for local STT startup...", "info")
		} else {
			runtimeLog(state, fmt.Sprintf("Retrying local STT startup (%d/%d)...", attempt, maxAttempts), "warn")
		}

		if err := provider.StartServer(ctx); err != nil {
			runtimeLog(state, fmt.Sprintf("Local STT startup attempt %d/%d failed: %v", attempt, maxAttempts, err), "warn")
		} else {
			runtimeLog(state, "Local STT ready", "success")
			syncRuntimeProviders(ctx, state, r)
			return
		}

		if attempt == maxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(localProviderRetryDelay):
		}
	}

	syncRuntimeProviders(ctx, state, r)
	runtimeLog(state, "Local STT unavailable after retries", "error")
}

func runtimeLog(state *appState, message, logType string) {
	if state != nil {
		state.addLog(message, logType)
		return
	}
	slog.Info(message, "type", logType)
}
