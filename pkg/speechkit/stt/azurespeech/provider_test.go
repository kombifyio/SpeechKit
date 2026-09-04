package azurespeech

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/sttcontract"
)

// testValidation permits httptest.Server URLs (http://127.0.0.1:RAND). The
// production constructor keeps the strict netsec default (public https only).
var testValidation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}

// successBody is the verified fast-transcription response shape with two
// speakers, phrase confidences and word timings on the first phrase.
const successBody = `{
  "durationMilliseconds": 2400,
  "combinedPhrases": [{"channel": 0, "text": "Hallo Welt. Guten Tag."}],
  "phrases": [
    {"channel": 0, "speaker": 1, "offsetMilliseconds": 0, "durationMilliseconds": 1000,
     "text": "Hallo Welt.", "locale": "de", "confidence": 0.9,
     "words": [
       {"text": "Hallo", "offsetMilliseconds": 0, "durationMilliseconds": 400},
       {"text": "Welt.", "offsetMilliseconds": 400, "durationMilliseconds": 600}
     ]},
    {"channel": 0, "speaker": 2, "offsetMilliseconds": 1200, "durationMilliseconds": 1200,
     "text": "Guten Tag.", "locale": "de", "confidence": 0.7}
  ]
}`

// capturedRequest is what the fake service saw for the last call.
type capturedRequest struct {
	Method     string
	Path       string
	Query      map[string]string
	Header     http.Header
	Audio      []byte
	Definition map[string]any
	Calls      atomic.Int32
}

// fakeService answers every request with status/body and records the last
// request, decoding the multipart transcription payload when present.
func fakeService(t *testing.T, status int, body string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.Calls.Add(1)
		captured.Method = r.Method
		captured.Path = r.URL.Path
		captured.Query = map[string]string{}
		for key := range r.URL.Query() {
			captured.Query[key] = r.URL.Query().Get(key)
		}
		captured.Header = r.Header.Clone()
		captured.Audio = nil
		captured.Definition = nil
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			if err := r.ParseMultipartForm(4 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
			} else {
				if file, _, err := r.FormFile("audio"); err == nil {
					captured.Audio, _ = io.ReadAll(file)
					_ = file.Close()
				}
				if def := r.FormValue("definition"); def != "" {
					if err := json.Unmarshal([]byte(def), &captured.Definition); err != nil {
						t.Errorf("definition is not JSON: %v", err)
					}
				}
			}
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, captured
}

func newTestProvider(server *httptest.Server, opts Options) *Provider {
	opts.Host = server.URL
	p := New(opts)
	p.Validation = testValidation
	return p
}

// lookup walks a decoded JSON object; ok is false when any step is missing.
func lookup(m map[string]any, path ...string) (any, bool) {
	var current any = m
	for _, key := range path {
		obj, isObj := current.(map[string]any)
		if !isObj {
			return nil, false
		}
		next, found := obj[key]
		if !found {
			return nil, false
		}
		current = next
	}
	return current, true
}

