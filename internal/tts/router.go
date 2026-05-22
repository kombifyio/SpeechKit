// Package tts implements the SpeechKit text-to-speech surface: a small
// provider interface plus concrete adapters for OpenAI, Google, and
// Hugging Face. The router in this file picks a provider per request
// based on Strategy (CloudFirst, LocalFirst) and degrades gracefully
// when a provider is unavailable.
//
// All providers must go through [github.com/kombifyio/SpeechKit/internal/netsec]
// for outbound HTTP. STT has a parallel routing layer in package router;
// this package is TTS-only.
package tts

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// Strategy determines how the TTS router selects a provider.
type Strategy string

const (
	StrategyCloudFirst Strategy = "cloud-first" // Default: try cloud providers first
	StrategyLocalFirst Strategy = "local-first" // Try local (Kokoro) first, cloud fallback
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
			slog.Warn("TTS router: provider failed", "provider", p.Name(), "err", err)
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

// OrderByPreferredProvider returns providers reordered so that the one whose
// Provider.Name() matches `preferred` comes first; the remaining providers
// retain their original relative order. Used by the bootstrap layer to honour
// the [model_selection.tts] PrimaryProfileID without rewriting the
// strategy/fallback logic. Empty preferred or no-match returns the input
// slice unchanged.
func OrderByPreferredProvider(providers []Provider, preferred string) []Provider {
	if preferred == "" || len(providers) <= 1 {
		return providers
	}
	out := make([]Provider, 0, len(providers))
	var pinned Provider
	for _, p := range providers {
		if p != nil && p.Name() == preferred {
			pinned = p
			continue
		}
		out = append(out, p)
	}
	if pinned == nil {
		return providers
	}
	return append([]Provider{pinned}, out...)
}

// PreferredProviderForProfileID maps a Voice-Output profile ID (e.g.
// "tts.google.studio-o-de") to the Provider.Name() value the TTS router
// uses internally. Returns the empty string when the profile is unknown
// or unset — callers should treat that as "no preference, use default
// strategy ordering".
//
// Mapping (kept in sync with pkg/speechkit/catalog.go TTS entries):
//
//	tts.openai.*                     → "openai"
//	tts.google.*                     → "google"
//	tts.huggingface.*                → "huggingface"
//	tts.openedai.*                   → "kokoro"           (OpenAI-compatible self-hosted endpoint)
//	tts.local.kokoro-*               → "kokoro_local"     (v0.37.3 ONNX in-process, Phase-3 runtime)
//	tts.local.supertonic-*           → "supertonic_local" (v0.37.3 ONNX in-process, multilingual)
//	tts.local.chatterbox-*           → "chatterbox_local" (v0.37.3 ONNX in-process, voice-clone)
//	tts.local.piper / tts.local.piper-* → "piper"          (HA-compatible Piper subprocess)
func PreferredProviderForProfileID(profileID string) string {
	id := strings.TrimSpace(profileID)
	if id == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(id, "tts.openai."):
		return "openai"
	case strings.HasPrefix(id, "tts.google."):
		return "google"
	case strings.HasPrefix(id, "tts.huggingface."):
		return "huggingface"
	case strings.HasPrefix(id, "tts.openedai."), strings.HasPrefix(id, "tts.kokoro."):
		return "kokoro"
	case strings.HasPrefix(id, "tts.local.kokoro"):
		return "kokoro_local"
	case strings.HasPrefix(id, "tts.local.supertonic"):
		return "supertonic_local"
	case strings.HasPrefix(id, "tts.local.chatterbox"):
		return "chatterbox_local"
	case id == "tts.local.piper", strings.HasPrefix(id, "tts.local.piper-"):
		return "piper"
	default:
		return ""
	}
}
