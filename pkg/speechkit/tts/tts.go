// Package tts exposes the embeddable SpeechKit text-to-speech surface.
package tts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/ttsroute"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var ErrMissingRouter = errors.New("speechkit tts: router is required")

// ttsTracer instruments TTS synthesis. A no-op tracer is used when no
// OpenTelemetry provider is installed, so spans cost nothing.
var ttsTracer = otel.Tracer("github.com/kombifyio/SpeechKit/pkg/speechkit/tts")

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
	// ProviderOptionsByProvider holds provider-keyed overrides that the Router
	// applies to the selected provider via ForProvider, so one request can
	// carry per-provider tuning without the caller knowing which provider wins.
	ProviderOptionsByProvider map[string]provideropts.Values
}

// ForProvider returns a copy of opts with ProviderOptions merged from the
// provider-keyed overrides for the named provider. The Router calls this for
// the selected provider so per-provider tuning takes effect transparently.
func (o SynthesizeOpts) ForProvider(provider string) SynthesizeOpts {
	provider = normalizeProviderKey(provider)
	if len(o.ProviderOptionsByProvider) == 0 {
		o.ProviderOptions = o.ProviderOptions.Clone()
		return o
	}
	values := o.ProviderOptions.Clone()
	values = values.Merge(o.ProviderOptionsByProvider[provider])
	o.ProviderOptions = values
	return o
}

func normalizeProviderKey(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
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

// Synthesize tries each eligible provider until one succeeds. Per-provider
// option overrides (SynthesizeOpts.ProviderOptionsByProvider) are applied to
// the selected provider, and an OpenTelemetry span records strategy/provider.
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
		result, synthErr := p.Synthesize(ctx, text, opts.ForProvider(p.Name()))
		if synthErr != nil {
			lastErr = synthErr
			slog.Warn("TTS router: provider failed", "provider", p.Name(), "err", synthErr)
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

// ReadyHealthCheck reports only providers eligible under the active routing
// strategy. A local-only router with cloud providers configured must not be
// advertised as ready when Synthesize would skip every one of them.
func (r *Router) ReadyHealthCheck(ctx context.Context) map[string]error {
	r.mu.RLock()
	providers := make([]Provider, len(r.providers))
	copy(providers, r.providers)
	r.mu.RUnlock()

	results := make(map[string]error, len(providers))
	for _, provider := range providers {
		if provider == nil || !r.isAllowed(provider) {
			continue
		}
		results[provider.Name()] = provider.Health(ctx)
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
	if len(override.ProviderOptionsByProvider) > 0 {
		merged := make(map[string]provideropts.Values, len(base.ProviderOptionsByProvider)+len(override.ProviderOptionsByProvider))
		for k, v := range base.ProviderOptionsByProvider {
			merged[k] = v
		}
		for k, v := range override.ProviderOptionsByProvider {
			merged[k] = v
		}
		base.ProviderOptionsByProvider = merged
	}
	return base
}