func stringList(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTranscribe_RequestShape(t *testing.T) {
	server, got := fakeService(t, http.StatusOK, successBody)
	p := newTestProvider(server, Options{APIKey: "resource-key", Style: "verbatim", Timestamps: "word"})

	_, err := p.Transcribe(context.Background(), []byte("raw-pcm-bytes"), stt.TranscribeOpts{
		Language: "de-DE",
		Keyterms: []string{" kombify ", "SpeechKit", "kombify", "  "},
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	if got.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", got.Method)
	}
	if got.Path != "/speechtotext/transcriptions:transcribe" {
		t.Errorf("path = %q", got.Path)
	}
	if got.Query["api-version"] != APIVersion {
		t.Errorf("api-version = %q, want %q", got.Query["api-version"], APIVersion)
	}
	if got.Header.Get("Ocp-Apim-Subscription-Key") != "resource-key" {
		t.Errorf("key header = %q", got.Header.Get("Ocp-Apim-Subscription-Key"))
	}
	if got.Header.Get("Authorization") != "" {
		t.Errorf("unexpected Authorization header %q on the key path", got.Header.Get("Authorization"))
	}
	if !stt.IsWAV(got.Audio) {
		t.Errorf("audio part is not a WAV container (raw PCM must be wrapped)")
	}

	if locales := stringList(mustLookup(t, got.Definition, "locales")); !equalStrings(locales, []string{"de"}) {
		t.Errorf("locales = %v, want [de]", locales)
	}
	if enabled, _ := mustLookup(t, got.Definition, "enhancedMode", "enabled").(bool); !enabled {
		t.Error("enhancedMode.enabled must be true")
	}
	if model := mustLookup(t, got.Definition, "enhancedMode", "model"); model != DefaultModel {
		t.Errorf("enhancedMode.model = %v, want %s", model, DefaultModel)
	}
	if style := mustLookup(t, got.Definition, "enhancedMode", "modelOptions", "transcribeStyle"); style != "verbatim" {
		t.Errorf("transcribeStyle = %v, want verbatim", style)
	}
	if ts := mustLookup(t, got.Definition, "enhancedMode", "modelOptions", "timestamps"); ts != "word" {
		t.Errorf("timestamps = %v, want word", ts)
	}
	if phrases := stringList(mustLookup(t, got.Definition, "phraseList", "phrases")); !equalStrings(phrases, []string{"kombify", "SpeechKit"}) {
		t.Errorf("phrases = %v, want trimmed and de-duplicated [kombify SpeechKit]", phrases)
	}
	if _, present := lookup(got.Definition, "diarization"); present {
		t.Error("diarization must be omitted when nobody asked for it")
	}
}

func mustLookup(t *testing.T, m map[string]any, path ...string) any {
	t.Helper()
	v, ok := lookup(m, path...)
	if !ok {
		t.Fatalf("definition is missing %s: %v", strings.Join(path, "."), m)
	}
	return v
}

func TestTranscribe_LocalesOmittedForAutoDetect(t *testing.T) {
	for _, language := range []string{"", "auto", "multi"} {
		t.Run("lang="+language, func(t *testing.T) {
			server, got := fakeService(t, http.StatusOK, successBody)
			p := newTestProvider(server, Options{APIKey: "k"})
			if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Language: language}); err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			if _, present := lookup(got.Definition, "locales"); present {
				t.Errorf("locales must be omitted for %q so the model auto-detects: %v", language, got.Definition["locales"])
			}
		})
	}
}

func TestTranscribe_LocaleFromGlobalOptions(t *testing.T) {
	// A language configured in Settings arrives through Options rather than
	// the per-request override and still has to narrow the request.
	server, got := fakeService(t, http.StatusOK, successBody)
	p := newTestProvider(server, Options{APIKey: "k"})
	opts := stt.TranscribeOpts{}
	opts.Options = provideropts.Values{provideropts.OptionLanguage: "pt-BR"}
	if _, err := p.Transcribe(context.Background(), []byte("pcm"), opts); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if locales := stringList(mustLookup(t, got.Definition, "locales")); !equalStrings(locales, []string{"pt"}) {
		t.Errorf("locales = %v, want [pt]", locales)
	}
}

func TestTranscribe_PhraseListCapped(t *testing.T) {
	server, got := fakeService(t, http.StatusOK, successBody)
	p := newTestProvider(server, Options{APIKey: "k", MaxPhrases: 2})
	_, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Keyterms: []string{"a", "b", "c"}})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if phrases := stringList(mustLookup(t, got.Definition, "phraseList", "phrases")); !equalStrings(phrases, []string{"a", "b"}) {
		t.Errorf("phrases = %v, want the first two", phrases)
	}
}

func TestTranscribe_DiarizationFlag(t *testing.T) {
	t.Run("per request enables segments", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, successBody)
		p := newTestProvider(server, Options{APIKey: "k"})
		_, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Speaker: speaker.Options{Diarization: true}})
		if err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
		if enabled, _ := mustLookup(t, got.Definition, "diarization", "enabled").(bool); !enabled {
			t.Error("diarization.enabled must be true when the request asks for speakers")
		}
		if ts := mustLookup(t, got.Definition, "enhancedMode", "modelOptions", "timestamps"); ts != "segment" {
			t.Errorf("timestamps = %v, want segment (speaker labels ride on segments)", ts)
		}
	})

	t.Run("provider default keeps word timestamps", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, successBody)
		p := newTestProvider(server, Options{APIKey: "k", Diarization: true, Timestamps: "word"})
		if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{}); err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
		if enabled, _ := mustLookup(t, got.Definition, "diarization", "enabled").(bool); !enabled {
			t.Error("diarization.enabled must be true from the provider default")
		}
		if ts := mustLookup(t, got.Definition, "enhancedMode", "modelOptions", "timestamps"); ts != "word" {
			t.Errorf("timestamps = %v, want the configured word granularity", ts)
		}
	})

	t.Run("none omits the field", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, successBody)
		p := newTestProvider(server, Options{APIKey: "k"})
		if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{}); err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
		if _, present := lookup(got.Definition, "enhancedMode", "modelOptions", "timestamps"); present {
			t.Error("timestamps must be omitted for the default none")
		}
	})
}

