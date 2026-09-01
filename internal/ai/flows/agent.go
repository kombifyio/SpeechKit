package flows

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/genkit"
	"github.com/kombifyio/SpeechKit/internal/ai/generation"
)

// AgentInput is the input for the agent flow.
type AgentInput struct {
	Utterance         string `json:"utterance"`
	Locale            string `json:"locale,omitempty"`
	Selection         string `json:"selection,omitempty"`
	LastTranscription string `json:"lastTranscription,omitempty"`
	SystemPrompt      string `json:"systemPrompt,omitempty"`
}

func DefineAgentFlowWithGenerator(g *genkit.Genkit, generator generation.Generator) *core.Flow[AgentInput, AgentOutput, struct{}] {
	return genkit.DefineFlow(g, "agent", func(ctx context.Context, input AgentInput) (AgentOutput, error) {
		if input.Utterance == "" {
			return AgentOutput{}, fmt.Errorf("agent: empty utterance")
		}
		result, err := generator.Generate(ctx, generation.Request{
			Purpose: generation.PurposeVoiceAgentThink, Locale: input.Locale,
			System: buildAgentSystemPrompt(input), Prompt: buildAgentUserPrompt(input),
			AffinityKey: "voice-agent", MaxOutputTokens: 2048, Temperature: 0.5,
		})
		if err != nil {
			return AgentOutput{}, fmt.Errorf("agent: %w", err)
		}
		if result.Text == "" {
			return AgentOutput{Action: "silent"}, nil
		}
		return AgentOutput{Text: result.Text, Action: "paste"}, nil
	})
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
		)
		if len(tools) > 0 {
			generateOpts = append(generateOpts, ai.WithTools(tools...))
		}

		var lastErr error
		for _, model := range models {
			opts := append([]ai.GenerateOption{ai.WithModel(model), generationConfigOption(model, 2048, 0.5)}, generateOpts...)
			resp, err := genkit.Generate(ctx, g, opts...)
			if err != nil {
				lastErr = err
				slog.Warn("agent: model failed", "err", err)
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

	if hostPrompt := strings.TrimSpace(input.SystemPrompt); hostPrompt != "" {
		prompt = hostPrompt + "\n\nSpeechKit voice behavior:\n" + prompt
	}

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
