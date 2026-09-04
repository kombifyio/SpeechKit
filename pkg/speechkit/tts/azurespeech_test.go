package tts

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

var azureSpeechTestValidation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}

// azureSpeechCapture records the last request the fake Speech endpoint saw.
type azureSpeechCapture struct {
	Method string
	Path   string
	Header http.Header
	Body   string
	Calls  atomic.Int32
}

func azureSpeechFake(t *testing.T, status int, contentType string, body []byte) (*httptest.Server, *azureSpeechCapture) {
	t.Helper()
	captured := &azureSpeechCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.Calls.Add(1)
		captured.Method = r.Method
		captured.Path = r.URL.Path
		captured.Header = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		captured.Body = string(raw)
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server, captured
}

func newAzureSpeechTest(server *httptest.Server, opts AzureSpeechOpts) *AzureSpeech {
	opts.Host = server.URL
	a := NewAzureSpeech(opts)
	a.Validation = azureSpeechTestValidation
	return a
}

// assertWellFormedXML fails when the Speech service would reject the SSML
// as unparsable.
func assertWellFormedXML(t *testing.T, doc string) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(doc))
	for {
		_, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatalf("SSML is not well-formed: %v\n%s", err, doc)
		}
	}
}

func TestAzureSpeechSynthesizeBuildsSSML(t *testing.T) {
	fakeAudio := []byte("fake-mp3-audio")
	server, got := azureSpeechFake(t, http.StatusOK, "audio/mpeg", fakeAudio)
	a := newAzureSpeechTest(server, AzureSpeechOpts{
		APIKey:      "resource-key",
		Voice:       "de-DE-Mia:MAI-Voice-2",
		Style:       "friendly",
		StyleDegree: 1.2,
	})

	res, err := a.Synthesize(context.Background(), "Tom & Jerry <3", SynthesizeOpts{Speed: 1.1, Format: "mp3"})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	if got.Method != http.MethodPost || got.Path != "/tts/cognitiveservices/v1" {
		t.Errorf("request hit %s %s", got.Method, got.Path)
	}
	if ct := got.Header.Get("Content-Type"); ct != "application/ssml+xml" {
		t.Errorf("Content-Type = %q", ct)
	}
	if of := got.Header.Get("X-Microsoft-OutputFormat"); of != "audio-24khz-96kbitrate-mono-mp3" {
		t.Errorf("X-Microsoft-OutputFormat = %q", of)
	}
	if ua := got.Header.Get("User-Agent"); ua != "kombify-SpeechKit" {
		t.Errorf("User-Agent = %q", ua)
	}
	if got.Header.Get("Ocp-Apim-Subscription-Key") != "resource-key" {
		t.Errorf("key header = %q", got.Header.Get("Ocp-Apim-Subscription-Key"))
	}
	if got.Header.Get("Authorization") != "" {
		t.Error("no Authorization header expected on the key path")
	}

	assertWellFormedXML(t, got.Body)
	for _, want := range []string{
		`xml:lang="de-DE"`,
		`<voice name="de-DE-Mia:MAI-Voice-2">`,
		`<mstts:express-as style="friendly" styledegree="1.2">`,
		`<prosody rate="+10%">Tom &amp; Jerry &lt;3</prosody>`,
		`</mstts:express-as></voice></speak>`,
	} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("SSML lacks %s:\n%s", want, got.Body)
		}
	}

	if !bytes.Equal(res.Audio, fakeAudio) {
		t.Error("audio bytes were not passed through")
	}
	if res.Format != "mp3" || res.SampleRate != 24000 || res.Provider != "foundry" || res.Voice != "de-DE-Mia:MAI-Voice-2" {
		t.Errorf("result = format %q rate %d provider %q voice %q", res.Format, res.SampleRate, res.Provider, res.Voice)
	}
}