func TestTranscribe_ModelOverride(t *testing.T) {
	server, got := fakeService(t, http.StatusOK, successBody)
	p := newTestProvider(server, Options{APIKey: "k"})
	res, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Model: "MAI-Transcribe-1.5"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if model := mustLookup(t, got.Definition, "enhancedMode", "model"); model != "MAI-Transcribe-1.5" {
		t.Errorf("request model = %v", model)
	}
	if res.Model != "MAI-Transcribe-1.5" {
		t.Errorf("Result.Model = %q", res.Model)
	}
}

func TestTranscribe_Credentials(t *testing.T) {
	bearer := func(context.Context) (string, error) { return "entra-token", nil }

	t.Run("bearer only", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, successBody)
		p := newTestProvider(server, Options{BearerToken: bearer})
		if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{}); err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
		if got.Header.Get("Authorization") != "Bearer entra-token" {
			t.Errorf("Authorization = %q", got.Header.Get("Authorization"))
		}
		if got.Header.Get("Ocp-Apim-Subscription-Key") != "" {
			t.Error("no key header expected on the bearer path")
		}
	})

	t.Run("bearer wins over key", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, successBody)
		p := newTestProvider(server, Options{APIKey: "resource-key", BearerToken: bearer})
		if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{}); err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
		if got.Header.Get("Authorization") != "Bearer entra-token" {
			t.Errorf("Authorization = %q", got.Header.Get("Authorization"))
		}
		if got.Header.Get("Ocp-Apim-Subscription-Key") != "" {
			t.Error("the key must not be sent when a bearer token is available")
		}
	})

	t.Run("bearer error surfaces and nothing is sent", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, successBody)
		tokenErr := errors.New("az login required")
		p := newTestProvider(server, Options{APIKey: "resource-key", BearerToken: func(context.Context) (string, error) { return "", tokenErr }})
		_, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{})
		if !errors.Is(err, tokenErr) {
			t.Fatalf("error = %v, want the token error wrapped", err)
		}
		if got.Calls.Load() != 0 {
			t.Error("no request must leave the process when the token cannot be minted")
		}
	})

	t.Run("no credential", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, successBody)
		p := newTestProvider(server, Options{})
		if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{}); err == nil {
			t.Fatal("expected an error without key or bearer token")
		}
		if got.Calls.Load() != 0 {
			t.Error("no request expected without a credential")
		}
	})
}

func TestTranscribe_MapsResponse(t *testing.T) {
	server, _ := fakeService(t, http.StatusOK, successBody)
	p := newTestProvider(server, Options{APIKey: "k"})
	res, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "Hallo Welt. Guten Tag." {
		t.Errorf("Text = %q", res.Text)
	}
	if res.Language != "de" {
		t.Errorf("Language = %q, want the phrase locale", res.Language)
	}
	if res.Provider != "foundry" {
		t.Errorf("Provider = %q", res.Provider)
	}
	if res.Model != DefaultModel {
		t.Errorf("Model = %q", res.Model)
	}
	if math.Abs(res.Confidence-0.8) > 1e-9 {
		t.Errorf("Confidence = %v, want the mean 0.8", res.Confidence)
	}
	if res.Words != nil {
		t.Error("Words must stay nil: MAI reports no per-word confidence")
	}
	if res.Speakers != nil {
		t.Error("Speakers must stay nil when diarization was not requested")
	}
	if res.Duration <= 0 {
		t.Error("Duration must record the wall time")
	}
}

