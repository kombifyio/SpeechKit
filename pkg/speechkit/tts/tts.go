// Package tts exposes the embeddable SpeechKit text-to-speech surface.
package tts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/ttsroute"
)

var ErrMissingRouter = errors.New("speechkit tts: router is required")

// Provider defines the interface for text-to-speech backends.
type Provider interface {
	Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (*Result, error)
	Name() string
	Kind() ProviderKind
	Health(ctx context.Context) error
}

// ProviderKind identifies whether a provider is local, cloud-routed, or a
// direct external API. Router strategies use this instead of provider names.
type ProviderKind string

const (
	ProviderKindLocalBuiltIn   ProviderKind = "local_built_in"
	ProviderKindLocalProvider  ProviderKind = "local_provider"
	ProviderKindCloudProvider  ProviderKind = "cloud_provider"
	ProviderKindDirectProvider ProviderKind = "direct_provider"
)

// SynthesizeOpts configures a single TTS request.
type SynthesizeOpts struct {
	Locale          string
	Voice           string
	Speed           float64
	Format          string
	Options         provideropts.Values
	ProviderOptions provideropts.Values
}

// Result holds the output of a TTS synthesis.
type Result struct {
	Audio      []byte
	Format     string
	SampleRate int
	Duration   time.Duration
	Provider   string
	Voice      string
}

// Strategy determines how Router selects providers.
type Strategy string

const (
	StrategyCloudFirst Strategy = "cloud-first"
	StrategyLocalFirst Strategy = "local-first"
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
func NewRouter(strategy Strategy, providers ...Provider) *Router {
	if strategy == "" {
		strategy = StrategyCloudFirst
	}
	return &Router{providers: providers, strategy: strategy}
}

// SetProviders replaces the provider list.
func (r *Router) SetProviders(providers ...Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = providers
}

// Synthesize tries each eligible provider until one succeeds.
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

func isLocalProviderKind(kind ProviderKind) bool {
	return kind == ProviderKindLocalBuiltIn || kind == ProviderKindLocalProvider
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

// CloseIdleConnections asks HTTP-backed providers to drop idle connection pools.
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

// OrderByPreferredProvider returns providers with the matching provider first.
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

// PreferredProviderForProfileID maps a Voice-Output profile ID to Provider.Name.
// The mapping is shared with the kernel via the ttsroute leaf package.
func PreferredProviderForProfileID(profileID string) string {
	return ttsroute.PreferredProvider(profileID)
}

// Service is a small stable facade over Router. It gives embedders one
// construction point while still letting them provide their own providers.
type Service struct {
	router      *Router
	defaultOpts SynthesizeOpts
}

type ServiceOption func(*Service)

func WithDefaultOpts(opts SynthesizeOpts) ServiceOption {
	return func(s *Service) {
		s.defaultOpts = opts
	}
}

func NewService(router *Router, opts ...ServiceOption) (*Service, error) {
	if router == nil {
		return nil, ErrMissingRouter
	}
	service := &Service{router: router}
	for _, opt := range opts {
		if opt != nil {
			opt(service)
		}
	}
	return service, nil
}

func (s *Service) Synthesize(ctx context.Context, text string, opts ...SynthesizeOpts) (*Result, error) {
	if s == nil || s.router == nil {
		return nil, ErrMissingRouter
	}
	requestOpts := s.defaultOpts
	if len(opts) > 0 {
		requestOpts = mergeOpts(requestOpts, opts[0])
	}
	return s.router.Synthesize(ctx, text, requestOpts)
}

func (s *Service) Router() *Router {
	if s == nil {
		return nil
	}
	return s.router
}

func mergeOpts(base, override SynthesizeOpts) SynthesizeOpts {
	if override.Locale != "" {
		base.Locale = override.Locale
	}
	if override.Voice != "" {
		base.Voice = override.Voice
	}
	if override.Speed != 0 {
		base.Speed = override.Speed
	}
	if override.Format != "" {
		base.Format = override.Format
	}
	if len(override.Options) > 0 {
		base.Options = base.Options.Clone()
		base.Options = base.Options.Merge(override.Options)
	}
	if len(override.ProviderOptions) > 0 {
		base.ProviderOptions = base.ProviderOptions.Clone()
		base.ProviderOptions = base.ProviderOptions.Merge(override.ProviderOptions)
	}
	return base
}
