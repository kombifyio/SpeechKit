package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

func newTestGoogleProvider(serverURL string) *Provider {
	p := New("test-api-key", "latest_long")
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
	result, err := p.Transcribe(context.Background(), []byte("test-audio"), stt.TranscribeOpts{Language: "de"})
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
	result, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{Language: "de"})
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
	result, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{
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
	result, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{Language: "de"})
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
	result, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{Model: "chirp_2"})
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
		// Multilanguage resolves to English as the primary language rather
		// than to an empty field. RecognitionConfig.languageCode is REQUIRED,
		// so the old expectation here encoded an invalid request: Google is
		// the one provider that cannot be told "no language".
		{"auto", googleEnglishPrimary},
		{"", googleEnglishPrimary},
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
			_, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{Language: tt.input})
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			if gotLang != tt.want {
				t.Errorf("language = %q, want %q", gotLang, tt.want)
			}
		})
	}
}

// Owner decision 2026-08-11: English is Google's standing primary language and
// a configured user language rides along as an alternative, because Google
// cannot express unconstrained multilanguage — v1 requires languageCode and
// caps alternativeLanguageCodes at three additional BCP-47 tags.
func TestGoogle_MultilanguageCarriesEnglishPlusUserLanguage(t *testing.T) {
	tests := []struct {
		name     string
		language string
		wantLang string
		wantAlts []string
	}{
		{"unset falls back to English alone", "", googleEnglishPrimary, nil},
		{"multi sentinel behaves like unset", stt.LanguageMulti, googleEnglishPrimary, nil},
		{"explicit pin stays primary, English rides along", "de", "de-DE", []string{googleEnglishPrimary}},
		{"an English pin needs no alternative", "en", googleEnglishPrimary, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotLang string
			var gotAlts []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var reqBody googleRecognizeRequest
				json.NewDecoder(r.Body).Decode(&reqBody) //nolint:errcheck // test double
				gotLang = reqBody.Config.LanguageCode
				gotAlts = reqBody.Config.AlternativeLanguageCodes
				json.NewEncoder(w).Encode(googleRecognizeResponse{}) //nolint:errcheck // test double
			}))
			defer server.Close()

			p := newTestGoogleProvider(server.URL)
			if _, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{Language: tt.language}); err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			if gotLang == "" {
				t.Fatal("languageCode is empty, but Google requires it")
			}
			if gotLang != tt.wantLang {
				t.Errorf("languageCode = %q, want %q", gotLang, tt.wantLang)
			}
			if len(gotAlts) != len(tt.wantAlts) {
				t.Fatalf("alternativeLanguageCodes = %v, want %v", gotAlts, tt.wantAlts)
			}
			for i := range tt.wantAlts {
				if gotAlts[i] != tt.wantAlts[i] {
					t.Errorf("alternativeLanguageCodes[%d] = %q, want %q", i, gotAlts[i], tt.wantAlts[i])
				}
			}
			// Google caps the list at three additional tags.
			if len(gotAlts) > 3 {
				t.Errorf("alternativeLanguageCodes has %d entries, Google allows at most 3", len(gotAlts))
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
	_, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{})
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
	_, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{})
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
	result, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	// With no language pinned Google is asked in English, and the result
	// reports what was actually recognised rather than a fabricated "de".
	if result.Language != googleEnglishPrimary {
		t.Errorf("default language = %q, want %q", result.Language, googleEnglishPrimary)
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
	_, err := p.Transcribe(context.Background(), audioData, stt.TranscribeOpts{})
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
	p := New("key", "latest_long")
	p.BaseURL = "http://127.0.0.1:1"
	p.Validation = testValidation
	p.client.Timeout = 100 * time.Millisecond
	err := p.Health(context.Background())
	if err == nil {
		t.Error("expected error for unreachable host")
	}
}

func TestGoogle_Name(t *testing.T) {
	p := New("key", "")
	if p.Name() != "google" {
		t.Errorf("Name() = %q", p.Name())
	}
}

func TestGoogle_DefaultModel(t *testing.T) {
	p := New("key", "")
	if p.Model != "latest_long" {
		t.Errorf("Model = %q, want %q", p.Model, "latest_long")
	}
}

func TestGoogle_CustomModel(t *testing.T) {
	p := New("key", "chirp_2")
	if p.Model != "chirp_2" {
		t.Errorf("Model = %q, want %q", p.Model, "chirp_2")
	}
}

func TestGoogle_ImplementsSTTProvider(t *testing.T) {
	var _ stt.STTProvider = (*Provider)(nil)
}

func TestGoogle_StartSpeakerStreamRequiresV2Credentials(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("SPEECHKIT_GOOGLE_STT_CREDENTIALS_JSON", "")
	p := New("api-key", "latest_long")
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