func TestTranscribe_MapsDiarization(t *testing.T) {
	server, _ := fakeService(t, http.StatusOK, successBody)
	p := newTestProvider(server, Options{APIKey: "k"})
	res, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Speaker: speaker.Options{Diarization: true}})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	d := res.Speakers
	if d == nil {
		t.Fatal("Speakers must be populated for a diarized response")
	}
	if d.Provider != "foundry" || d.Model != DefaultModel || d.Level != speaker.IdentificationDiarization {
		t.Errorf("diarization header = %+v", *d)
	}
	if d.Text != res.Text || d.Language != res.Language {
		t.Errorf("diarization text/language must mirror the result: %+v", *d)
	}
	if len(d.Segments) != 2 {
		t.Fatalf("segments = %d, want one per phrase", len(d.Segments))
	}
	first, second := d.Segments[0], d.Segments[1]
	if first.StartMs != 0 || first.EndMs != 1000 || first.Text != "Hallo Welt." {
		t.Errorf("first segment = %+v", first)
	}
	if second.StartMs != 1200 || second.EndMs != 2400 || second.Text != "Guten Tag." {
		t.Errorf("second segment = %+v", second)
	}
	if first.SpeakerLabel == "" || second.SpeakerLabel == "" || first.SpeakerLabel == second.SpeakerLabel {
		t.Errorf("segments must carry distinct speaker labels: %q vs %q", first.SpeakerLabel, second.SpeakerLabel)
	}
	if len(d.Speakers) != 2 || d.Speakers[0].Label != first.SpeakerLabel || d.Speakers[1].Label != second.SpeakerLabel {
		t.Errorf("speakers = %+v, want the two segment labels", d.Speakers)
	}
	if len(first.Words) != 2 || first.Words[1].StartMs != 400 || first.Words[1].EndMs != 1000 || first.Words[1].SpeakerLabel != first.SpeakerLabel {
		t.Errorf("first segment words = %+v", first.Words)
	}
	if len(d.Words) != 2 {
		t.Errorf("flattened words = %d, want 2", len(d.Words))
	}
}

func TestTranscribe_DiarizationWithoutSpeakersStaysNil(t *testing.T) {
	body := `{"combinedPhrases":[{"channel":0,"text":"Nur ich."}],"phrases":[{"channel":0,"offsetMilliseconds":0,"durationMilliseconds":500,"text":"Nur ich.","locale":"de"}]}`
	server, _ := fakeService(t, http.StatusOK, body)
	p := newTestProvider(server, Options{APIKey: "k", Diarization: true})
	res, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Speakers != nil {
		t.Errorf("Speakers = %+v, want nil when no phrase carries a speaker", res.Speakers)
	}
	if res.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0 when the service reported none", res.Confidence)
	}
}

func TestTranscribe_TextFallsBackToPhrases(t *testing.T) {
	body := `{"phrases":[{"text":"Erster Teil."},{"text":"Zweiter Teil."}]}`
	server, _ := fakeService(t, http.StatusOK, body)
	p := newTestProvider(server, Options{APIKey: "k"})
	res, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "Erster Teil. Zweiter Teil." {
		t.Errorf("Text = %q", res.Text)
	}
	if res.Language != "de" {
		t.Errorf("Language = %q, want the requested language when phrases carry none", res.Language)
	}
}

func TestTranscribe_BadRequestSurfacesServiceMessage(t *testing.T) {
	body := `{"code":"InvalidRequest","message":"Requested MAI transcription model 'MAI-Transcribe-9' is not supported."}`
	server, _ := fakeService(t, http.StatusBadRequest, body)
	p := newTestProvider(server, Options{APIKey: "k"})
	_, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Model: "MAI-Transcribe-9"})
	if err == nil {
		t.Fatal("expected an error for HTTP 400")
	}
	if !strings.Contains(err.Error(), "MAI-Transcribe-9") {
		t.Errorf("the service's explanation must reach the caller: %v", err)
	}
}

func TestTranscribe_AuthErrorDoesNotLeakBody(t *testing.T) {
	body := `{"error":{"code":"401","message":"Access denied due to invalid subscription key sk-secret-body"}}`
	server, _ := fakeService(t, http.StatusUnauthorized, body)
	p := newTestProvider(server, Options{APIKey: "k"})
	_, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{})
	if err == nil {
		t.Fatal("expected an error for HTTP 401")
	}
	if strings.Contains(err.Error(), "sk-secret-body") {
		t.Errorf("provider response body leaked in error: %v", err)
	}
}