func TestAzureSpeechSynthesizeMinimalSSML(t *testing.T) {
	server, got := azureSpeechFake(t, http.StatusOK, "audio/mpeg", []byte("audio"))
	a := newAzureSpeechTest(server, AzureSpeechOpts{APIKey: "k"})

	res, err := a.Synthesize(context.Background(), "Hello", SynthesizeOpts{})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	assertWellFormedXML(t, got.Body)
	if strings.Contains(got.Body, "express-as") || strings.Contains(got.Body, "prosody") {
		t.Errorf("default request must not carry style or prosody:\n%s", got.Body)
	}
	if !strings.Contains(got.Body, `<voice name="en-US-Harper:MAI-Voice-2">Hello</voice>`) {
		t.Errorf("default voice with plain text expected:\n%s", got.Body)
	}
	if !strings.Contains(got.Body, `xml:lang="en-US"`) {
		t.Errorf("xml:lang must follow the voice locale:\n%s", got.Body)
	}
	if res.Voice != "en-US-Harper:MAI-Voice-2" {
		t.Errorf("Voice = %q", res.Voice)
	}
}

func TestAzureSpeechSynthesizeRequestVoiceOverride(t *testing.T) {
	server, got := azureSpeechFake(t, http.StatusOK, "audio/mpeg", []byte("audio"))
	a := newAzureSpeechTest(server, AzureSpeechOpts{APIKey: "k", Voice: "de-DE-Mia:MAI-Voice-2"})

	res, err := a.Synthesize(context.Background(), "Hi", SynthesizeOpts{Voice: "en-US-Ethan:MAI-Voice-2-Flash"})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if !strings.Contains(got.Body, `xml:lang="en-US"`) || !strings.Contains(got.Body, `<voice name="en-US-Ethan:MAI-Voice-2-Flash">`) {
		t.Errorf("request voice must win and drive xml:lang:\n%s", got.Body)
	}
	if res.Voice != "en-US-Ethan:MAI-Voice-2-Flash" {
		t.Errorf("Voice = %q", res.Voice)
	}
}

func TestAzureSpeechProsodyRate(t *testing.T) {
	cases := []struct {
		speed float64
		want  string
	}{
		{0.75, `rate="-25%"`},
		{1.5, `rate="+50%"`},
		{5, `rate="+100%"`},
		{0.1, `rate="-50%"`},
	}
	for _, tc := range cases {
		server, got := azureSpeechFake(t, http.StatusOK, "audio/mpeg", []byte("audio"))
		a := newAzureSpeechTest(server, AzureSpeechOpts{APIKey: "k"})
		if _, err := a.Synthesize(context.Background(), "x", SynthesizeOpts{Speed: tc.speed}); err != nil {
			t.Fatalf("speed %v: %v", tc.speed, err)
		}
		if !strings.Contains(got.Body, tc.want) {
			t.Errorf("speed %v: SSML lacks %s:\n%s", tc.speed, tc.want, got.Body)
		}
	}
}

func TestAzureSpeechOutputFormats(t *testing.T) {
	cases := []struct {
		format     string
		wantFormat string
		wantHeader string
	}{
		{"mp3", "mp3", "audio-24khz-96kbitrate-mono-mp3"},
		{"wav", "wav", "riff-24khz-16bit-mono-pcm"},
		{"pcm", "pcm", "raw-24khz-16bit-mono-pcm"},
		{"opus", "opus", "ogg-24khz-16bit-mono-opus"},
		{"flac", "mp3", "audio-24khz-96kbitrate-mono-mp3"},
		{"", "mp3", "audio-24khz-96kbitrate-mono-mp3"},
	}
	for _, tc := range cases {
		server, got := azureSpeechFake(t, http.StatusOK, "application/octet-stream", []byte("audio"))
		a := newAzureSpeechTest(server, AzureSpeechOpts{APIKey: "k"})
		res, err := a.Synthesize(context.Background(), "x", SynthesizeOpts{Format: tc.format})
		if err != nil {
			t.Fatalf("format %q: %v", tc.format, err)
		}
		if h := got.Header.Get("X-Microsoft-OutputFormat"); h != tc.wantHeader {
			t.Errorf("format %q: header = %q, want %q", tc.format, h, tc.wantHeader)
		}
		if res.Format != tc.wantFormat {
			t.Errorf("format %q: Result.Format = %q, want %q", tc.format, res.Format, tc.wantFormat)
		}
		if res.SampleRate != 24000 {
			t.Errorf("format %q: SampleRate = %d", tc.format, res.SampleRate)
		}
	}
}

