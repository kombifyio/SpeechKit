package stt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestHFProvider(serverURL string) *HuggingFaceProvider {
	return &HuggingFaceProvider{
		Model:   "test-model",
		Token:   "test-token",
		BaseURL: serverURL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

func TestHF_Transcribe_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "audio/wav" {
			t.Errorf("expected audio/wav content type, got %q", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("expected Bearer test-token")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != "fake-wav" {
			t.Errorf("unexpected request body %q", string(body))
		}
		json.NewEncoder(w).Encode(map[string]string{"text": "Hallo Welt"})
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("fake-wav"), TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "Hallo Welt" {
		t.Errorf("text = %q, want 'Hallo Welt'", result.Text)
	}
	if result.Provider != "huggingface" {
		t.Errorf("provider = %q", result.Provider)
	}
	if result.Duration < 0 {
		t.Error("duration should not be negative")
	}
}

func TestHF_Transcribe_503ModelLoading(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		w.Write([]byte(`{"error":"Model is loading"}`))
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	_, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected 503 in error: %v", err)
	}
}

func TestHF_Transcribe_429RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"Rate limit exceeded"}`))
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	_, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected 429 in error: %v", err)
	}
}

func TestHF_Transcribe_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	_, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestHF_Transcribe_EmptyText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"text": ""})
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "" {
		t.Errorf("expected empty text, got %q", result.Text)
	}
}

func TestHF_Transcribe_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := p.Transcribe(ctx, []byte("wav"), TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestHF_Health_OK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	if err := p.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
}

func TestHF_Health_503(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	if err := p.Health(context.Background()); err == nil {
		t.Error("expected error for 503")
	}
}

func TestHF_Health_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	if err := p.Health(context.Background()); err == nil {
		t.Error("expected error for 404")
	}
}

func TestHF_Name(t *testing.T) {
	p := NewHuggingFaceProvider("model", "token")
	if p.Name() != "huggingface" {
		t.Errorf("Name() = %q", p.Name())
	}
	if p.BaseURL != hfBaseURL {
		t.Errorf("BaseURL = %q, want %q", p.BaseURL, hfBaseURL)
	}
}

func TestHF_Transcribe_DefaultLanguage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"text": "test"})
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Language != "de" {
		t.Errorf("default language = %q, want 'de'", result.Language)
	}
}
