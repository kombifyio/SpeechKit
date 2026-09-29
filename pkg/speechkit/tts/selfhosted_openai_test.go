package tts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSelfHostedOpenAI_SynthesizesAgainstLocalServer(t *testing.T) {
	var got openAIRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("RIFF-local"))
	}))
	defer server.Close()

	// Operators often copy the OpenAI-style base URL including /v1.
	provider, err := NewSelfHostedOpenAI(SelfHostedOpenAIOpts{BaseURL: server.URL + "/v1/"})
	if err != nil {
		t.Fatalf("NewSelfHostedOpenAI: %v", err)
	}
	result, err := provider.Synthesize(context.Background(), "Hallo", SynthesizeOpts{Format: "wav"})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if string(result.Audio) != "RIFF-local" || result.Format != "wav" || result.Provider != "kokoro" {
		t.Fatalf("result = %+v", result)
	}
	if got.Model != "kokoro" || got.Voice != "af_bella" || got.Input != "Hallo" {
		t.Fatalf("request = %+v, want Kokoro defaults", got)
	}
	if provider.Kind() != ProviderKindLocalProvider {
		t.Fatalf("kind = %q, want local provider", provider.Kind())
	}
}
