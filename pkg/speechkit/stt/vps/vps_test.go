package vps

import (
	"context"
	"encoding/json"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVPS_Transcribe_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/v1/audio/transcriptions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer vps-key" {
			t.Errorf("expected Bearer vps-key, got %s", r.Header.Get("Authorization"))
		}
		json.NewEncoder(w).Encode(map[string]string{"text": "VPS result"})
	}))
	defer server.Close()

	p := New(server.URL, "vps-key")

	result, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "VPS result" {
		t.Errorf("text = %q", result.Text)
	}
	if result.Provider != "vps" {
		t.Errorf("provider = %q", result.Provider)
	}
}

func TestVPS_Transcribe_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	p := New(server.URL, "key")
	_, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error: %v", err)
	}
}

func TestVPS_Health_OK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/health") {
			t.Errorf("expected /health path, got %s", r.URL.Path)
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	p := New(server.URL, "key")
	if err := p.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
}

func TestVPS_Health_Unreachable(t *testing.T) {
	p := New("http://127.0.0.1:1", "key")
	p.SetHTTPClient(&http.Client{Timeout: 100 * time.Millisecond})
	err := p.Health(context.Background())
	if err == nil {
		t.Error("expected error for unreachable VPS")
	}
}

func TestVPS_Name(t *testing.T) {
	p := New("http://example.com", "key")
	if p.Name() != "vps" {
		t.Errorf("Name() = %q", p.Name())
	}
}

func TestNewVPSProvider_Defaults(t *testing.T) {
	p := New("http://vps.example.com", "vps-key")
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
