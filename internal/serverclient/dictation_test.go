package serverclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/stt"
)

func TestDictationTranscribeRoundTrip(t *testing.T) {
	wantText := "hello world"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/dictation/transcribe" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer T" {
			t.Errorf("auth = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content-type = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req transcribeRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Format != "pcm16" {
			t.Errorf("format = %q, want pcm16", req.Format)
		}
		audioBytes, _ := base64.StdEncoding.DecodeString(req.AudioBase64)
		if string(audioBytes) != "fake pcm" {
			t.Errorf("audio = %q, want %q", audioBytes, "fake pcm")
		}
		if req.Language != "de" {
			t.Errorf("language = %q", req.Language)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(transcribeResponse{
			Text:       wantText,
			Language:   "de",
			DurationMs: 1234,
			Provider:   "huggingface",
			Model:      "openai/whisper-large-v3",
			Confidence: 0.91,
		})
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL, BearerToken: "T", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	d, err := NewDictation(c)
	if err != nil {
		t.Fatalf("NewDictation: %v", err)
	}
	res, err := d.Transcribe(context.Background(), []byte("fake pcm"), stt.TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != wantText {
		t.Errorf("text = %q", res.Text)
	}
	if res.Provider != "huggingface" {
		t.Errorf("provider = %q", res.Provider)
	}
	if res.Duration.Milliseconds() != 1234 {
		t.Errorf("duration = %v", res.Duration)
	}
}

func TestDictationRejectsEmptyAudio(t *testing.T) {
	c, err := New(Options{BaseURL: "http://localhost:9999"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	d, _ := NewDictation(c)
	_, err = d.Transcribe(context.Background(), nil, stt.TranscribeOpts{})
	if err == nil || !strings.Contains(err.Error(), "empty audio") {
		t.Fatalf("err = %v, want empty-audio guard", err)
	}
}

func TestDictationDecodesErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"provider_unavailable","message":"no STT provider","details":{"providers_tried":3}}}`))
	}))
	defer srv.Close()

	c, _ := New(Options{BaseURL: srv.URL, HTTPClient: srv.Client()})
	d, _ := NewDictation(c)
	_, err := d.Transcribe(context.Background(), []byte{0x01, 0x02}, stt.TranscribeOpts{})
	var se *ServerError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *ServerError", err)
	}
	if se.Status != http.StatusServiceUnavailable {
		t.Errorf("status = %d", se.Status)
	}
	if se.Code != "provider_unavailable" {
		t.Errorf("code = %q", se.Code)
	}
	if !IsCode(err, "provider_unavailable") {
		t.Errorf("IsCode returned false")
	}
	if se.Details["providers_tried"].(float64) != 3 {
		t.Errorf("details = %v", se.Details)
	}
}

func TestDictationFallsBackForNonEnvelopeBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>cloudflare gateway error</html>"))
	}))
	defer srv.Close()

	c, _ := New(Options{BaseURL: srv.URL, HTTPClient: srv.Client()})
	d, _ := NewDictation(c)
	_, err := d.Transcribe(context.Background(), []byte{0x01, 0x02}, stt.TranscribeOpts{})
	var se *ServerError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *ServerError", err)
	}
	if se.Status != http.StatusBadGateway {
		t.Errorf("status = %d", se.Status)
	}
	if !strings.Contains(se.Message, "cloudflare") {
		t.Errorf("message = %q, want truncated raw body", se.Message)
	}
}

func TestDictationHealth(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" {
			called = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL, HTTPClient: srv.Client()})
	d, _ := NewDictation(c)
	if err := d.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !called {
		t.Error("/readyz not called")
	}
}
