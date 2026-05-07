package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientAddsBearerAndDecodesStatus(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/readyz" {
			t.Fatalf("path = %s, want /readyz", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "test"})
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL, Token: "secret"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	status, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Status != "ok" || status.Version != "test" {
		t.Fatalf("status = %+v", status)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestClientReturnsHTTPErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusTeapot)
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Status(context.Background())
	if err == nil {
		t.Fatal("Status succeeded, want error")
	}
	httpErr, ok := err.(HTTPError)
	if !ok {
		t.Fatalf("error type = %T, want HTTPError", err)
	}
	if httpErr.StatusCode != http.StatusTeapot || !strings.Contains(httpErr.Body, "nope") {
		t.Fatalf("HTTPError = %+v", httpErr)
	}
}

func TestClientTypedVocabularyAndTTSMethods(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.Method+" "+r.URL.RequestURI())
		switch r.URL.Path {
		case "/v1/vocabulary/dictionary":
			_ = json.NewEncoder(w).Encode(map[string]any{"entries": []map[string]any{{"spoken": "codex", "canonical": "Codex", "language": "en", "enabled": true}}})
		case "/v1/tts/synthesize":
			_ = json.NewEncoder(w).Encode(map[string]any{"audio_base64": "YXVkaW8=", "format": "mp3", "provider": "openai", "voice": "nova"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	entries, err := c.VocabularyEntries(context.Background(), "en")
	if err != nil {
		t.Fatalf("VocabularyEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Canonical != "Codex" {
		t.Fatalf("entries = %+v", entries)
	}
	tts, err := c.TTSSynthesize(context.Background(), TTSSynthesizeRequest{Text: "hello"})
	if err != nil {
		t.Fatalf("TTSSynthesize: %v", err)
	}
	if tts.Format != "mp3" || tts.Provider != "openai" {
		t.Fatalf("tts = %+v", tts)
	}
	want := []string{"GET /v1/vocabulary/dictionary?language=en", "POST /v1/tts/synthesize"}
	if strings.Join(gotPaths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths = %v, want %v", gotPaths, want)
	}
}
