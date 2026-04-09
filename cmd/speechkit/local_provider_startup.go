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

var localProviderRetryDelay = 2 * time.Second

type localProviderStarter interface {
	stt.STTProvider
	StartServer(context.Context) error
	IsReady() bool
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
		syncRuntimeProviders(state, r)
		return
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt == 1 {
			runtimeLog(state, "Starting local STT...", "info")
		} else {
			runtimeLog(state, fmt.Sprintf("Retrying local STT startup (%d/%d)...", attempt, maxAttempts), "warn")
		}

		if err := provider.StartServer(ctx); err == nil {
			runtimeLog(state, "Local STT ready", "success")
			syncRuntimeProviders(state, r)
			return
		} else {
			runtimeLog(state, fmt.Sprintf("Local STT startup attempt %d/%d failed: %v", attempt, maxAttempts, err), "warn")
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

	syncRuntimeProviders(state, r)
	runtimeLog(state, "Local STT unavailable after retries", "error")
}

func runtimeLog(state *appState, message, logType string) {
	if state != nil {
		state.addLog(message, logType)
		return
	}
	slog.Info(message, "type", logType)
}
