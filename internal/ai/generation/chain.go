package generation

import (
	"context"
	"log/slog"
	"strings"
)

type Chain struct {
	generators []Generator
}

func NewChain(generators ...Generator) *Chain {
	filtered := make([]Generator, 0, len(generators))
	for _, generator := range generators {
		if generator != nil {
			filtered = append(filtered, generator)
		}
	}
	return &Chain{generators: filtered}
}

func (c *Chain) Models(ctx context.Context, query ModelQuery) (Catalog, error) {
	var catalog Catalog
	var lastErr error
	for _, generator := range c.generators {
		current, err := generator.Models(ctx, query)
		if err != nil {
			lastErr = err
			continue
		}
		catalog.Models = append(catalog.Models, current.Models...)
	}
	if len(catalog.Models) == 0 && lastErr != nil {
		return Catalog{}, lastErr
	}
	return catalog, nil
}

func (c *Chain) Generate(ctx context.Context, request Request) (Result, error) {
	var lastErr error
	explicitProvider := providerFromModelID(request.ModelID)
	for index, generator := range c.generators {
		if explicitProvider != "" {
			serves, err := generatorServes(ctx, generator, request.Purpose, explicitProvider)
			if err != nil {
				lastErr = err
				continue
			}
			if !serves {
				continue
			}
		}
		result, err := generator.Generate(ctx, request)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if explicitProvider != "" {
			return Result{}, err
		}
		if ctx.Err() != nil {
			return Result{}, err
		}
		if index < len(c.generators)-1 && !isResting(err) {
			// The caller only sees the last error; name the one the fallback
			// hides. A Copilot account over its quota went unnoticed for days
			// because every meeting summary still arrived from the local model.
			// A resting provider logged its pause once already.
			slog.Warn("generation.fallback", "purpose", string(request.Purpose), "kind", string(Kind(err)), "err", err)
		}
	}
	if lastErr == nil {
		lastErr = &Error{Kind: ErrorConfiguration, Operation: "generate"}
	}
	return Result{}, lastErr
}

func providerFromModelID(modelID string) string {
	provider, _, found := strings.Cut(modelID, "/")
	if !found {
		return ""
	}
	return provider
}

// generatorServes reports whether a generator can serve the given provider.
// A generator that names its single provider answers without listing models:
// listing Copilot's starts its CLI, which a request pinned to the local model
// has no reason to wait for.
func generatorServes(ctx context.Context, generator Generator, purpose Purpose, provider string) (bool, error) {
	if named, ok := generator.(ProviderIdentifier); ok {
		if id := named.ProviderID(); id != "" {
			return id == provider, nil
		}
	}
	catalog, err := generator.Models(ctx, ModelQuery{Purpose: purpose})
	if err != nil {
		return false, err
	}
	return catalogHasProvider(catalog, provider), nil
}

func catalogHasProvider(catalog Catalog, provider string) bool {
	for _, model := range catalog.Models {
		if model.Provider == provider {
			return true
		}
	}
	return false
}
