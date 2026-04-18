//go:build integration

// Live end-to-end smoke test for the assist Genkit flow against a real
// Gemini model. Gated by the `integration` build tag so it is excluded
// from default `go test ./...` runs. Skips cleanly when GOOGLE_AI_API_KEY
// is not injected.
//
// Run locally with a key in env:
//   GOOGLE_AI_API_KEY=... go test -tags=integration -run TestAssistFlow_Integration ./internal/ai/flows/

package flows

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/googlegenai"
)

func TestAssistFlow_IntegrationWithGemini(t *testing.T) {
	apiKey := os.Getenv("GOOGLE_AI_API_KEY")
	if apiKey == "" {
		t.Skip("GOOGLE_AI_API_KEY not set — integration test requires live Google AI credentials")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	g := genkit.Init(ctx, genkit.WithPlugins(&googlegenai.GoogleAI{APIKey: apiKey}))

	// Gemini 2.5 Flash is the assist-tier model SpeechKit uses in production.
	// If the API rejects the name, surface a test failure — a silent skip
	// here would hide a real breakage when the vendor retires a model.
	model := genkit.LookupModel(g, "googleai/gemini-2.5-flash")
	if model == nil {
		t.Fatal("googleai/gemini-2.5-flash not registered — plugin init failure")
	}

	flow := DefineAssistFlow(g, []ai.Model{model})

	out, err := flow.Run(ctx, AssistInput{
		Utterance: "Reply with exactly the single word: hello.",
		Locale:    "en",
	})
	if err != nil {
		t.Fatalf("assist flow failed: %v", err)
	}
	if out.Action != "respond" && out.Action != "silent" {
		t.Errorf("unexpected action %q", out.Action)
	}
	if out.Action == "respond" && strings.TrimSpace(out.Text) == "" {
		t.Error("respond action with empty text")
	}
	if out.Locale != "en" {
		t.Errorf("locale = %q, want en", out.Locale)
	}
}
