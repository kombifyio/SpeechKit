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

	p := &VPSProvider{
		name:    "vps",
		BaseURL: server.URL,
		APIKey:  "vps-key",
		Model:   "whisper-1",
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	result, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{Language: "de"})
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

	p := NewVPSProvider(server.URL, "key")
	_, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
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

	p := NewVPSProvider(server.URL, "key")
	if err := p.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
}

func TestVPS_Health_Unreachable(t *testing.T) {
	p := NewVPSProvider("http://127.0.0.1:1", "key")
	p.client.Timeout = 100 * time.Millisecond
	err := p.Health(context.Background())
	if err == nil {
		t.Error("expected error for unreachable VPS")
	}
}

func TestVPS_Name(t *testing.T) {
	p := NewVPSProvider("http://example.com", "key")
	if p.Name() != "vps" {
		t.Errorf("Name() = %q", p.Name())
	}
}
