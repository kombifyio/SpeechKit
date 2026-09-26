// Package genkitadapter keeps host-specific Assist wiring out of the core
// public assist package. Hosts can wrap their own flow callback here
// without making pkg/speechkit/assist import the AI runtime directly.
package genkitadapter

import (
	"context"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// GenerateFunc is the host flow callback the adapter wraps: it receives the
// Assist request and returns the generated result.
type GenerateFunc func(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error)

// Generator adapts a GenerateFunc to the assist.Generator contract.
type Generator struct {
	generate GenerateFunc
}

// NewGenerator wraps generate; a nil callback yields a Generator that
// returns an empty result.
func NewGenerator(generate GenerateFunc) *Generator {
	return &Generator{generate: generate}
}

// GenerateAssist implements assist.Generator by delegating to the wrapped
// callback. A nil receiver or callback returns an empty result and no error.
func (g *Generator) GenerateAssist(ctx context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	if g == nil || g.generate == nil {
		return speechkit.AssistResult{}, nil
	}
	return g.generate(ctx, req)
}
