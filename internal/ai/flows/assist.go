package flows

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/genkit"
)

// AssistInput is the input for the assist flow.
type AssistInput struct {
	Utterance string `json:"utterance"`          // The user's spoken text
	Locale    string `json:"locale,omitempty"`    // "de", "en", etc.
	Selection string `json:"selection,omitempty"` // Text currently selected in the active window
	Context   string `json:"context,omitempty"`   // Additional context (last transcription, active app, etc.)
}

// AssistOutput is the Genkit flow output (before TTS synthesis).
type AssistOutput struct {
	Text      string `json:"text"`      // Full LLM response text (always present)
	SpeakText string `json:"speakText"` // TTS-optimized text (shorter, more natural than Text)
	Action    string `json:"action"`    // "respond", "execute", "silent"
	Locale    string `json:"locale"`    // Response language
}

// DefineAssistFlow creates the assist Genkit flow for single-turn voice interactions.
// Optimized for speed: uses utility models, short responses, low temperature.
func DefineAssistFlow(g *genkit.Genkit, models []ai.Model) *core.Flow[AssistInput, AssistOutput, struct{}] {
	return genkit.DefineFlow(g, "assist", func(ctx context.Context, input AssistInput) (AssistOutput, error) {
		if input.Utterance == "" {
			return AssistOutput{}, fmt.Errorf("assist: empty utterance")
		}

		locale := input.Locale
		if locale == "" {
			locale = "en"
		}

		systemPrompt := buildAssistSystemPrompt(locale, input)
		userPrompt := input.Utterance

		var lastErr error
		for _, model := range models {
			resp, err := genkit.Generate(ctx, g,
				ai.WithModel(model),
				ai.WithSystem(systemPrompt),
				ai.WithPrompt(userPrompt),
				ai.WithConfig(&ai.GenerationCommonConfig{
					MaxOutputTokens: 1024,
					Temperature:     0.4,
				}),
			)
			if err != nil {
				lastErr = err
				slog.Warn("assist: model failed", "err", err)
				continue
			}

			text := resp.Text()
			if text == "" {
				return AssistOutput{Action: "silent", Locale: locale}, nil
			}

			return AssistOutput{
				Text:      text,
				SpeakText: text, // For now, same as text. Can be optimized later with a separate TTS-text generation step.
				Action:    "respond",
				Locale:    locale,
			}, nil
		}

		if lastErr != nil {
			return AssistOutput{}, fmt.Errorf("assist: all models failed: %w", lastErr)
		}
		return AssistOutput{}, fmt.Errorf("assist: no models configured")
	})
}

func buildAssistSystemPrompt(locale string, input AssistInput) string {
	lang := langName(locale)

	prompt := fmt.Sprintf(`You are a fast, helpful voice assistant. Respond in %s.
You receive a voice transcription. Answer concisely in 1-3 sentences.
Your response will be spoken aloud via TTS, so:
- Be natural and conversational
- Avoid markdown, bullet points, or formatting
- Avoid long lists or technical details unless asked
- If the question is unclear, ask a brief clarifying question`, lang)

	if input.Selection != "" {
		prompt += fmt.Sprintf("\n\nThe user has this text selected:\n%s", input.Selection)
	}
	if input.Context != "" {
		prompt += fmt.Sprintf("\n\nAdditional context:\n%s", input.Context)
	}

	return prompt
}

func langName(locale string) string {
	switch locale {
	case "de", "de-DE":
		return "German"
	case "fr", "fr-FR":
		return "French"
	case "es", "es-ES":
		return "Spanish"
	case "it", "it-IT":
		return "Italian"
	case "pt", "pt-BR":
		return "Portuguese"
	case "ja", "ja-JP":
		return "Japanese"
	case "ko", "ko-KR":
		return "Korean"
	case "zh", "zh-CN":
		return "Chinese"
	default:
		return "English"
	}
}
