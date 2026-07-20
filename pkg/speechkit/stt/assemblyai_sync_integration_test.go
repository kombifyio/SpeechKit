//go:build integration

// Live smoke test against the AssemblyAI Sync Speech-to-Text API
// (Universal-3.5 Pro). Gated by the `integration` build tag so it is
// excluded from default `go test ./...` runs. Locally skips cleanly when
// ASSEMBLYAI_API_KEY is not injected.
//
// Run locally with a key in env:
//   ASSEMBLYAI_API_KEY=... go test -tags=integration -run TestAssemblyAI_SyncIntegration ./pkg/speechkit/stt/ -v

package stt

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/testutil"
)

func TestAssemblyAI_SyncIntegrationTranscribe(t *testing.T) {
	key := testutil.RequireEnvOrSkip(t, "ASSEMBLYAI_API_KEY", "Integration test requires live AssemblyAI credentials.")

	fixture := filepath.Join("..", "..", "..", "testdata", "e2e", "assist", "llm-shortq.wav")
	wav, err := os.ReadFile(fixture) // #nosec G304 -- fixed repo-relative test fixture path.
	if err != nil {
		t.Skipf("spoken fixture unavailable: %v", err)
	}

	provider := NewAssemblyAIProvider(key, "")
	resolved := ResolveTranscribeOptions("assemblyai", "stt.assemblyai.universal", TranscribeOpts{Language: "en"}, nil, nil)
	if !provider.syncEligible(wav, TranscribeOpts{}, resolved) {
		t.Fatalf("fixture should be sync-eligible (len=%d)", len(wav))
	}

	if err := provider.Warm(context.Background()); err != nil {
		t.Logf("warm failed (non-fatal): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := time.Now()
	result, err := provider.transcribeSync(ctx, wav, start, TranscribeOpts{
		Language: "en",
		Prompt:   "Short spoken question from a software development context.",
		Keyterms: []string{"SpeechKit"},
		ConversationContext: []string{
			"The user is testing a dictation feature.",
		},
	}, ResolveTranscribeOptions("assemblyai", "stt.assemblyai.universal", TranscribeOpts{Language: "en"}, nil, nil))
	if err != nil {
		t.Fatalf("live sync transcribe failed: %v", err)
	}
	if result.Text == "" {
		t.Fatal("expected non-empty transcript from spoken fixture")
	}
	if result.Model != "universal-3-5-pro" || result.Provider != "assemblyai" {
		t.Fatalf("unexpected result metadata: %+v", result)
	}
	t.Logf("sync transcript (%s, %.0fms): %q (words=%d, confidence=%.2f)",
		result.Model, float64(result.Duration.Milliseconds()), result.Text, len(result.Words), result.Confidence)
}
