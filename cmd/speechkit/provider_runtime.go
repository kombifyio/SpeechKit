package main

import (
	"context"
	"time"

	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

const providerHealthTimeout = 400 * time.Millisecond

type readinessProvider interface {
	IsReady() bool
}

// runtimeAvailableProviders returns providers that are configured and currently usable.
// Cloud providers are considered available when configured; local is only listed once ready.
func runtimeAvailableProviders(ctx context.Context, r *router.Router) []string {
	if r == nil {
		return nil
	}

	configured := r.AvailableProviders()
	if len(configured) == 0 {
		return nil
	}

	localReady := false
	if local := r.Local(); local != nil {
		localReady = providerReady(ctx, local)
	}

	providers := make([]string, 0, len(configured))
	for _, name := range configured {
		if name == "local" {
			if localReady {
				providers = append(providers, name)
			}
			continue
		}
		providers = append(providers, name)
	}
	return providers
}

func syncRuntimeProviders(ctx context.Context, state *appState, r *router.Router) {
	providers := runtimeAvailableProviders(ctx, r)
	if state == nil {
		return
	}

	state.mu.Lock()
	state.providers = append([]string(nil), providers...)
	state.syncSpeechKitSnapshotLocked()
	state.mu.Unlock()
}

func providerReady(ctx context.Context, provider stt.STTProvider) bool {
	if provider == nil {
		return false
	}
	if readyProvider, ok := provider.(readinessProvider); ok {
		return readyProvider.IsReady()
	}
	ctx, cancel := context.WithTimeout(ctx, providerHealthTimeout)
	defer cancel()
	return provider.Health(ctx) == nil
}