func TestHealth(t *testing.T) {
	bearer := func(context.Context) (string, error) { return "entra-token", nil }

	t.Run("ok", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, `{"values":[]}`)
		p := newTestProvider(server, Options{APIKey: "k"})
		if err := p.Health(context.Background()); err != nil {
			t.Fatalf("Health: %v", err)
		}
		if got.Method != http.MethodGet || got.Path != "/speechtotext/models/base" {
			t.Errorf("health hit %s %s", got.Method, got.Path)
		}
		if got.Query["api-version"] != APIVersion || got.Query["top"] != "1" {
			t.Errorf("health query = %v", got.Query)
		}
		if got.Header.Get("Ocp-Apim-Subscription-Key") != "k" {
			t.Error("health must use the same credential as transcription")
		}
	})

	t.Run("invalid key", func(t *testing.T) {
		server, _ := fakeService(t, http.StatusUnauthorized, "")
		p := newTestProvider(server, Options{APIKey: "k"})
		err := p.Health(context.Background())
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "key") {
			t.Errorf("a 401 on the key path must point at the key: %v", err)
		}
	})

	t.Run("missing role", func(t *testing.T) {
		server, _ := fakeService(t, http.StatusForbidden, "")
		p := newTestProvider(server, Options{BearerToken: bearer})
		err := p.Health(context.Background())
		if err == nil || !strings.Contains(err.Error(), "Cognitive Services User") {
			t.Errorf("a 403 on the bearer path must point at the roles: %v", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		server, _ := fakeService(t, http.StatusInternalServerError, "")
		p := newTestProvider(server, Options{APIKey: "k"})
		if err := p.Health(context.Background()); err == nil {
			t.Error("expected an error for HTTP 500")
		}
	})
}

func TestEndpoint_BareHostBecomesHTTPS(t *testing.T) {
	// The colon in "transcriptions:transcribe" must survive URL building
	// unescaped; the service does not accept %3A.
	p := New(Options{Host: "myresource.cognitiveservices.azure.com"})
	got, err := p.endpoint(transcribePath, "api-version="+APIVersion)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	want := "https://myresource.cognitiveservices.azure.com/speechtotext/transcriptions:transcribe?api-version=" + APIVersion
	if got != want {
		t.Errorf("endpoint = %q, want %q", got, want)
	}
	if _, err := New(Options{}).endpoint(transcribePath, ""); err == nil {
		t.Error("an empty host must be rejected before any request")
	}
}

func TestCapabilitiesIncludeDiarization(t *testing.T) {
	caps := New(Options{}).Capabilities()
	found := false
	for _, c := range caps {
		if c == speechkit.CapabilitySpeakerDiarization {
			found = true
		}
	}
	if !found {
		t.Errorf("capabilities = %v, want speaker diarization", caps)
	}
}

func TestShortLocale(t *testing.T) {
	cases := map[string]string{
		"de-DE": "de",
		"en-US": "en",
		"pt-BR": "pt",
		"zh-CN": "zh",
		"nb-NO": "nb",
		"yue":   "yue",
		"fil":   "fil",
		"DE":    "de",
		"de_AT": "de",
		" en ":  "en",
		"":      "",
	}
	for in, want := range cases {
		if got := ShortLocale(in); got != want {
			t.Errorf("ShortLocale(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsMAITranscribeModel(t *testing.T) {
	cases := map[string]bool{
		"MAI-Transcribe-2":        true,
		"mai-transcribe-1.5":      true,
		" MAI-Transcribe-Medical": true,
		"gpt-4o-mini-transcribe":  false,
		"whisper-1":               false,
		"":                        false,
	}
	for in, want := range cases {
		if got := IsMAITranscribeModel(in); got != want {
			t.Errorf("IsMAITranscribeModel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestProviderContract(t *testing.T) {
	sttcontract.RunContract(t, sttcontract.Case{
		Name:         "azurespeech",
		ExpectedName: "foundry",
		WantText:     "Hallo Welt. Guten Tag.",
		NewProvider: func(baseURL string) stt.STTProvider {
			p := New(Options{Host: baseURL, APIKey: "contract-key"})
			p.Validation = testValidation
			return p
		},
		Success: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, successBody)
		},
	})
}