func TestAzureSpeechCredentials(t *testing.T) {
	bearer := func(context.Context) (string, error) { return "entra-token", nil }

	t.Run("bearer wins over key", func(t *testing.T) {
		server, got := azureSpeechFake(t, http.StatusOK, "audio/mpeg", []byte("audio"))
		a := newAzureSpeechTest(server, AzureSpeechOpts{APIKey: "resource-key", BearerToken: bearer})
		if _, err := a.Synthesize(context.Background(), "x", SynthesizeOpts{}); err != nil {
			t.Fatalf("Synthesize: %v", err)
		}
		if got.Header.Get("Authorization") != "Bearer entra-token" {
			t.Errorf("Authorization = %q", got.Header.Get("Authorization"))
		}
		if got.Header.Get("Ocp-Apim-Subscription-Key") != "" {
			t.Error("the key must not be sent when a bearer token is available")
		}
	})

	t.Run("bearer error surfaces and nothing is sent", func(t *testing.T) {
		server, got := azureSpeechFake(t, http.StatusOK, "audio/mpeg", []byte("audio"))
		tokenErr := errors.New("az login required")
		a := newAzureSpeechTest(server, AzureSpeechOpts{BearerToken: func(context.Context) (string, error) { return "", tokenErr }})
		if _, err := a.Synthesize(context.Background(), "x", SynthesizeOpts{}); !errors.Is(err, tokenErr) {
			t.Fatalf("error = %v, want the token error wrapped", err)
		}
		if got.Calls.Load() != 0 {
			t.Error("no request must leave the process when the token cannot be minted")
		}
	})

	t.Run("no credential", func(t *testing.T) {
		server, got := azureSpeechFake(t, http.StatusOK, "audio/mpeg", []byte("audio"))
		a := newAzureSpeechTest(server, AzureSpeechOpts{})
		if _, err := a.Synthesize(context.Background(), "x", SynthesizeOpts{}); err == nil {
			t.Fatal("expected an error without key or bearer token")
		}
		if got.Calls.Load() != 0 {
			t.Error("no request expected without a credential")
		}
	})
}

func TestAzureSpeechSynthesizeErrors(t *testing.T) {
	t.Run("empty text", func(t *testing.T) {
		a := NewAzureSpeech(AzureSpeechOpts{Host: "example.cognitiveservices.azure.com", APIKey: "k"})
		if _, err := a.Synthesize(context.Background(), "", SynthesizeOpts{}); err == nil {
			t.Fatal("expected error for empty text")
		}
	})

	t.Run("missing host", func(t *testing.T) {
		a := NewAzureSpeech(AzureSpeechOpts{APIKey: "k"})
		if _, err := a.Synthesize(context.Background(), "x", SynthesizeOpts{}); err == nil {
			t.Fatal("expected error without a host")
		}
	})

	t.Run("server error propagates", func(t *testing.T) {
		server, _ := azureSpeechFake(t, http.StatusInternalServerError, "", []byte("boom"))
		a := newAzureSpeechTest(server, AzureSpeechOpts{APIKey: "k"})
		if _, err := a.Synthesize(context.Background(), "x", SynthesizeOpts{}); err == nil {
			t.Fatal("expected error for HTTP 500")
		}
	})

	t.Run("auth body does not leak", func(t *testing.T) {
		server, _ := azureSpeechFake(t, http.StatusUnauthorized, "", []byte(`{"error":{"code":"401","message":"invalid subscription key sk-secret-body"}}`))
		a := newAzureSpeechTest(server, AzureSpeechOpts{APIKey: "k"})
		_, err := a.Synthesize(context.Background(), "x", SynthesizeOpts{})
		if err == nil {
			t.Fatal("expected error for HTTP 401")
		}
		if strings.Contains(err.Error(), "sk-secret-body") {
			t.Errorf("provider response body leaked in error: %v", err)
		}
	})

	t.Run("bad request surfaces the service message", func(t *testing.T) {
		server, _ := azureSpeechFake(t, http.StatusBadRequest, "", []byte(`{"code":"InvalidRequest","message":"Voice 'xx-XX-Nobody:MAI-Voice-2' is not available in this region."}`))
		a := newAzureSpeechTest(server, AzureSpeechOpts{APIKey: "k"})
		_, err := a.Synthesize(context.Background(), "x", SynthesizeOpts{})
		if err == nil || !strings.Contains(err.Error(), "not available in this region") {
			t.Errorf("the service's explanation must reach the caller: %v", err)
		}
	})
}

