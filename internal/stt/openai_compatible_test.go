package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewVPSProvider_Defaults(t *testing.T) {
	p := NewVPSProvider("http://vps.example.com", "vps-key")
	if p.Name() != "vps" {
		t.Errorf("Name() = %q, want %q", p.Name(), "vps")
	}
	if p.Model != "whisper-1" {
		t.Errorf("Model = %q, want %q", p.Model, "whisper-1")
	}
	if p.BaseURL != "http://vps.example.com" {
		t.Errorf("BaseURL = %q", p.BaseURL)
	}
	if p.APIKey != "vps-key" {
		t.Errorf("APIKey = %q", p.APIKey)
	}
}

func TestNewOpenAISTTProvider_Defaults(t *testing.T) {
	p := NewOpenAISTTProvider("oai-key")
	if p.Name() != "openai" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openai")
	}
	if p.BaseURL != "https://api.openai.com" {
		t.Errorf("BaseURL = %q", p.BaseURL)
	}
	if p.Model != "whisper-1" {
		t.Errorf("Model = %q, want %q", p.Model, "whisper-1")
	}
	if p.APIKey != "oai-key" {
		t.Errorf("APIKey = %q", p.APIKey)
	}
}

func TestNewGroqSTTProvider_Defaults(t *testing.T) {
	p := NewGroqSTTProvider("groq-key")
	if p.Name() != "groq" {
		t.Errorf("Name() = %q, want %q", p.Name(), "groq")
	}
	if p.BaseURL != "https://api.groq.com/openai" {
		t.Errorf("BaseURL = %q", p.BaseURL)
	}
	if p.Model != "whisper-large-v3-turbo" {
		t.Errorf("Model = %q, want %q", p.Model, "whisper-large-v3-turbo")
	}
	if p.APIKey != "groq-key" {
		t.Errorf("APIKey = %q", p.APIKey)
	}
}

func TestOpenAICompat_Transcribe_Success(t *testing.T) {
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/v1/audio/transcriptions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		gotModel = r.FormValue("model")
		json.NewEncoder(w).Encode(map[string]string{"text": "transcribed text"})
	}))
	defer server.Close()

	p := NewOpenAICompatibleProvider("test", server.URL, "test-key", "default-model")
	result, err := p.Transcribe(context.Background(), []byte("wav-data"), TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "transcribed text" {
		t.Errorf("text = %q", result.Text)
	}
	if result.Provider != "test" {
		t.Errorf("provider = %q", result.Provider)
	}
	if result.Model != "default-model" {
		t.Errorf("model = %q, want %q", result.Model, "default-model")
	}
	if gotModel != "default-model" {
		t.Errorf("sent model = %q, want %q", gotModel, "default-model")
	}
	if result.Duration < 0 {
		t.Error("duration should not be negative")
	}
}

func TestOpenAICompat_Transcribe_ModelOverride(t *testing.T) {
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		gotModel = r.FormValue("model")
		json.NewEncoder(w).Encode(map[string]string{"text": "ok"})
	}))
	defer server.Close()

	p := NewOpenAICompatibleProvider("test", server.URL, "key", "default-model")
	result, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{Model: "custom-model"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotModel != "custom-model" {
		t.Errorf("sent model = %q, want %q", gotModel, "custom-model")
	}
	if result.Model != "custom-model" {
		t.Errorf("result model = %q, want %q", result.Model, "custom-model")
	}
}

func TestOpenAICompat_Transcribe_LanguageField(t *testing.T) {
	tests := []struct {
		name     string
		lang     string
		wantLang string // expected form value; empty means field should be absent
	}{
		{"explicit language", "en", "en"},
		{"auto language", "auto", ""},
		{"empty language", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotLang string
			var hasLang bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Fatalf("parse multipart: %v", err)
				}
				gotLang = r.FormValue("language")
				_, hasLang = r.MultipartForm.Value["language"]
				json.NewEncoder(w).Encode(map[string]string{"text": "ok"})
			}))
			defer server.Close()

			p := NewOpenAICompatibleProvider("test", server.URL, "key", "model")
			_, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{Language: tt.lang})
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			if tt.wantLang == "" && hasLang {
				t.Errorf("language field should be absent, got %q", gotLang)
			}
			if tt.wantLang != "" && gotLang != tt.wantLang {
				t.Errorf("language = %q, want %q", gotLang, tt.wantLang)
			}
		})
	}
}

func TestOpenAICompat_Transcribe_DefaultLanguageInResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"text": "test"})
	}))
	defer server.Close()

	p := NewOpenAICompatibleProvider("test", server.URL, "key", "model")
	result, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Language != "de" {
		t.Errorf("default language = %q, want %q", result.Language, "de")
	}
}

func TestOpenAICompat_Transcribe_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	p := NewOpenAICompatibleProvider("myapi", server.URL, "key", "model")
	_, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error: %v", err)
	}
	if !strings.Contains(err.Error(), "myapi") {
		t.Errorf("expected provider name in error: %v", err)
	}
}

func TestOpenAICompat_Transcribe_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	p := NewOpenAICompatibleProvider("test", server.URL, "key", "model")
	_, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestOpenAICompat_Transcribe_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	p := NewOpenAICompatibleProvider("test", server.URL, "key", "model")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := p.Transcribe(ctx, []byte("wav"), TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestOpenAICompat_Health_HealthEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(200)
			return
		}
		t.Errorf("unexpected path: %s", r.URL.Path)
		w.WriteHeader(404)
	}))
	defer server.Close()

	p := NewOpenAICompatibleProvider("test", server.URL, "key", "model")
	if err := p.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
}

func TestOpenAICompat_Health_FallbackToModels(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(404) // Not available
		case "/v1/models":
			w.WriteHeader(200) // Fallback succeeds
		default:
			w.WriteHeader(500)
		}
	}))
	defer server.Close()

	p := NewOpenAICompatibleProvider("test", server.URL, "key", "model")
	if err := p.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 requests, got %d: %v", len(paths), paths)
	}
	if paths[0] != "/health" {
		t.Errorf("first request = %q, want /health", paths[0])
	}
	if paths[1] != "/v1/models" {
		t.Errorf("second request = %q, want /v1/models", paths[1])
	}
}

func TestOpenAICompat_Health_BothFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer server.Close()

	p := NewOpenAICompatibleProvider("myapi", server.URL, "key", "model")
	err := p.Health(context.Background())
	if err == nil {
		t.Fatal("expected error when both endpoints fail")
	}
	if !strings.Contains(err.Error(), "myapi") {
		t.Errorf("expected provider name in error: %v", err)
	}
}

func TestOpenAICompat_Health_Unreachable(t *testing.T) {
	p := NewOpenAICompatibleProvider("test", "http://127.0.0.1:1", "key", "model")
	p.client.Timeout = 100 * time.Millisecond
	err := p.Health(context.Background())
	if err == nil {
		t.Error("expected error for unreachable host")
	}
}

func TestOpenAICompat_ImplementsSTTProvider(t *testing.T) {
	var _ STTProvider = (*OpenAICompatibleProvider)(nil)
}
