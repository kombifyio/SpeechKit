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
	"sync"

	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/ttsroute"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ttsTracer instruments TTS synthesis. A no-op tracer is used when no
// OpenTelemetry provider is installed, so spans cost nothing.
var ttsTracer = otel.Tracer("github.com/kombifyio/SpeechKit/internal/tts")

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
func (r *Router) Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (res *Result, err error) {
	ctx, span := ttsTracer.Start(ctx, "tts.synthesize", trace.WithAttributes(
		attribute.String("speechkit.tts.strategy", string(r.strategy)),
		attribute.String("speechkit.tts.format", opts.Format),
	))
	defer func() {
		switch {
		case err != nil:
			span.RecordError(err)
			span.SetStatus(codes.Error, "synthesize failed")
		case res != nil:
			span.SetAttributes(attribute.String("speechkit.tts.provider", res.Provider))
		}
		span.End()
	}()

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

		result, err := p.Synthesize(ctx, text, opts.ForProvider(p.Name()))
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
	isLocal := isLocalProviderKind(p.Kind())

	switch r.strategy {
	case StrategyCloudOnly:
		return !isLocal
	case StrategyLocalOnly:
		return isLocal
	default:
		return true
	}
}

func isLocalProviderKind(kind models.ProviderKind) bool {
	return kind == models.ProviderKindLocalBuiltIn || kind == models.ProviderKindLocalProvider
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

// CloseIdleConnections asks HTTP-backed providers to drop idle connection
// pools. It is safe to call when a router is no longer referenced by any
// active mode runtime.
func (r *Router) CloseIdleConnections() {
	if r == nil {
		return
	}
	r.mu.RLock()
	providers := make([]Provider, len(r.providers))
	copy(providers, r.providers)
	r.mu.RUnlock()

	for _, p := range providers {
		if closer, ok := p.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
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
// The mapping itself lives in the zero-dependency ttsroute leaf package so the
// kernel and the public pkg/speechkit/tts surface share one source of truth.
func PreferredProviderForProfileID(profileID string) string {
	return ttsroute.PreferredProvider(profileID)
}
