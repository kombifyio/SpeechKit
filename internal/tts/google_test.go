package tts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGoogleSynthesize(t *testing.T) {
	fakeAudio := []byte("fake-google-audio")
	encoded := base64.StdEncoding.EncodeToString(fakeAudio)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var req googleTTSRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.Input.Text != "Hallo Welt" {
			t.Errorf("expected 'Hallo Welt', got %q", req.Input.Text)
		}
		if req.Voice.LanguageCode != "de-DE" {
			t.Errorf("expected de-DE, got %s", req.Voice.LanguageCode)
		}
		if req.AudioConfig.AudioEncoding != "MP3" {
			t.Errorf("expected MP3, got %s", req.AudioConfig.AudioEncoding)
		}
		json.NewEncoder(w).Encode(googleTTSResponse{AudioContent: encoded})
	}))
	defer srv.Close()

	g := &Google{
		apiKey: "test-key",
		voice:  googleDefaultVoice,
		client: srv.Client(),
	}
	// Override the endpoint by using the test server client.
	// Google provider builds its own URL, so we test via a custom request approach.
	// For proper e2e mock we need to patch the URL. Since the struct doesn't expose
	// the endpoint, we test by hijacking the client transport.
	g.client = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			// Rewrite to the test server.
			r.URL.Scheme = "http"
			r.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(r)
		}),
	}

	result, err := g.Synthesize(context.Background(), "Hallo Welt", SynthesizeOpts{
		Locale: "de-DE",
		Format: "mp3",
	})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if string(result.Audio) != string(fakeAudio) {
		t.Errorf("audio mismatch: got %d bytes", len(result.Audio))
	}
	if result.Format != "mp3" {
		t.Errorf("expected mp3, got %s", result.Format)
	}
	if result.Provider != "google" {
		t.Errorf("expected google, got %s", result.Provider)
	}
}

func TestGoogleSynthesizeWAV(t *testing.T) {
	fakeAudio := []byte("fake-wav")
	encoded := base64.StdEncoding.EncodeToString(fakeAudio)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req googleTTSRequest
		json.Unmarshal(body, &req)
		if req.AudioConfig.AudioEncoding != "LINEAR16" {
			t.Errorf("expected LINEAR16, got %s", req.AudioConfig.AudioEncoding)
		}
		json.NewEncoder(w).Encode(googleTTSResponse{AudioContent: encoded})
	}))
	defer srv.Close()

	g := &Google{
		apiKey: "test-key",
		voice:  googleDefaultVoice,
		client: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				r.URL.Scheme = "http"
				r.URL.Host = srv.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(r)
			}),
		},
	}

	result, err := g.Synthesize(context.Background(), "test", SynthesizeOpts{Format: "wav"})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if result.Format != "wav" {
		t.Errorf("expected wav, got %s", result.Format)
	}
}

func TestGoogleEmptyText(t *testing.T) {
	g := NewGoogle(GoogleOpts{APIKey: "test"})
	_, err := g.Synthesize(context.Background(), "", SynthesizeOpts{})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestGoogleHealthNoKey(t *testing.T) {
	g := NewGoogle(GoogleOpts{})
	if err := g.Health(context.Background()); err == nil {
		t.Error("expected error without API key")
	}
}

func TestGoogleHealthWithKey(t *testing.T) {
	g := NewGoogle(GoogleOpts{APIKey: "test-key"})
	if err := g.Health(context.Background()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGoogleServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	g := &Google{
		apiKey: "test-key",
		voice:  googleDefaultVoice,
		client: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				r.URL.Scheme = "http"
				r.URL.Host = srv.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(r)
			}),
		},
	}
	_, err := g.Synthesize(context.Background(), "test", SynthesizeOpts{})
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

func TestLocaleToLanguageCode(t *testing.T) {
	tests := []struct {
		locale string
		want   string
	}{
		{"de", "de-DE"},
		{"de-DE", "de-DE"},
		{"en", "en-US"},
		{"en-US", "en-US"},
		{"en-GB", "en-GB"},
		{"fr", "fr-FR"},
		{"ja", "ja-JP"},
		{"unknown", "en-US"},
		{"", "en-US"},
	}
	for _, tt := range tests {
		got := localeToLanguageCode(tt.locale)
		if got != tt.want {
			t.Errorf("localeToLanguageCode(%q) = %q, want %q", tt.locale, got, tt.want)
		}
	}
}

func TestVoiceForLocale(t *testing.T) {
	g := NewGoogle(GoogleOpts{APIKey: "test", Voice: "en-US-Neural2-J"})
	tests := []struct {
		locale string
		want   string
	}{
		{"en-US", "en-US-Neural2-J"}, // matches configured voice prefix
		{"de-DE", "de-DE-Neural2-B"}, // fallback German voice
		{"fr-FR", "fr-FR-Neural2-B"}, // fallback French voice
	}
	for _, tt := range tests {
		got := g.voiceForLocale(tt.locale)
		if got != tt.want {
			t.Errorf("voiceForLocale(%q) = %q, want %q", tt.locale, got, tt.want)
		}
	}
}

func TestNewGoogle(t *testing.T) {
	g := NewGoogle(GoogleOpts{APIKey: "key"})
	if g.Name() != "google" {
		t.Errorf("expected 'google', got %q", g.Name())
	}

	g2 := NewGoogle(GoogleOpts{APIKey: "key", Voice: "custom-voice"})
	if g2.voice != "custom-voice" {
		t.Errorf("expected custom voice, got %q", g2.voice)
	}
}

func TestRouterSetProviders(t *testing.T) {
	r := NewRouter(StrategyCloudFirst)
	_, err := r.Synthesize(context.Background(), "test", SynthesizeOpts{})
	if err == nil {
		t.Fatal("expected error with no providers")
	}

	r.SetProviders(&mockProvider{name: "openai"})
	result, err := r.Synthesize(context.Background(), "test", SynthesizeOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Provider != "openai" {
		t.Errorf("expected openai, got %s", result.Provider)
	}
}

func TestRouterDefaultStrategy(t *testing.T) {
	r := NewRouter("", &mockProvider{name: "openai"})
	if r.strategy != StrategyCloudFirst {
		t.Errorf("expected cloud-first default, got %s", r.strategy)
	}
}

// roundTripFunc adapts a function to http.RoundTripper for test URL rewriting.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
