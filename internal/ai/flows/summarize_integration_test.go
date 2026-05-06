//go:build integration

// Live end-to-end smoke test for the summarize Genkit flow against a
// real Groq-hosted model. Gated by the `integration` build tag.
//
// Run locally with a key in env:
//   GROQ_API_KEY=... go test -tags=integration -run TestSummarizeFlow_Integration ./internal/ai/flows/

package flows

import (
	"context"
	"os"
	"testing"

	appai "github.com/kombifyio/SpeechKit/internal/ai"
)

func TestSummarizeFlow_Integration(t *testing.T) {
	groqKey := os.Getenv("GROQ_API_KEY")
	if groqKey == "" {
		t.Skip("GROQ_API_KEY not set â€” integration test requires live Groq credentials")
	}

	rt, err := appai.Init(context.Background(), appai.Config{
		GroqAPIKey:       groqKey,
		GroqUtilityModel: "llama-3.1-8b-instant",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	flow := DefineSummarizeFlow(rt.G, rt.UtilityModels())
	result, err := flow.Run(context.Background(), SummarizeInput{
		Text:   "Kubernetes ist ein Open-Source-System zur Automatisierung der Bereitstellung, Skalierung und Verwaltung von containerisierten Anwendungen. Es wurde urspruenglich von Google entworfen und wird nun von der Cloud Native Computing Foundation gepflegt.",
		Locale: "de",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty summary")
	}
	t.Logf("Summary: %s", result)
}
