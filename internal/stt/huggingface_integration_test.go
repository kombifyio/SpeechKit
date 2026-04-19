//go:build integration

// Live smoke test against the Hugging Face Inference API.
// Gated by the `integration` build tag so it is excluded from default
// `go test ./...` runs. Additionally skips cleanly when HF_TOKEN is not
// injected (e.g. OSS contributors without credentials).
//
// Run locally with a token in env:
//   HF_TOKEN=... go test -tags=integration -run TestHF_IntegrationUploadSmoke ./internal/stt/

package stt

import (
	"context"
	"os"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/audio"
)

func TestHF_IntegrationUploadSmoke(t *testing.T) {
	token := os.Getenv("HF_TOKEN")
	if token == "" {
		t.Skip("HF_TOKEN not set — integration test requires live Hugging Face credentials")
	}

	model := os.Getenv("HF_MODEL")
	if model == "" {
		model = "openai/whisper-large-v3"
	}

	provider := NewHuggingFaceProvider(model, token)
	wav := audio.PCMToWAV(make([]byte, audio.SampleRate*audio.BytesPerSample))

	result, err := provider.Transcribe(context.Background(), wav, TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("live hf transcribe failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
}
