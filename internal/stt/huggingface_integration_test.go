package stt

import (
	"context"
	"os"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/audio"
)

func TestHF_IntegrationUploadSmoke(t *testing.T) {
	if os.Getenv("SPEECHKIT_HF_SMOKE") != "1" {
		t.Skip("set SPEECHKIT_HF_SMOKE=1 to run live Hugging Face upload smoke test")
	}

	token := os.Getenv("HF_TOKEN")
	if token == "" {
		t.Skip("HF_TOKEN not set")
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
