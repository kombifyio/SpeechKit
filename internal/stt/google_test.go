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

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

func newTestGoogleProvider(serverURL string) *GoogleSTTProvider {
	p := NewGoogleSTTProvider("test-api-key", "latest_long")
	p.BaseURL = serverURL
	p.Validation = testValidation
	p.client.Timeout = 5 * time.Second
	return p
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
			Results: []googleSpeechRecognitionResult{
				{
					Alternatives: []googleSpeechAlternative{
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
	if result.Duration < 0 {
		t.Error("duration should not be negative")
	}
}

func TestGoogle_Transcribe_MultipleResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := googleRecognizeResponse{
			Results: []googleSpeechRecognitionResult{
				{
					Alternatives: []googleSpeechAlternative{
						{Transcript: "Hallo ", Confidence: 0.9},
					},
				},
				{
					Alternatives: []googleSpeechAlternative{
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

func TestGoogle_Transcribe_Diarization(t *testing.T) {
	var gotConfig googleRecognitionConfig
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody googleRecognizeRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotConfig = reqBody.Config
		resp := googleRecognizeResponse{
			Results: []googleSpeechRecognitionResult{
				{
					Alternatives: []googleSpeechAlternative{
						{Transcript: "Hallo Welt", Confidence: 0.9},
					},
				},
				{
					Alternatives: []googleSpeechAlternative{
						{
							Transcript: "Hallo Welt",
							Confidence: 0.9,
							Words: []googleWordInfo{
								{Word: "Hallo", StartTime: "0s", EndTime: "0.500s", SpeakerTag: 1},
								{Word: "Welt", StartTime: "0.600s", EndTime: "1s", SpeakerTag: 2},
							},
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestGoogleProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{
		Language: "de",
		Speaker:  speaker.Options{Diarization: true, MinSpeakersExpected: 2, MaxSpeakersExpected: 2},
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotConfig.DiarizationConfig == nil || !gotConfig.DiarizationConfig.EnableSpeakerDiarization {
		t.Fatalf("diarization config = %+v", gotConfig.DiarizationConfig)
	}
	if !gotConfig.EnableWordTimeOffsets {
		t.Fatal("word time offsets should be enabled when diarization is requested")
	}
	if result.Speakers == nil {
		t.Fatal("expected diarization result")
	}
	if len(result.Speakers.Segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(result.Speakers.Segments))
	}
	if result.Speakers.Segments[0].SpeakerLabel != "speaker_1" {
		t.Fatalf("first speaker label = %q", result.Speakers.Segments[0].SpeakerLabel)
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
	if result.Duration < 0 {
		t.Error("duration should not be negative")
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
	p := NewGoogleSTTProvider("key", "latest_long")
	p.BaseURL = "http://127.0.0.1:1"
	p.Validation = testValidation
	p.client.Timeout = 100 * time.Millisecond
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
	if p.Model != "latest_long" {
		t.Errorf("Model = %q, want %q", p.Model, "latest_long")
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

func TestGoogle_StartSpeakerStreamRequiresV2Credentials(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("SPEECHKIT_GOOGLE_STT_CREDENTIALS_JSON", "")
	p := NewGoogleSTTProvider("api-key", "latest_long")
	_, err := p.StartSpeakerStream(context.Background(), speaker.Options{Diarization: true}, speaker.AudioFormat{})
	if err == nil {
		t.Fatal("expected credentials error")
	}
	if !strings.Contains(err.Error(), "GOOGLE_APPLICATION_CREDENTIALS") ||
		!strings.Contains(err.Error(), "SPEECHKIT_GOOGLE_STT_CREDENTIALS_JSON") ||
		!strings.Contains(err.Error(), "batch REST only") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGoogleStreamingCredentialsPresentAcceptsADCEnv(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "C:/creds/google.json")
	if !googleStreamingCredentialsPresent("GOOGLE_APPLICATION_CREDENTIALS", "SPEECHKIT_GOOGLE_STT_CREDENTIALS_JSON") {
		t.Fatal("expected GOOGLE_APPLICATION_CREDENTIALS to enable streaming credential detection")
	}
}