const azureSpeechVoicesBody = `[
  {"Name":"Microsoft Server Speech Text to Speech Voice (de-DE, Mia:MAI-Voice-2)",
   "DisplayName":"Mia","LocalName":"Mia","ShortName":"de-DE-Mia:MAI-Voice-2","Gender":"Female",
   "Locale":"de-DE","LocaleName":"German (Germany)","StyleList":["friendly","whispering"],
   "SecondaryLocaleList":["en-US"],"SampleRateHertz":"24000","VoiceType":"NeuralHD","Status":"Preview",
   "VoiceTag":{"ModelSeries":["MAI-Voice-2"],"PersonaId":["mia"],"Source":["MAI"]}},
  {"Name":"Microsoft Server Speech Text to Speech Voice (de-DE, KatjaNeural)",
   "DisplayName":"Katja","LocalName":"Katja","ShortName":"de-DE-KatjaNeural","Gender":"Female",
   "Locale":"de-DE","LocaleName":"German (Germany)","SampleRateHertz":"48000","VoiceType":"Neural","Status":"GA"}
]`

func TestAzureSpeechListVoices(t *testing.T) {
	server, got := azureSpeechFake(t, http.StatusOK, "application/json", []byte(azureSpeechVoicesBody))
	a := newAzureSpeechTest(server, AzureSpeechOpts{APIKey: "k"})

	voices, err := a.ListVoices(context.Background())
	if err != nil {
		t.Fatalf("ListVoices: %v", err)
	}
	if got.Method != http.MethodGet || got.Path != "/tts/cognitiveservices/voices/list" {
		t.Errorf("request hit %s %s", got.Method, got.Path)
	}
	if got.Header.Get("Ocp-Apim-Subscription-Key") != "k" {
		t.Error("voices list must use the same credential as synthesis")
	}
	if len(voices) != 2 {
		t.Fatalf("voices = %d, want 2", len(voices))
	}
	mia := voices[0]
	if mia.ShortName != "de-DE-Mia:MAI-Voice-2" || mia.DisplayName != "Mia" || mia.LocalName != "Mia" ||
		mia.Locale != "de-DE" || mia.Gender != "Female" || mia.VoiceType != "NeuralHD" || mia.Status != "Preview" {
		t.Errorf("mia = %+v", mia)
	}
	if mia.SampleRateHertz != 24000 {
		t.Errorf("SampleRateHertz = %d, want the quoted string decoded", mia.SampleRateHertz)
	}
	if mia.ModelSeries != "MAI-Voice-2" {
		t.Errorf("ModelSeries = %q", mia.ModelSeries)
	}
	if len(mia.Styles) != 2 || len(mia.SecondaryLocales) != 1 {
		t.Errorf("styles/secondary locales = %v / %v", mia.Styles, mia.SecondaryLocales)
	}
	if !IsMAIVoice(mia.ShortName) || IsMAIVoice(voices[1].ShortName) {
		t.Error("IsMAIVoice must separate MAI from classic neural voices")
	}
	if voices[1].ModelSeries != "" || voices[1].SampleRateHertz != 48000 {
		t.Errorf("classic voice = %+v", voices[1])
	}
}

