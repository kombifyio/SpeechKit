package live

import (
	"errors"
	"fmt"
	"strings"
)

var ErrUnknownLiveProvider = errors.New("speechkit live: unknown provider")

// ProviderFactory constructs a fresh LiveProvider instance.
type ProviderFactory func() LiveProvider

// ProviderFactoryRegistry lets embedders override or extend provider
// construction while still using SpeechKit's provider/profile normalization.
type ProviderFactoryRegistry map[string]ProviderFactory

// DefaultProviderFactories returns factories for the built-in native realtime
// providers. The returned map is a copy and can be safely modified by callers.
func DefaultProviderFactories() ProviderFactoryRegistry {
	return ProviderFactoryRegistry{
		"google":     func() LiveProvider { return NewGeminiLive() },
		"deepgram":   func() LiveProvider { return NewDeepgramLive() },
		"assemblyai": func() LiveProvider { return NewAssemblyAILive() },
		"openai":     func() LiveProvider { return NewOpenAILive() },
	}
}

// NewProvider returns a fresh built-in LiveProvider for a provider id, alias,
// or public realtime profile id.
func NewProvider(providerOrProfile string) (LiveProvider, error) {
	return NewProviderWithFactories(providerOrProfile, nil)
}

// NewProviderWithFactories resolves provider aliases/profile ids against a
// custom factory registry. Missing registry entries fall back to the built-in
// providers, so callers can override only the providers they own.
func NewProviderWithFactories(providerOrProfile string, factories ProviderFactoryRegistry) (LiveProvider, error) {
	provider := NormalizeProviderID(providerOrProfile)
	if strings.TrimSpace(provider) == "" {
		return nil, fmt.Errorf("%w: provider is empty", ErrUnknownLiveProvider)
	}
	merged := DefaultProviderFactories()
	for key, factory := range factories {
		merged[NormalizeProviderID(key)] = factory
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
func NormalizeLiveConfig(cfg LiveConfig) (LiveConfig, error) {
	selector := firstLiveConfigSelector(cfg)
	descriptor, ok := FindProviderDescriptor(selector)
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

// NewProviderForConfig normalizes a LiveConfig and returns the matching
// provider. It is the most convenient entrypoint for embedders that expose a
// provider/profile/model picker to users.
func NewProviderForConfig(cfg LiveConfig) (LiveProvider, LiveConfig, error) {
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

func firstLiveConfigSelector(cfg LiveConfig) string {
	if strings.TrimSpace(cfg.Provider) != "" {
		return cfg.Provider
	}
	if strings.TrimSpace(cfg.ProfileID) != "" {
		return cfg.ProfileID
	}
	return cfg.Model
}

func findDescriptorByModel(modelID string) (ProviderDescriptor, bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ProviderDescriptor{}, false
	}
	for _, descriptor := range DefaultProviderDescriptors() {
		for _, model := range descriptor.Models {
			if strings.EqualFold(model.ModelID, modelID) {
				return descriptor, true
			}
		}
	}
	return ProviderDescriptor{}, false
}
