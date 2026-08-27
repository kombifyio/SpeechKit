package flows

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/genkit"
)

// summarizePerModelTimeout bounds a single model attempt inside the summarize
// chain. The chain used to run every model against the caller's shared
// deadline, so one unresponsive model (typically a dead local llama-server
// that accepts the TCP connection but never answers) consumed nearly the whole
// budget and every later cloud model started with an already-expired context.
// Each attempt now gets min(summarizePerModelTimeout, remaining budget), which
// guarantees at least two models see real time inside a ~15-20s caller budget.
// Var (not const) so tests can shrink it; production code never mutates it.
var summarizePerModelTimeout = 6 * time.Second

// perModelAttemptContext derives the bounded context for one model attempt.
// Returns ok=false when the overall budget is already exhausted.
func perModelAttemptContext(ctx context.Context, perModelCap time.Duration) (context.Context, context.CancelFunc, bool) {
	timeout := perModelCap
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, nil, false
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	return attemptCtx, cancel, true
}

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
			attemptCtx, cancel, ok := perModelAttemptContext(ctx, summarizePerModelTimeout)
			if !ok {
				if lastErr == nil {
					lastErr = ctx.Err()
				}
				break
			}
			resp, err := genkit.Generate(attemptCtx, g,
				ai.WithModel(model),
				ai.WithSystem(systemPrompt),
				ai.WithPrompt(userPrompt),
				generationConfigOption(model, 512, 0.3),
			)
			cancel()
			if err != nil {
				lastErr = err
				slog.Warn("summarize: model failed", "model", model.Name(), "err", err)
				if ctx.Err() != nil {
					break // overall budget exhausted — later models would get no time
				}
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
