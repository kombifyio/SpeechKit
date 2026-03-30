package flows

import (
	"context"
	"fmt"
	"log"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/genkit"
)

// AgentInput is the input for the agent flow.
type AgentInput struct {
	Utterance         string `json:"utterance"`
	Locale            string `json:"locale,omitempty"`
	Selection         string `json:"selection,omitempty"`
	LastTranscription string `json:"lastTranscription,omitempty"`
}

// AgentOutput is the output of the agent flow.
type AgentOutput struct {
	Text   string `json:"text"`
	Action string `json:"action"` // "paste", "display", "silent"
}

// DefineAgentFlow creates and registers the agent Genkit flow.
// The agent can use tools and reason over multiple steps.
func DefineAgentFlow(g *genkit.Genkit, models []ai.Model, tools ...ai.ToolRef) *core.Flow[AgentInput, AgentOutput, struct{}] {
	return genkit.DefineFlow(g, "agent", func(ctx context.Context, input AgentInput) (AgentOutput, error) {
		if input.Utterance == "" {
			return AgentOutput{}, fmt.Errorf("agent: empty utterance")
		}

		systemPrompt := buildAgentSystemPrompt(input)
		userPrompt := buildAgentUserPrompt(input)

		var generateOpts []ai.GenerateOption
		generateOpts = append(generateOpts,
			ai.WithSystem(systemPrompt),
			ai.WithPrompt(userPrompt),
			ai.WithConfig(&ai.GenerationCommonConfig{
				MaxOutputTokens: 2048,
				Temperature:     0.5,
			}),
		)
		if len(tools) > 0 {
			generateOpts = append(generateOpts, ai.WithTools(tools...))
		}

		var lastErr error
		for _, model := range models {
			opts := append([]ai.GenerateOption{ai.WithModel(model)}, generateOpts...)
			resp, err := genkit.Generate(ctx, g, opts...)
			if err != nil {
				lastErr = err
				log.Printf("agent: model failed: %v", err)
				continue
			}

			text := resp.Text()
			if text == "" {
				return AgentOutput{Action: "silent"}, nil
			}

			return AgentOutput{
				Text:   text,
				Action: "paste",
			}, nil
		}

		if lastErr != nil {
			return AgentOutput{}, fmt.Errorf("agent: all models failed: %w", lastErr)
		}
		return AgentOutput{}, fmt.Errorf("agent: no models configured")
	})
}

func buildAgentSystemPrompt(input AgentInput) string {
	lang := "English"
	if input.Locale == "de" || input.Locale == "de-DE" {
		lang = "German"
	}

	prompt := fmt.Sprintf(`You are a helpful voice-activated AI assistant. Respond in %s unless the user requests otherwise.
You receive voice transcriptions from the user. Interpret them as instructions and respond helpfully.
Be concise and direct. Output only the answer or result.`, lang)

	if input.Selection != "" {
		prompt += fmt.Sprintf("\n\nThe user currently has the following text selected:\n%s", input.Selection)
	}
	if input.LastTranscription != "" {
		prompt += fmt.Sprintf("\n\nPrevious transcription for context:\n%s", input.LastTranscription)
	}

	return prompt
}

func buildAgentUserPrompt(input AgentInput) string {
	return input.Utterance
}
