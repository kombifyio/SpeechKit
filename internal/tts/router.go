package tts

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// Strategy determines how the TTS router selects a provider.
type Strategy string

const (
	StrategyCloudFirst Strategy = "cloud-first" // Default: try cloud providers first
	StrategyLocalFirst Strategy = "local-first"  // Try local (Kokoro) first, cloud fallback
	StrategyCloudOnly  Strategy = "cloud-only"
	StrategyLocalOnly  Strategy = "local-only"
)

// Router selects and falls back between TTS providers.
type Router struct {
	mu        sync.RWMutex
	providers []Provider
	strategy  Strategy
}

// NewRouter creates a TTS router with the given strategy and providers.
// Providers are tried in order according to the strategy.
func NewRouter(strategy Strategy, providers ...Provider) *Router {
	if strategy == "" {
		strategy = StrategyCloudFirst
	}
	return &Router{
		providers: providers,
		strategy:  strategy,
	}
}

// SetProviders replaces the provider list (thread-safe for runtime reconfiguration).
func (r *Router) SetProviders(providers ...Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = providers
}

// Synthesize tries each provider in order until one succeeds.
func (r *Router) Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (*Result, error) {
	r.mu.RLock()
	providers := make([]Provider, len(r.providers))
	copy(providers, r.providers)
	r.mu.RUnlock()

	if len(providers) == 0 {
		return nil, fmt.Errorf("tts router: no providers configured")
	}

	var lastErr error
	for _, p := range providers {
		if !r.isAllowed(p) {
			continue
		}

		result, err := p.Synthesize(ctx, text, opts)
		if err != nil {
			lastErr = err
			log.Printf("TTS router: provider %s failed: %v", p.Name(), err)
			continue
		}
		return result, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("tts router: all providers failed, last error: %w", lastErr)
	}
	return nil, fmt.Errorf("tts router: no eligible providers for strategy %s", r.strategy)
}

// isAllowed checks if a provider is allowed under the current strategy.
func (r *Router) isAllowed(p Provider) bool {
	name := p.Name()
	isLocal := name == "kokoro" || name == "local"

	switch r.strategy {
	case StrategyCloudOnly:
		return !isLocal
	case StrategyLocalOnly:
		return isLocal
	default:
		return true
	}
}

// HealthCheck returns health status for all providers.
func (r *Router) HealthCheck(ctx context.Context) map[string]error {
	r.mu.RLock()
	providers := make([]Provider, len(r.providers))
	copy(providers, r.providers)
	r.mu.RUnlock()

	results := make(map[string]error, len(providers))
	for _, p := range providers {
		results[p.Name()] = p.Health(ctx)
	}
	return results
}
