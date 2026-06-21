package stt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewOpenRouterSTTProviderDefaults(t *testing.T) {
	p := NewOpenRouterSTTProvider("or-key", "")

	if p.Name() != "openrouter" {
		t.Fatalf("Name() = %q, want %q", p.Name(), "openrouter")
	}
	if p.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("BaseURL = %q, want OpenRouter API v1", p.BaseURL)
	}
	if p.Model != "openai/whisper-1" {
		t.Fatalf("Model = %q, want %q", p.Model, "openai/whisper-1")
	}
	if p.APIKey != "or-key" {
		t.Fatalf("APIKey = %q, want %q", p.APIKey, "or-key")
	}
}

func TestOpenRouterSTTTranscribeUsesJSONAudioEndpoint(t *testing.T) {
	var gotPath string
	var gotAuthorization string
	var gotContentType string
	var gotModel string
	var gotLanguage string
	var gotFormat string
	var gotAudio []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")

		var body struct {
			InputAudio struct {
				Data   string `json:"data"`
				Format string `json:"format"`
			} `json:"input_audio"`
			Model    string `json:"model"`
			Language string `json:"language"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel = body.Model
		gotLanguage = body.Language
		gotFormat = body.InputAudio.Format
		audio, err := base64.StdEncoding.DecodeString(body.InputAudio.Data)
		if err != nil {
			t.Fatalf("decode audio: %v", err)
		}
		gotAudio = audio

		json.NewEncoder(w).Encode(map[string]any{
			"text": "transcribed through openrouter",
			"usage": map[string]any{
				"seconds": 1.2,
			},
		})
	}))
	defer server.Close()

	p := NewOpenRouterSTTProvider("test-key", "openai/whisper-1")
	p.BaseURL = server.URL + "/api/v1"
	p.Validation = testValidation

	result, err := p.Transcribe(context.Background(), []byte("pcm-data"), TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	if gotPath != "/api/v1/audio/transcriptions" {
		t.Fatalf("path = %q, want %q", gotPath, "/api/v1/audio/transcriptions")
	}
	if gotAuthorization != "Bearer test-key" {
		t.Fatalf("Authorization = %q, want bearer key", gotAuthorization)
	}
	if !strings.HasPrefix(gotContentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotModel != "openai/whisper-1" {
		t.Fatalf("model = %q, want %q", gotModel, "openai/whisper-1")
	}
	if gotLanguage != "de" {
		t.Fatalf("language = %q, want %q", gotLanguage, "de")
	}
	if gotFormat != "wav" {
		t.Fatalf("format = %q, want %q", gotFormat, "wav")
	}
	if len(gotAudio) < 44 || string(gotAudio[:4]) != "RIFF" || string(gotAudio[8:12]) != "WAVE" {
		t.Fatalf("raw PCM upload should be wrapped as WAV, got %q", string(gotAudio[:min(len(gotAudio), 12)]))
	}
	if result.Text != "transcribed through openrouter" {
		t.Fatalf("text = %q", result.Text)
	}
	if result.Provider != "openrouter" {
		t.Fatalf("provider = %q, want openrouter", result.Provider)
	}
}

func TestOpenRouterSTTImplementsProvider(t *testing.T) {
	var _ STTProvider = (*OpenRouterSTTProvider)(nil)
}
