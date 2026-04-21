package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/netsec"
)

// loopbackValidation permits httptest.Server URLs (loopback + http://).
// Production code uses the strict zero-value Validation which rejects both.
var loopbackValidation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}

func newHFTestProvider(t *testing.T, baseURL string) *HuggingFace {
	t.Helper()
	p := NewHuggingFace(HuggingFaceOpts{Token: "test-token"})
	p.BaseURL = baseURL
	p.Validation = loopbackValidation
	return p
}

func TestHuggingFace_NewDefaultsModel(t *testing.T) {
	p := NewHuggingFace(HuggingFaceOpts{Token: "t"})
	if p.model != hfDefaultTTSModel {
		t.Errorf("default model = %q, want %q", p.model, hfDefaultTTSModel)
	}
	if p.BaseURL != hfTTSBaseURL {
		t.Errorf("default BaseURL = %q, want %q", p.BaseURL, hfTTSBaseURL)
	}
}

func TestHuggingFace_NewCustomModel(t *testing.T) {
	p := NewHuggingFace(HuggingFaceOpts{Token: "t", Model: "my/custom-model"})
	if p.model != "my/custom-model" {
		t.Errorf("custom model not applied: %q", p.model)
	}
}

func TestHuggingFace_Synthesize_Success(t *testing.T) {
	fakeAudio := []byte("fake-flac-audio")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("auth header = %q, want 'Bearer test-token'", auth)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}
		// Endpoint must contain the model ID.
		if !strings.Contains(r.URL.Path, "parler-tts/parler-tts-mini-multilingual-v1.1") {
			t.Errorf("path = %q, missing model id", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var req hfTTSRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Inputs != "Hallo Welt" {
			t.Errorf("inputs = %q, want 'Hallo Welt'", req.Inputs)
		}
		if got := req.Parameters["description"]; !strings.Contains(got, "German") {
			t.Errorf("description = %q, want contains 'German'", got)
		}

		w.Header().Set("Content-Type", "audio/flac")
		w.Write(fakeAudio)
	}))
	defer server.Close()

	p := newHFTestProvider(t, server.URL)
	res, err := p.Synthesize(context.Background(), "Hallo Welt", SynthesizeOpts{Locale: "de-DE"})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if !bytes.Equal(res.Audio, fakeAudio) {
		t.Errorf("Audio bytes mismatch")
	}
	if res.Format != "flac" {
		t.Errorf("Format = %q, want flac", res.Format)
	}
	if res.SampleRate != hfTTSSampleRate {
		t.Errorf("SampleRate = %d, want %d", res.SampleRate, hfTTSSampleRate)
	}
	if res.Provider != "huggingface" {
		t.Errorf("Provider = %q, want huggingface", res.Provider)
	}
}

func TestHuggingFace_Synthesize_ContentTypeFormats(t *testing.T) {
	cases := []struct {
		contentType string
		wantFormat  string
	}{
		{"audio/wav", "wav"},
		{"audio/x-wav", "wav"},
		{"audio/mpeg", "mp3"},
		{"audio/ogg", "ogg"},
		{"audio/flac", "flac"},          // default fallback
		{"application/unknown", "flac"}, // default fallback
		{"", "flac"},                    // default fallback
	}
	for _, tc := range cases {
		t.Run(tc.contentType, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				w.Write([]byte("audio-bytes"))
			}))
			defer server.Close()

			p := newHFTestProvider(t, server.URL)
			res, err := p.Synthesize(context.Background(), "hi", SynthesizeOpts{})
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			if res.Format != tc.wantFormat {
				t.Errorf("content-type %q -> format %q, want %q", tc.contentType, res.Format, tc.wantFormat)
			}
		})
	}
}

func TestHuggingFace_Synthesize_OmitsParametersForAutoLocale(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req hfTTSRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.Parameters != nil {
			t.Errorf("Parameters = %+v, want nil for auto locale", req.Parameters)
		}
		w.Header().Set("Content-Type", "audio/flac")
		w.Write([]byte("a"))
	}))
	defer server.Close()

	p := newHFTestProvider(t, server.URL)
	if _, err := p.Synthesize(context.Background(), "hi", SynthesizeOpts{Locale: "auto"}); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
}

func TestHuggingFace_Synthesize_EmptyTextRejected(t *testing.T) {
	p := NewHuggingFace(HuggingFaceOpts{Token: "t"})
	_, err := p.Synthesize(context.Background(), "", SynthesizeOpts{})
	if err == nil || !strings.Contains(err.Error(), "empty text") {
		t.Errorf("err = %v, want 'empty text'", err)
	}
}

func TestHuggingFace_Synthesize_InvalidModelRejected(t *testing.T) {
	p := NewHuggingFace(HuggingFaceOpts{Token: "t", Model: "../traversal"})
	_, err := p.Synthesize(context.Background(), "hi", SynthesizeOpts{})
	if err == nil || !strings.Contains(err.Error(), "invalid model") {
		t.Errorf("err = %v, want 'invalid model'", err)
	}
}

func TestHuggingFace_Synthesize_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"model loading"}`))
	}))
	defer server.Close()

	p := newHFTestProvider(t, server.URL)
	_, err := p.Synthesize(context.Background(), "hi", SynthesizeOpts{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("err = %v, want contains '503'", err)
	}
	if !strings.Contains(err.Error(), "model loading") {
		t.Errorf("err = %v, want contains body 'model loading'", err)
	}
}

func TestHuggingFace_Synthesize_InvalidBaseURLRejected(t *testing.T) {
	p := NewHuggingFace(HuggingFaceOpts{Token: "t"})
	p.BaseURL = "http://169.254.169.254/latest/meta-data/" // AWS IMDS — blocked by strict validation
	// Validation left at zero value (strict: no loopback, no http, no private nets)
	_, err := p.Synthesize(context.Background(), "hi", SynthesizeOpts{})
	if err == nil {
		t.Fatal("expected SSRF guard to reject IMDS URL, got nil")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("err = %v, want 'endpoint' wrapper", err)
	}
}

func TestHuggingFace_Name(t *testing.T) {
	if got := (&HuggingFace{}).Name(); got != "huggingface" {
		t.Errorf("Name() = %q, want 'huggingface'", got)
	}
}

func TestHuggingFace_Health_NoToken(t *testing.T) {
	err := (&HuggingFace{}).Health(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no token") {
		t.Errorf("Health with no token = %v, want 'no token'", err)
	}
}

func TestHuggingFace_Health_WithToken(t *testing.T) {
	p := NewHuggingFace(HuggingFaceOpts{Token: "t"})
	if err := p.Health(context.Background()); err != nil {
		t.Errorf("Health with token = %v, want nil", err)
	}
}

func TestVoiceDescriptionForLocale(t *testing.T) {
	cases := map[string]string{
		"de":    "German",
		"de-DE": "German",
		"fr":    "French",
		"fr-FR": "French",
		"es":    "Spanish",
		"es-ES": "Spanish",
		"en":    "moderate pace",
		"it":    "moderate pace",
		"":      "moderate pace",
	}
	for locale, want := range cases {
		if got := voiceDescriptionForLocale(locale); !strings.Contains(got, want) {
			t.Errorf("voiceDescriptionForLocale(%q) = %q, want contains %q", locale, got, want)
		}
	}
}
