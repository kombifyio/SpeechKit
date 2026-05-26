// Package genkitadapter keeps Genkit-specific Assist wiring out of the core
// public assist package. Hosts can wrap their own Genkit flow callback here
// without making pkg/speechkit/assist import Genkit directly.
package genkitadapter

import (
	"context"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type GenerateFunc func(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error)

type Generator struct {
	generate GenerateFunc
}

func NewGenerator(generate GenerateFunc) *Generator {
	return &Generator{generate: generate}
}

func (g *Generator) GenerateAssist(ctx context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	if g == nil || g.generate == nil {
		return speechkit.AssistResult{}, nil
	}
	return g.generate(ctx, req)
}
