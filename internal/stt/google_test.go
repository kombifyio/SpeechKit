package stt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestGoogleProvider(serverURL string) *GoogleSTTProvider {
	return &GoogleSTTProvider{
		APIKey:  "test-api-key",
		Model:   "latest_long",
		BaseURL: serverURL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

func TestGoogle_Transcribe_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/v1/speech:recognize") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-api-key" {
			t.Errorf("expected key=test-api-key, got %q", r.URL.Query().Get("key"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}

		var reqBody googleRecognizeRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		// Verify audio is base64-encoded.
		decoded, err := base64.StdEncoding.DecodeString(reqBody.Audio.Content)
		if err != nil {
			t.Fatalf("decode base64: %v", err)
		}
		if string(decoded) != "test-audio" {
			t.Errorf("audio = %q", string(decoded))
		}

		if reqBody.Config.Model != "latest_long" {
			t.Errorf("model = %q", reqBody.Config.Model)
		}

		resp := googleRecognizeResponse{
			Results: []struct {
				Alternatives []struct {
					Transcript string  `json:"transcript"`
					Confidence float64 `json:"confidence"`
				} `json:"alternatives"`
			}{
				{
					Alternatives: []struct {
						Transcript string  `json:"transcript"`
						Confidence float64 `json:"confidence"`
					}{
						{Transcript: "Hallo Welt", Confidence: 0.95},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestGoogleProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("test-audio"), TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "Hallo Welt" {
		t.Errorf("text = %q, want %q", result.Text, "Hallo Welt")
	}
	if result.Provider != "google" {
		t.Errorf("provider = %q", result.Provider)
	}
	if result.Confidence != 0.95 {
		t.Errorf("confidence = %f, want 0.95", result.Confidence)
	}
	if result.Duration == 0 {
		t.Error("duration should be > 0")
	}
}

func TestGoogle_Transcribe_MultipleResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := googleRecognizeResponse{
			Results: []struct {
				Alternatives []struct {
					Transcript string  `json:"transcript"`
					Confidence float64 `json:"confidence"`
				} `json:"alternatives"`
			}{
				{
					Alternatives: []struct {
						Transcript string  `json:"transcript"`
						Confidence float64 `json:"confidence"`
					}{
						{Transcript: "Hallo ", Confidence: 0.9},
					},
				},
				{
					Alternatives: []struct {
						Transcript string  `json:"transcript"`
						Confidence float64 `json:"confidence"`
					}{
						{Transcript: "Welt", Confidence: 0.85},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestGoogleProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "Hallo Welt" {
		t.Errorf("text = %q, want %q", result.Text, "Hallo Welt")
	}
}

func TestGoogle_Transcribe_EmptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(googleRecognizeResponse{})
	}))
	defer server.Close()

	p := newTestGoogleProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "" {
		t.Errorf("expected empty text, got %q", result.Text)
	}
}

func TestGoogle_Transcribe_ModelOverride(t *testing.T) {
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody googleRecognizeRequest
		json.NewDecoder(r.Body).Decode(&reqBody)
		gotModel = reqBody.Config.Model
		json.NewEncoder(w).Encode(googleRecognizeResponse{})
	}))
	defer server.Close()

	p := newTestGoogleProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{Model: "chirp_2"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotModel != "chirp_2" {
		t.Errorf("sent model = %q, want %q", gotModel, "chirp_2")
	}
	if result.Model != "chirp_2" {
		t.Errorf("result model = %q, want %q", result.Model, "chirp_2")
	}
}

func TestGoogle_LanguageMapping(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"de", "de-DE"},
		{"en", "en-US"},
		{"fr", "fr-FR"},
		{"es", "es-ES"},
		{"it", "it-IT"},
		{"auto", ""},
		{"", ""},
		{"pt-BR", "pt-BR"}, // passthrough for full BCP-47 codes
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var gotLang string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var reqBody googleRecognizeRequest
				json.NewDecoder(r.Body).Decode(&reqBody)
				gotLang = reqBody.Config.LanguageCode
				json.NewEncoder(w).Encode(googleRecognizeResponse{})
			}))
			defer server.Close()

			p := newTestGoogleProvider(server.URL)
			_, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{Language: tt.input})
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			if gotLang != tt.want {
				t.Errorf("language = %q, want %q", gotLang, tt.want)
			}
		})
	}
}

func TestGoogle_Transcribe_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"error":{"message":"API key not valid"}}`))
	}))
	defer server.Close()

	p := newTestGoogleProvider(server.URL)
	_, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 in error: %v", err)
	}
	if !strings.Contains(err.Error(), "google") {
		t.Errorf("expected 'google' in error: %v", err)
	}
}

func TestGoogle_Transcribe_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	p := newTestGoogleProvider(server.URL)
	_, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGoogle_Transcribe_DefaultLanguage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(googleRecognizeResponse{})
	}))
	defer server.Close()

	p := newTestGoogleProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Language != "de" {
		t.Errorf("default language = %q, want %q", result.Language, "de")
	}
}

func TestGoogle_Transcribe_Base64Audio(t *testing.T) {
	audioData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}
	var gotContent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody googleRecognizeRequest
		json.NewDecoder(r.Body).Decode(&reqBody)
		gotContent = reqBody.Audio.Content
		json.NewEncoder(w).Encode(googleRecognizeResponse{})
	}))
	defer server.Close()

	p := newTestGoogleProvider(server.URL)
	_, err := p.Transcribe(context.Background(), audioData, TranscribeOpts{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	expected := base64.StdEncoding.EncodeToString(audioData)
	if gotContent != expected {
		t.Errorf("base64 content = %q, want %q", gotContent, expected)
	}
}

func TestGoogle_Health_OK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/v1/operations") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-api-key" {
			t.Errorf("key = %q", r.URL.Query().Get("key"))
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	p := newTestGoogleProvider(server.URL)
	if err := p.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
}

func TestGoogle_Health_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer server.Close()

	p := newTestGoogleProvider(server.URL)
	err := p.Health(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "google") {
		t.Errorf("expected 'google' in error: %v", err)
	}
}

func TestGoogle_Health_Unreachable(t *testing.T) {
	p := &GoogleSTTProvider{
		APIKey:  "key",
		Model:   "latest_long",
		BaseURL: "http://127.0.0.1:1",
		client:  &http.Client{Timeout: 100 * time.Millisecond},
	}
	err := p.Health(context.Background())
	if err == nil {
		t.Error("expected error for unreachable host")
	}
}

func TestGoogle_Name(t *testing.T) {
	p := NewGoogleSTTProvider("key", "")
	if p.Name() != "google" {
		t.Errorf("Name() = %q", p.Name())
	}
}

func TestGoogle_DefaultModel(t *testing.T) {
	p := NewGoogleSTTProvider("key", "")
	if p.Model != "chirp_3" {
		t.Errorf("Model = %q, want %q", p.Model, "chirp_3")
	}
}

func TestGoogle_CustomModel(t *testing.T) {
	p := NewGoogleSTTProvider("key", "chirp_2")
	if p.Model != "chirp_2" {
		t.Errorf("Model = %q, want %q", p.Model, "chirp_2")
	}
}

func TestGoogle_ImplementsSTTProvider(t *testing.T) {
	var _ STTProvider = (*GoogleSTTProvider)(nil)
}
