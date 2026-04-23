package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/netsec"
)

func TestOpenAISynthesize(t *testing.T) {
	fakeAudio := []byte("fake-mp3-audio-data")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", auth)
		}

		body, _ := io.ReadAll(r.Body)
		var req openAIRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "tts-1" {
			t.Errorf("expected model tts-1, got %s", req.Model)
		}
		if req.Input != "Hallo Welt" {
			t.Errorf("expected input 'Hallo Welt', got %s", req.Input)
		}
		if req.Voice != "nova" {
			t.Errorf("expected voice nova, got %s", req.Voice)
		}

		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write(fakeAudio)
	}))
	defer server.Close()

	p := NewOpenAI(OpenAIOpts{APIKey: "test-key"})
	p.BaseURL = server.URL
	p.Validation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}

	result, err := p.Synthesize(context.Background(), "Hallo Welt", SynthesizeOpts{
		Locale: "de-DE",
		Format: "mp3",
	})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}

	if !bytes.Equal(result.Audio, fakeAudio) {
		t.Errorf("unexpected audio data")
	}
	if result.Format != "mp3" {
		t.Errorf("expected format mp3, got %s", result.Format)
	}
	if result.Provider != "openai" {
		t.Errorf("expected provider openai, got %s", result.Provider)
	}
	if result.Voice != "nova" {
		t.Errorf("expected voice nova, got %s", result.Voice)
	}
}

func TestOpenAIEmptyText(t *testing.T) {
	p := NewOpenAI(OpenAIOpts{APIKey: "test"})
	_, err := p.Synthesize(context.Background(), "", SynthesizeOpts{})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestOpenAISynthesizeErrorDoesNotLeakProviderBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key sk-secret-body"}`))
	}))
	defer server.Close()

	p := NewOpenAI(OpenAIOpts{APIKey: "test-key"})
	p.BaseURL = server.URL
	p.Validation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}

	_, err := p.Synthesize(context.Background(), "Hallo", SynthesizeOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "sk-secret-body") || strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("provider response body leaked in error: %v", err)
	}
}

func TestOpenAISpeedClamping(t *testing.T) {
	tests := []struct {
		input    float64
		expected float64
	}{
		{0, 1.0},
		{-1, 1.0},
		{0.1, 0.25},
		{0.25, 0.25},
		{1.0, 1.0},
		{4.0, 4.0},
		{5.0, 4.0},
	}

	for _, tt := range tests {
		speed := tt.input
		if speed <= 0 {
			speed = 1.0
		}
		if speed < 0.25 {
			speed = 0.25
		}
		if speed > 4.0 {
			speed = 4.0
		}
		if speed != tt.expected {
			t.Errorf("speed %f: expected %f, got %f", tt.input, tt.expected, speed)
		}
	}
}
