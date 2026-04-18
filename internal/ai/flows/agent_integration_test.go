//go:build integration

// Live end-to-end smoke test for the agent Genkit flow against a real
// Groq-hosted model. Gated by the `integration` build tag.
//
// Run locally with a key in env:
//   GROQ_API_KEY=... go test -tags=integration -run TestAgentFlow_Integration ./internal/ai/flows/

package flows

import (
	"context"
	"os"
	"testing"

	appai "github.com/kombifyio/SpeechKit/internal/ai"
)

func TestAgentFlow_Integration(t *testing.T) {
	groqKey := os.Getenv("GROQ_API_KEY")
	if groqKey == "" {
		t.Skip("GROQ_API_KEY not set â€” integration test requires live Groq credentials")
	}

	rt, err := appai.Init(context.Background(), appai.Config{
		GroqAPIKey:     groqKey,
		GroqAgentModel: "llama-3.1-8b-instant",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	flow := DefineAgentFlow(rt.G, rt.AgentModels())
	result, err := flow.Run(context.Background(), AgentInput{
		Utterance: "Was ist 2 plus 2?",
		Locale:    "de",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Text == "" {
		t.Fatal("expected non-empty response")
	}
	if result.Action != "paste" {
		t.Errorf("action = %q, want paste", result.Action)
	}
	t.Logf("Agent: %s (action=%s)", result.Text, result.Action)
}
