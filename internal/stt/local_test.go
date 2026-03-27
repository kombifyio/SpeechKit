package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLocal_Transcribe_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"text": "Local result"})
	}))
	defer server.Close()

	p := &LocalProvider{
		BaseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
	p.ready.Store(true)

	result, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "Local result" {
		t.Errorf("text = %q", result.Text)
	}
	if result.Provider != "local" {
		t.Errorf("provider = %q", result.Provider)
	}
}

func TestLocal_Transcribe_NotReady(t *testing.T) {
	p := NewLocalProvider(8080, "/fake/model.bin", "cpu")
	_, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error when not ready")
	}
}

func TestLocal_Health_NotRunning(t *testing.T) {
	p := NewLocalProvider(8080, "/fake/model.bin", "cpu")
	err := p.Health(context.Background())
	if err == nil {
		t.Error("expected error when not running")
	}
}

func TestLocal_Health_OK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	p := &LocalProvider{
		BaseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
	p.ready.Store(true)

	if err := p.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
}

func TestLocal_IsReady(t *testing.T) {
	p := NewLocalProvider(8080, "/model.bin", "cpu")
	if p.IsReady() {
		t.Error("should not be ready before StartServer")
	}
}

func TestLocal_Name(t *testing.T) {
	p := NewLocalProvider(8080, "/model.bin", "cpu")
	if p.Name() != "local" {
		t.Errorf("Name() = %q", p.Name())
	}
}
