package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// A host that signs in with an identity provider hands the adapter a token
// source; the static key must then stay out of the request entirely.
func TestOpenAICompat_BearerTokenWinsOverAPIKey(t *testing.T) {
	var gotAuth []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"text": "ok"})
	}))
	defer server.Close()

	p := New("foundry", server.URL, "static-key", "gpt-4o-mini-transcribe")
	p.Validation = testValidation
	calls := 0
	p.BearerToken = func(ctx context.Context) (string, error) {
		calls++
		return "minted-token", nil
	}

	if _, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{}); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if err := p.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if calls != 2 {
		t.Fatalf("token source called %d times, want once per request", calls)
	}
	for _, auth := range gotAuth {
		if auth != "Bearer minted-token" {
			t.Fatalf("Authorization = %q, want the minted token", auth)
		}
	}
}

func TestOpenAICompat_BearerTokenFailureIsReportedNotRetriedWithKey(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		json.NewEncoder(w).Encode(map[string]string{"text": "ok"})
	}))
	defer server.Close()

	p := New("foundry", server.URL, "static-key", "m")
	p.Validation = testValidation
	wantErr := errors.New("not signed in")
	p.BearerToken = func(ctx context.Context) (string, error) { return "", wantErr }

	_, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want the token source error", err)
	}
	if requests != 0 {
		t.Fatalf("request sent despite missing token (%d)", requests)
	}
}