func TestAzureSpeechHealth(t *testing.T) {
	bearer := func(context.Context) (string, error) { return "entra-token", nil }

	t.Run("ok", func(t *testing.T) {
		server, _ := azureSpeechFake(t, http.StatusOK, "application/json", []byte(azureSpeechVoicesBody))
		a := newAzureSpeechTest(server, AzureSpeechOpts{APIKey: "k"})
		if err := a.Health(context.Background()); err != nil {
			t.Fatalf("Health: %v", err)
		}
	})

	t.Run("invalid key", func(t *testing.T) {
		server, _ := azureSpeechFake(t, http.StatusUnauthorized, "", nil)
		a := newAzureSpeechTest(server, AzureSpeechOpts{APIKey: "k"})
		err := a.Health(context.Background())
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "key") {
			t.Errorf("a 401 on the key path must point at the key: %v", err)
		}
	})

	t.Run("missing role", func(t *testing.T) {
		server, _ := azureSpeechFake(t, http.StatusForbidden, "", nil)
		a := newAzureSpeechTest(server, AzureSpeechOpts{BearerToken: bearer})
		err := a.Health(context.Background())
		if err == nil || !strings.Contains(err.Error(), "Cognitive Services User") {
			t.Errorf("a 403 on the bearer path must point at the roles: %v", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		server, _ := azureSpeechFake(t, http.StatusServiceUnavailable, "", nil)
		a := newAzureSpeechTest(server, AzureSpeechOpts{APIKey: "k"})
		if err := a.Health(context.Background()); err == nil {
			t.Error("expected an error for HTTP 503")
		}
	})
}

func TestAzureSpeechIdentity(t *testing.T) {
	a := NewAzureSpeech(AzureSpeechOpts{})
	if a.Name() != "foundry" {
		t.Errorf("Name() = %q, want foundry", a.Name())
	}
	if a.Kind() != ProviderKindDirectProvider {
		t.Errorf("Kind() = %q", a.Kind())
	}
	var _ Provider = a
	var _ CapabilityReporter = a
}

func TestVoiceLocale(t *testing.T) {
	cases := map[string]string{
		"de-DE-Mia:MAI-Voice-2":         "de-DE",
		"en-US-Ethan:MAI-Voice-2-Flash": "en-US",
		"de-DE-KatjaNeural":             "de-DE",
		"zh-CN-shaanxi-XiaoniNeural":    "zh-CN",
		"es-419-SomeoneNeural":          "es-419",
		" pt-BR-Thalita:MAI-Voice-2 ":   "pt-BR",
		"Harper":                        "",
		"":                              "",
		"alloy":                         "",
		"MAI-Voice-2":                   "",
	}
	for in, want := range cases {
		if got := VoiceLocale(in); got != want {
			t.Errorf("VoiceLocale(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsMAIVoiceAndFamilies(t *testing.T) {
	if !IsMAIVoice("de-DE-Mia:MAI-Voice-2") || !IsMAIVoice("en-US-Ethan:MAI-Voice-2-Flash") {
		t.Error("MAI short names must be recognised")
	}
	if IsMAIVoice("de-DE-KatjaNeural") || IsMAIVoice("alloy") || IsMAIVoice("") {
		t.Error("classic and OpenAI voice names are not MAI voices")
	}
	families := MAIVoiceFamilies()
	if len(families) != 2 {
		t.Fatalf("families = %v", families)
	}
	for _, family := range families {
		if !IsMAIVoice("en-US-Harper:" + family) {
			t.Errorf("a voice in family %s must be recognised as MAI", family)
		}
	}
}
