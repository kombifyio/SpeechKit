// Package allproviders is the batteries-included realtime Voice Agent
// assembly layer: it knows every provider SpeechKit ships and resolves a
// provider id, alias or profile id to a live provider.
//
// Importing it compiles every realtime provider, which is the right trade for
// a host that offers a provider choice at runtime. A host that speaks exactly
// one realtime protocol imports that provider's own package instead and stays
// free of the others' dependencies.

package allproviders

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/assemblyai"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/deepgram"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/gemini"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/openai"
)

var ErrUnknownLiveProvider = errors.New("speechkit live: unknown provider")

// ProviderFactory constructs a fresh live.LiveProvider instance.
type ProviderFactory func() live.LiveProvider

// ProviderFactoryRegistry lets embedders override or extend provider
// construction while still using SpeechKit's provider/profile normalization.
type ProviderFactoryRegistry map[string]ProviderFactory

// DefaultProviderFactories returns factories for the built-in native realtime
// providers. The returned map is a copy and can be safely modified by callers.
func DefaultProviderFactories() ProviderFactoryRegistry {
	return ProviderFactoryRegistry{
		"google":     func() live.LiveProvider { return gemini.New() },
		"deepgram":   func() live.LiveProvider { return deepgram.New() },
		"assemblyai": func() live.LiveProvider { return assemblyai.New() },
		"openai":     func() live.LiveProvider { return openai.New() },
	}
}

// NewProvider returns a fresh built-in live.LiveProvider for a provider id, alias,
// or public realtime profile id.
func NewProvider(providerOrProfile string) (live.LiveProvider, error) {
	return NewProviderWithFactories(providerOrProfile, nil)
}

// NewProviderWithFactories resolves provider aliases/profile ids against a
// custom factory registry. Missing registry entries fall back to the built-in
// providers, so callers can override only the providers they own.
func NewProviderWithFactories(providerOrProfile string, factories ProviderFactoryRegistry) (live.LiveProvider, error) {
	provider := live.NormalizeProviderID(providerOrProfile)
	if strings.TrimSpace(provider) == "" {
		return nil, fmt.Errorf("%w: provider is empty", ErrUnknownLiveProvider)
	}
	merged := DefaultProviderFactories()
	for key, factory := range factories {
		merged[live.NormalizeProviderID(key)] = factory
	}
	factory := merged[provider]
	if factory == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownLiveProvider, providerOrProfile)
	}
	instance := factory()
	if instance == nil {
		return nil, fmt.Errorf("speechkit live: factory for %s returned nil", provider)
	}
	return instance, nil
}

// NormalizeLiveConfig fills Provider/ProfileID/Model from the provider
// descriptor catalog without touching credentials, prompts, tools, or policies.
// It accepts Provider, ProfileID, or a model id that is unique in the descriptor
// catalog. Existing non-empty fields remain stronger than descriptor defaults.
func NormalizeLiveConfig(cfg live.LiveConfig) (live.LiveConfig, error) {
	selector := firstLiveConfigSelector(cfg)
	descriptor, ok := live.FindProviderDescriptor(selector)
	if !ok && strings.TrimSpace(cfg.Model) != "" {
		descriptor, ok = findDescriptorByModel(cfg.Model)
	}
	if !ok {
		return cfg, fmt.Errorf("%w: %s", ErrUnknownLiveProvider, selector)
	}
	cfg.Provider = descriptor.Provider
	if strings.TrimSpace(cfg.ProfileID) == "" {
		cfg.ProfileID = descriptor.ProfileID
	}
	if strings.TrimSpace(cfg.Model) == "" {
		if model, ok := descriptor.DefaultModel(); ok {
			cfg.Model = model.ModelID
		}
	}
	return cfg, nil
}

// NewProviderForConfig normalizes a live.LiveConfig and returns the matching
// provider. It is the most convenient entrypoint for embedders that expose a
// provider/profile/model picker to users.
func NewProviderForConfig(cfg live.LiveConfig) (live.LiveProvider, live.LiveConfig, error) {
	normalized, err := NormalizeLiveConfig(cfg)
	if err != nil {
		return nil, cfg, err
	}
	provider, err := NewProvider(normalized.Provider)
	if err != nil {
		return nil, normalized, err
	}
	return provider, normalized, nil
}

func firstLiveConfigSelector(cfg live.LiveConfig) string {
	if strings.TrimSpace(cfg.Provider) != "" {
		return cfg.Provider
	}
	if strings.TrimSpace(cfg.ProfileID) != "" {
		return cfg.ProfileID
	}
	return cfg.Model
}

func findDescriptorByModel(modelID string) (live.ProviderDescriptor, bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return live.ProviderDescriptor{}, false
	}
	for _, descriptor := range live.DefaultProviderDescriptors() {
		for _, model := range descriptor.Models {
			if strings.EqualFold(model.ModelID, modelID) {
				return descriptor, true
			}
		}
	}
	return live.ProviderDescriptor{}, false
}
