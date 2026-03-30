package flows

import (
	"context"
	"fmt"
	"log"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/genkit"
)

// SummarizeInput is the input for the summarize flow.
type SummarizeInput struct {
	Text        string `json:"text"`
	Instruction string `json:"instruction,omitempty"`
	Locale      string `json:"locale,omitempty"`
}

// DefineSummarizeFlow creates and registers the summarize Genkit flow.
// Models are tried in order; first successful response wins.
func DefineSummarizeFlow(g *genkit.Genkit, models []ai.Model) *core.Flow[SummarizeInput, string, struct{}] {
	return genkit.DefineFlow(g, "summarize", func(ctx context.Context, input SummarizeInput) (string, error) {
		if input.Text == "" {
			return "", fmt.Errorf("summarize: empty text")
		}

		systemPrompt := buildSummarizeSystemPrompt(input.Locale)
		userPrompt := buildSummarizeUserPrompt(input)

		var lastErr error
		for _, model := range models {
			resp, err := genkit.Generate(ctx, g,
				ai.WithModel(model),
				ai.WithSystem(systemPrompt),
				ai.WithPrompt(userPrompt),
				ai.WithConfig(&ai.GenerationCommonConfig{
					MaxOutputTokens: 512,
					Temperature:     0.3,
				}),
			)
			if err != nil {
				lastErr = err
				log.Printf("summarize: model failed: %v", err)
				continue
			}
			return resp.Text(), nil
		}

		if lastErr != nil {
			return "", fmt.Errorf("summarize: all models failed: %w", lastErr)
		}
		return "", fmt.Errorf("summarize: no models configured")
	})
}

func buildSummarizeSystemPrompt(locale string) string {
	lang := "English"
	if locale == "de" || locale == "de-DE" {
		lang = "German"
	}
	return fmt.Sprintf(
		"You are a concise text assistant. Summarize or transform the given text as instructed. "+
			"Respond in %s unless the user requests otherwise. Output only the result, no preamble.", lang)
}

func buildSummarizeUserPrompt(input SummarizeInput) string {
	if input.Instruction != "" {
		return fmt.Sprintf("Instruction: %s\n\nText:\n%s", input.Instruction, input.Text)
	}
	return fmt.Sprintf("Summarize the following text concisely:\n\n%s", input.Text)
}
