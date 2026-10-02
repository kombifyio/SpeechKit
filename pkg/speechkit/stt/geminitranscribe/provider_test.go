package geminitranscribe

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// testValidation permits httptest.Server URLs; production keeps the strict
// netsec defaults (public https only).
var testValidation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}

type capturedRequest struct {
	Path   string
	APIKey string
	Body   struct {
		Model string `json:"model"`
		Input []struct {
			Type    string `json:"type"`
			Content []struct {
				Type     string `json:"type"`
				Data     string `json:"data"`
				MimeType string `json:"mime_type"`
			} `json:"content"`
		} `json:"input"`
		GenerationConfig struct {
			TranscriptionConfig map[string]json.RawMessage `json:"transcription_config"`
		} `json:"generation_config"`
	}
}

func newTestProvider(t *testing.T, got *capturedRequest) *Provider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Path = r.URL.Path
		got.APIKey = r.Header.Get("x-goog-api-key")
		if err := json.NewDecoder(r.Body).Decode(&got.Body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "completed",
			"steps": []any{
				map[string]any{"type": "user_input", "content": []any{map[string]any{"type": "audio"}}},
				map[string]any{"type": "model_output", "content": []any{map[string]any{"type": "text", "text": "Termin am Mittwoch"}}},
			},
		})
	}))
	t.Cleanup(server.Close)
	p := New(Options{APIKey: "gemini-key"})
	p.BaseURL = server.URL
	p.Validation = testValidation
	return p
}

func TestTranscribeSendsInlineAudioWithLanguageAndVocabulary(t *testing.T) {
	var got capturedRequest
	p := newTestProvider(t, &got)

	result, err := p.Transcribe(context.Background(), []byte("pcm-data"), stt.TranscribeOpts{
		Language: "de",
		Keyterms: []string{"Kombify"},
		Options:  provideropts.Values{provideropts.OptionLanguageHints: []string{"en"}},
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	if got.Path != "/v1beta/interactions" || got.APIKey != "gemini-key" {
		t.Fatalf("request = %s with key %q, want /v1beta/interactions with the API key", got.Path, got.APIKey)
	}
	if got.Body.Model != DefaultModel {
		t.Fatalf("model = %q, want %q", got.Body.Model, DefaultModel)
	}
	if len(got.Body.Input) != 1 || len(got.Body.Input[0].Content) != 1 {
		t.Fatalf("input = %+v, want one user_input step with one audio block", got.Body.Input)
	}
	audio := got.Body.Input[0].Content[0]
	wav, err := base64.StdEncoding.DecodeString(audio.Data)
	if err != nil || audio.Type != "audio" || audio.MimeType != "audio/wav" || !stt.IsWAV(wav) {
		t.Fatalf("audio block = type %q mime %q (decode err %v), want inline base64 WAV", audio.Type, audio.MimeType, err)
	}
	var codes, vocabulary []string
	config := got.Body.GenerationConfig.TranscriptionConfig
	_ = json.Unmarshal(config["language_codes"], &codes)
	_ = json.Unmarshal(config["custom_vocabulary"], &vocabulary)
	if !slices.Equal(codes, []string{"de-DE", "en-US"}) {
		t.Fatalf("language_codes = %v, want [de-DE en-US]", codes)
	}
	if !slices.Equal(vocabulary, []string{"Kombify"}) {
		t.Fatalf("custom_vocabulary = %v, want [Kombify]", vocabulary)
	}
	if result.Text != "Termin am Mittwoch" {
		t.Fatalf("text = %q, want the model_output transcript", result.Text)
	}
}

// Pinning a language makes the model drop speech in any other language, so a
// multilanguage session must leave language_codes off and let Gemini detect.
func TestTranscribeMultilanguageSendsNoLanguageCodes(t *testing.T) {
	var got capturedRequest
	p := newTestProvider(t, &got)

	result, err := p.Transcribe(context.Background(), []byte("pcm-data"), stt.TranscribeOpts{Language: stt.LanguageMulti})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if raw, ok := got.Body.GenerationConfig.TranscriptionConfig["language_codes"]; ok {
		t.Fatalf("language_codes = %s, want omitted for multilanguage", raw)
	}
	if result.Language != stt.LanguageMulti {
		t.Fatalf("result language = %q, want %q", result.Language, stt.LanguageMulti)
	}
}

type otherProvider struct{ stt.STTProvider }

func (otherProvider) Name() string { return "deepgram" }

func TestSelectedGeminiTranscribeProfileRoutesToGemini(t *testing.T) {
	gemini := New(Options{APIKey: "key"})
	candidates := []stt.STTProvider{otherProvider{}, gemini}

	got := stt.PrioritizeProviderProfile(candidates, "stt.google.gemini-3.5-transcribe")
	if got[0] != stt.STTProvider(gemini) {
		t.Fatalf("selected Gemini Transcribe profile routed to %q first, want Gemini Transcribe", got[0].Name())
	}
}
