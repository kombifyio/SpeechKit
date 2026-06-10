package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/speakercontract"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

func newTestDeepgramProvider(serverURL string) *DeepgramProvider {
	p := NewDeepgramProvider("deepgram-test-key", "nova-3")
	p.BaseURL = serverURL
	p.Validation = testValidation
	p.client.Timeout = 5 * time.Second
	return p
}

func TestDeepgram_Transcribe_Diarization(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Token deepgram-test-key" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		gotQuery = r.URL.RawQuery
		speaker0 := 0
		speaker1 := 1
		resp := deepgramResponse{
			Results: deepgramResults{
				Channels: []deepgramChannel{
					{
						DetectedLanguage: "de",
						Alternatives: []deepgramAlternative{
							{
								Transcript: "Hallo Welt",
								Confidence: 0.91,
								Words: []deepgramWord{
									{Word: "Hallo", Start: 0, End: 0.4, Confidence: 0.95, Speaker: &speaker0, SpeakerConfidence: 0.8},
									{Word: "Welt", Start: 0.5, End: 0.9, Confidence: 0.93, Speaker: &speaker1, SpeakerConfidence: 0.82},
								},
							},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestDeepgramProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("pcm"), TranscribeOpts{
		Language: "de",
		Speaker:  speaker.Options{Diarization: true},
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if !strings.Contains(gotQuery, "diarize_model=latest") {
		t.Fatalf("query = %q, want diarize_model=latest", gotQuery)
	}
	if result.Text != "Hallo Welt" {
		t.Fatalf("text = %q", result.Text)
	}
	if result.Speakers == nil || len(result.Speakers.Segments) != 2 {
		t.Fatalf("speakers = %+v", result.Speakers)
	}
	if result.Speakers.Segments[1].SpeakerLabel != "speaker_1" {
		t.Fatalf("second speaker label = %q", result.Speakers.Segments[1].SpeakerLabel)
	}
	speakercontract.AssertDiarizationResult(t, result.Speakers)
}

func TestDeepgram_Transcribe_TextOnlyDoesNotRequestDiarization(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		resp := deepgramResponse{
			Results: deepgramResults{
				Channels: []deepgramChannel{
					{Alternatives: []deepgramAlternative{{Transcript: "plain", Confidence: 0.7}}},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestDeepgramProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("pcm"), TranscribeOpts{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if strings.Contains(gotQuery, "diarize") {
		t.Fatalf("query = %q, should not request diarization", gotQuery)
	}
	if result.Speakers != nil {
		t.Fatalf("speakers = %+v, want nil", result.Speakers)
	}
}

func TestDeepgram_Transcribe_AppliesListenOptionsAndKeyterms(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		resp := deepgramResponse{
			Results: deepgramResults{
				Channels: []deepgramChannel{
					{Alternatives: []deepgramAlternative{{Transcript: "hello Kombify", Confidence: 0.7}}},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestDeepgramProvider(server.URL)
	p.ApplyOptions(DeepgramOptions{
		Configured:            true,
		SmartFormat:           true,
		Dictation:             true,
		FillerWords:           true,
		Numerals:              true,
		LanguageOverride:      "multi",
		UseVocabularyKeyterms: true,
		Keyterms:              []string{"Kombify"},
	})
	_, err := p.Transcribe(context.Background(), []byte("pcm"), TranscribeOpts{
		Language: "de",
		Keyterms: []string{"SpeechKit", "kombify"},
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	for _, key := range []string{"smart_format", "dictation", "filler_words", "numerals"} {
		if gotQuery.Get(key) != "true" {
			t.Fatalf("%s = %q, want true (query=%s)", key, gotQuery.Get(key), gotQuery.Encode())
		}
	}
	if gotQuery.Get("language") != "de" {
		t.Fatalf("language = %q, want request override de", gotQuery.Get("language"))
	}
	if got := gotQuery["keyterm"]; len(got) != 2 || got[0] != "Kombify" || got[1] != "SpeechKit" {
		t.Fatalf("keyterm = %v, want [Kombify SpeechKit]", got)
	}
	if gotQuery.Has("keywords") {
		t.Fatalf("query used legacy keywords for nova-3: %s", gotQuery.Encode())
	}
}

func TestDeepgram_Transcribe_DetectLanguageSkipsLanguage(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		resp := deepgramResponse{
			Results: deepgramResults{
				Channels: []deepgramChannel{
					{Alternatives: []deepgramAlternative{{Transcript: "hello", Confidence: 0.7}}},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestDeepgramProvider(server.URL)
	p.ApplyOptions(DeepgramOptions{
		Configured:            true,
		SmartFormat:           true,
		DetectLanguage:        true,
		LanguageOverride:      "multi",
		UseVocabularyKeyterms: true,
	})
	_, err := p.Transcribe(context.Background(), []byte("pcm"), TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotQuery.Get("detect_language") != "true" {
		t.Fatalf("detect_language = %q, want true", gotQuery.Get("detect_language"))
	}
	if gotQuery.Has("language") {
		t.Fatalf("language should be omitted when detect_language=true: %s", gotQuery.Encode())
	}
}

func TestDeepgram_Transcribe_DefaultsToMultilingualCodeSwitching(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		resp := deepgramResponse{
			Results: deepgramResults{
				Channels: []deepgramChannel{
					{Alternatives: []deepgramAlternative{{Transcript: "Gemma E2B und LiteRT", Confidence: 0.8}}},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestDeepgramProvider(server.URL)
	p.ApplyOptions(DeepgramOptions{
		Configured:            true,
		SmartFormat:           true,
		LanguageOverride:      "multi",
		UseVocabularyKeyterms: true,
	})
	_, err := p.Transcribe(context.Background(), []byte("pcm"), TranscribeOpts{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotQuery.Get("language") != "multi" {
		t.Fatalf("language = %q, want multi (query=%s)", gotQuery.Get("language"), gotQuery.Encode())
	}
	if gotQuery.Has("detect_language") {
		t.Fatalf("language=multi should not also set detect_language: %s", gotQuery.Encode())
	}
}

func TestDeepgram_Transcribe_UsesLegacyKeywordsForNova2(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		resp := deepgramResponse{
			Results: deepgramResults{
				Channels: []deepgramChannel{
					{Alternatives: []deepgramAlternative{{Transcript: "hello", Confidence: 0.7}}},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewDeepgramProvider("deepgram-test-key", "nova-2")
	p.BaseURL = server.URL
	p.Validation = testValidation
	p.client.Timeout = 5 * time.Second
	_, err := p.Transcribe(context.Background(), []byte("pcm"), TranscribeOpts{Keyterms: []string{"AcmeOS"}})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got := gotQuery["keywords"]; len(got) != 1 || got[0] != "AcmeOS" {
		t.Fatalf("keywords = %v, want [AcmeOS]", got)
	}
	if gotQuery.Has("keyterm") {
		t.Fatalf("query used nova-3 keyterm for nova-2: %s", gotQuery.Encode())
	}
}

func TestDeepgram_Transcribe_ProviderErrorIsRedacted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key sk-secret"}}`))
	}))
	defer server.Close()

	p := newTestDeepgramProvider(server.URL)
	_, err := p.Transcribe(context.Background(), []byte("pcm"), TranscribeOpts{
		Language: "de",
		Speaker:  speaker.Options{Diarization: true},
	})
	speakercontract.AssertProviderError(t, err, "deepgram")
}
