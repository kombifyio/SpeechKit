package generation

import (
	"context"
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
	for _, generator := range c.generators {
		if explicitProvider != "" {
			catalog, err := generator.Models(ctx, ModelQuery{Purpose: request.Purpose})
			if err != nil {
				lastErr = err
				continue
			}
			if !catalogHasProvider(catalog, explicitProvider) {
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

func catalogHasProvider(catalog Catalog, provider string) bool {
	for _, model := range catalog.Models {
		if model.Provider == provider {
			return true
		}
	}
	return false
}
