package generation

import (
	"context"
	"errors"
)

// Granted wraps a generator that may only run for purposes the user allowed.
// A request for any other purpose fails closed with ErrorConsent before the
// provider is contacted. Models still lists the provider, so a caller names
// the missing permission instead of reporting no model at all.
type Granted struct {
	inner    Generator
	provider string
	allowed  func(Purpose) bool
}

// NewGranted gates inner behind allowed. provider names the provider in the
// consent error and for chain routing.
func NewGranted(inner Generator, provider string, allowed func(Purpose) bool) *Granted {
	return &Granted{inner: inner, provider: provider, allowed: allowed}
}

func (g *Granted) Generate(ctx context.Context, request Request) (Result, error) {
	if g.allowed == nil || !g.allowed(request.Purpose) {
		return Result{}, &Error{
			Kind:      ErrorConsent,
			Operation: "generate",
			Provider:  g.provider,
			Err:       errors.New("processing this content with the provider is not permitted"),
		}
	}
	return g.inner.Generate(ctx, request)
}

func (g *Granted) Models(ctx context.Context, query ModelQuery) (Catalog, error) {
	return g.inner.Models(ctx, query)
}

// ProviderID names the one provider behind the gate.
func (g *Granted) ProviderID() string { return g.provider }
