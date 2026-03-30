package assist

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/tts"
)

type mockTTSProvider struct {
	audio []byte
	err   error
}

func (m *mockTTSProvider) Synthesize(_ context.Context, text string, _ tts.SynthesizeOpts) (*tts.Result, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &tts.Result{
		Audio:    m.audio,
		Format:   "mp3",
		Provider: "mock",
	}, nil
}

func (m *mockTTSProvider) Name() string                    { return "mock" }
func (m *mockTTSProvider) Health(_ context.Context) error { return nil }

func TestProcessShortcut(t *testing.T) {
	mockTTS := &mockTTSProvider{audio: []byte("fake-audio")}
	router := tts.NewRouter(tts.StrategyCloudFirst, mockTTS)
	pipeline := NewPipeline(nil, router, true)

	result, err := pipeline.Process(context.Background(), "copy last", ProcessOpts{Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "shortcut" {
		t.Errorf("expected action 'shortcut', got %s", result.Action)
	}
	if result.Shortcut != "copy_last" {
		t.Errorf("expected shortcut 'copy_last', got %s", result.Shortcut)
	}
	if result.Text == "" {
		t.Error("expected non-empty text")
	}
	if len(result.Audio) == 0 {
		t.Error("expected audio when TTS enabled")
	}
}

func TestProcessShortcutGerman(t *testing.T) {
	mockTTS := &mockTTSProvider{audio: []byte("audio")}
	router := tts.NewRouter(tts.StrategyCloudFirst, mockTTS)
	pipeline := NewPipeline(nil, router, true)

	result, err := pipeline.Process(context.Background(), "zusammenfassen", ProcessOpts{Locale: "de"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Shortcut != "summarize" {
		t.Errorf("expected shortcut 'summarize', got %s", result.Shortcut)
	}
	if result.Text != "Wird zusammengefasst..." {
		t.Errorf("unexpected text: %s", result.Text)
	}
}

func TestProcessNoTTS(t *testing.T) {
	pipeline := NewPipeline(nil, nil, false)

	result, err := pipeline.Process(context.Background(), "copy last", ProcessOpts{Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text == "" {
		t.Error("expected text even without TTS")
	}
	if len(result.Audio) != 0 {
		t.Error("expected no audio when TTS disabled")
	}
}

func TestProcessEmptyTranscript(t *testing.T) {
	pipeline := NewPipeline(nil, nil, false)
	_, err := pipeline.Process(context.Background(), "", ProcessOpts{})
	if err == nil {
		t.Fatal("expected error for empty transcript")
	}
}

func TestProcessNoLLMFallsThrough(t *testing.T) {
	pipeline := NewPipeline(nil, nil, false)

	// Non-shortcut text with no LLM configured.
	_, err := pipeline.Process(context.Background(), "what is the weather today", ProcessOpts{Locale: "en"})
	if err == nil {
		t.Fatal("expected error when no LLM configured")
	}
}
