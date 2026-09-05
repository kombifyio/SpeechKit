package tts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFoundryTTSBearerTokenWinsOverAPIKey(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("mp3-bytes"))
	}))
	defer server.Close()

	p := NewFoundry(FoundryOpts{
		APIKey:  "static-key",
		BaseURL: server.URL,
		BearerToken: func(ctx context.Context) (string, error) {
			return "minted-token", nil
		},
	})
	p.Validation = loopbackValidation

	if _, err := p.Synthesize(context.Background(), "hello", SynthesizeOpts{Format: "mp3"}); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if gotAuth != "Bearer minted-token" {
		t.Fatalf("Authorization = %q, want the minted token", gotAuth)
	}
}

// Sign-in mode has no resource key at all; Health must not demand one.
func TestFoundryTTSHealthAcceptsTokenSourceWithoutKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer minted-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("mp3-bytes"))
	}))
	defer server.Close()

	p := NewFoundry(FoundryOpts{
		BaseURL:     server.URL,
		BearerToken: func(ctx context.Context) (string, error) { return "minted-token", nil },
	})
	p.Validation = loopbackValidation
	if err := p.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}

	keyless := NewFoundry(FoundryOpts{BaseURL: server.URL})
	keyless.Validation = loopbackValidation
	if err := keyless.Health(context.Background()); err == nil {
		t.Fatal("Health must fail without any credential")
	}
}
