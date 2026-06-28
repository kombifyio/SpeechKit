//go:build integration

// Live smoke test against the OpenAI TTS API.
// Gated by the `integration` build tag so it is excluded from default
// `go test ./...` runs. Locally skips cleanly when OPENAI_API_KEY is not
// injected; CI/required-config runs fail closed.
//
// Run locally with a key in env:
//   OPENAI_API_KEY=... go test -tags=integration -run TestOpenAI_Integration ./internal/tts/

package tts

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/testutil"
)

func TestOpenAI_IntegrationSynthesizeSmoke(t *testing.T) {
	apiKey := testutil.RequireEnvOrSkip(t, "OPENAI_API_KEY", "Integration test requires live OpenAI credentials.")

	provider := NewOpenAI(OpenAIOpts{APIKey: apiKey})

	res, err := provider.Synthesize(context.Background(), "Integration smoke test.", SynthesizeOpts{
		Locale: "en-US",
		Format: "mp3",
	})
	if err != nil {
		t.Fatalf("openai synthesize failed: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
	if len(res.Audio) == 0 {
		t.Error("expected non-empty audio bytes")
	}
	if res.Format != "mp3" {
		t.Errorf("format = %q, want mp3", res.Format)
	}
	if res.Provider != "openai" {
		t.Errorf("provider = %q, want openai", res.Provider)
	}
}
