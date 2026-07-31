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

	"github.com/kombifyio/SpeechKit/pkg/speechkit/internal/speakercontract"
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

// TestDeepgram_Transcribe_PopulatesWordConfidence guards that plain dictation
// (no diarization) now carries the per-word acoustic confidence Deepgram
// returns, which bestTranscript() used to discard. This is the data that lets
// the host flag likely-misrecognized words (e.g. "Ultracord" for "Ultracode").
func TestDeepgram_Transcribe_PopulatesWordConfidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := deepgramResponse{
			Results: deepgramResults{
				Channels: []deepgramChannel{
					{
						DetectedLanguage: "de",
						Alternatives: []deepgramAlternative{
							{
								Transcript: "Ich nutze Ultracord",
								Confidence: 0.9,
								Words: []deepgramWord{
									{Word: "ich", PunctuatedWord: "Ich", Start: 0, End: 0.3, Confidence: 0.99},
									{Word: "nutze", PunctuatedWord: "nutze", Start: 0.3, End: 0.7, Confidence: 0.98},
									{Word: "ultracord", PunctuatedWord: "Ultracord", Start: 0.7, End: 1.4, Confidence: 0.41},
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
	result, err := p.Transcribe(context.Background(), []byte("pcm"), TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if len(result.Words) != 3 {
		t.Fatalf("Words = %#v, want 3 entries", result.Words)
	}
	// PunctuatedWord is preferred over the lowercase Word form.
	if result.Words[2].Text != "Ultracord" || result.Words[2].Confidence != 0.41 {
		t.Fatalf("third word = %+v, want {Text:Ultracord Confidence:0.41}", result.Words[2])
	}
	if result.Words[2].StartMs != 700 || result.Words[2].EndMs != 1400 {
		t.Fatalf("third word timing = %d..%d ms, want 700..1400", result.Words[2].StartMs, result.Words[2].EndMs)
	}
	if result.Speakers != nil {
		t.Fatalf("diarization was not requested; Speakers must be nil, got %+v", result.Speakers)
	}
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
	// The request language outranks the provider-level override, per the
	// standard option layering. Deepgram must receive it verbatim — a
	// configured language that silently became "multi" is what made the
	// language setting a no-op for batch transcription.
	if gotQuery.Get("language") != "de" {
		t.Fatalf("language = %q, want the configured request language (query=%s)", gotQuery.Get("language"), gotQuery.Encode())
	}
	if got := gotQuery["keyterm"]; len(got) != 2 || got[0] != "Kombify" || got[1] != "SpeechKit" {
		t.Fatalf("keyterm = %v, want [Kombify SpeechKit]", got)
	}
	if gotQuery.Has("keywords") {
		t.Fatalf("query used legacy keywords for nova-3: %s", gotQuery.Encode())
	}
}

func TestDeepgram_Transcribe_DetectLanguageIsIgnoredForCodeSwitching(t *testing.T) {
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
	if gotQuery.Has("detect_language") {
		t.Fatalf("detect_language should be ignored for Deepgram code-switching mode: %s", gotQuery.Encode())
	}
	if gotQuery.Get("language") != "de" {
		t.Fatalf("language = %q, want the configured request language (query=%s)", gotQuery.Get("language"), gotQuery.Encode())
	}
}

// A provider-level language override must reach the API when no per-request
// language is set, instead of being replaced by the code-switching default.
func TestDeepgram_Transcribe_HonoursProviderLanguageOverride(t *testing.T) {
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
	p.ApplyOptions(DeepgramOptions{Configured: true, SmartFormat: true, LanguageOverride: "de"})
	if _, err := p.Transcribe(context.Background(), []byte("pcm"), TranscribeOpts{}); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotQuery.Get("language") != "de" {
		t.Fatalf("language = %q, want the provider override (query=%s)", gotQuery.Get("language"), gotQuery.Encode())
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
