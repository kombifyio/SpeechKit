//go:build linux

package assist

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

// deepgramListenStub stands in for Deepgram's Listen API and records the query
// of every request it serves. Assist's locale handling is only correct if it
// survives handler → router → provider adapter → wire; asserting at the
// Transcriber boundary alone leaves the last hop untested.
type deepgramListenStub struct {
	*httptest.Server

	mu    sync.Mutex
	query url.Values
}

func newDeepgramListenStub(t *testing.T, detectedLanguage string) *deepgramListenStub {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"results": map[string]any{
			"channels": []any{
				map[string]any{
					"detected_language": detectedLanguage,
					"alternatives": []any{
						map[string]any{"transcript": "hello there", "confidence": 0.9},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal stub response: %v", err)
	}
	stub := &deepgramListenStub{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.query = r.URL.Query()
		stub.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(stub.Close)
	return stub
}

func (s *deepgramListenStub) language() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.query.Get("language")
}

func newStubbedDeepgramRouter(baseURL string) *router.Router {
	provider := stt.NewDeepgramProvider("deepgram-test-key", "nova-3")
	provider.BaseURL = baseURL
	provider.Validation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}
	provider.ApplyOptions(stt.DeepgramOptions{
		Configured:            true,
		SmartFormat:           true,
		UseVocabularyKeyterms: true,
	})
	r := &router.Router{Strategy: router.StrategyCloudOnly}
	r.AddCloud(provider)
	return r
}

func postAssistAudioMultipart(t *testing.T, h *Handler, locale string) *httptest.ResponseRecorder {
	t.Helper()
	wav := wrapWAV(synthSine(16000, 250), 16000)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, err := mw.CreatePart(map[string][]string{
		"Content-Type":        {"audio/wav"},
		"Content-Disposition": {`form-data; name="audio"; filename="clip.wav"`},
	})
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	if _, err := part.Write(wav); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if locale != "" {
		if err := mw.WriteField("locale", locale); err != nil {
			t.Fatalf("write field locale: %v", err)
		}
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func postAssistAudioJSON(t *testing.T, h *Handler, locale string) *httptest.ResponseRecorder {
	t.Helper()
	payload := map[string]any{
		"audio_base64": base64Encode(wrapWAV(synthSine(16000, 250), 16000)),
		"format":       "wav",
	}
	if locale != "" {
		payload["locale"] = locale
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The multipart branch builds its own ProcessOpts, so the JSON-path guarantees
// do not carry over to it.
func TestHandler_MultipartAudio_DoesNotPinSTTToDefaultLocale(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	ft := &fakeTranscriber{result: &stt.Result{Text: "hello there", Language: "en", Provider: "fake"}}
	h := mustHandler(t, Options{Processor: fp, Transcriber: ft, DefaultLocale: "de"})

	rec := postAssistAudioMultipart(t, h, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := ft.lastOpts.Language; got != "" {
		t.Fatalf("STT language = %q; a caller that sent no locale must not be pinned to the server default", got)
	}
}

func TestHandler_MultipartAudio_ForwardsRequestedLocaleToSTT(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	ft := &fakeTranscriber{result: &stt.Result{Text: "hello there", Language: "en", Provider: "fake"}}
	h := mustHandler(t, Options{Processor: fp, Transcriber: ft, DefaultLocale: "de"})

	rec := postAssistAudioMultipart(t, h, "en-GB")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := ft.lastOpts.Language; got != "en-GB" {
		t.Fatalf("STT language = %q, want en-GB", got)
	}
}

func TestHandler_MultipartAudio_MultiIsNotAdoptedAsReplyLocale(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	ft := &fakeTranscriber{result: &stt.Result{Text: "hello there", Language: "multi", Provider: "fake"}}
	h := mustHandler(t, Options{Processor: fp, Transcriber: ft, DefaultLocale: "de"})

	rec := postAssistAudioMultipart(t, h, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := fp.lastOpts.Locale; got != "de" {
		t.Fatalf("reply locale = %q, want the server default rather than the routing value", got)
	}
}

// A concrete language the provider reports is still adopted, so keeping the
// default out of the STT call does not cost the reply its locale.
func TestHandler_MultipartAudio_AdoptsDetectedLocaleForReply(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	ft := &fakeTranscriber{result: &stt.Result{Text: "hello there", Language: "en-GB", Provider: "fake"}}
	h := mustHandler(t, Options{Processor: fp, Transcriber: ft, DefaultLocale: "de"})

	rec := postAssistAudioMultipart(t, h, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := fp.lastOpts.Locale; got != "en-GB" {
		t.Fatalf("reply locale = %q, want the detected en-GB", got)
	}
}

// The text branch has no STT to learn a locale from, so it must keep resolving
// the server default itself.
func TestHandler_TextInput_ResolvesDefaultLocale(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	h := mustHandler(t, Options{Processor: fp, DefaultLocale: "de"})

	body, _ := json.Marshal(map[string]any{"text": "summarize this"})
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := fp.lastOpts.Locale; got != "de" {
		t.Fatalf("reply locale = %q, want the server default de", got)
	}
}

func TestHandler_Audio_LocaleReachesProvider(t *testing.T) {
	tests := []struct {
		name   string
		locale string
		want   string
	}{
		{name: "no locale leaves code-switching in place", want: "multi"},
		{name: "explicit locale reaches the provider", locale: "en", want: "en"},
		{name: "region subtag survives", locale: "de-DE", want: "de-DE"},
		{name: "en-GB stays en-GB", locale: "en-GB", want: "en-GB"},
		{name: "underscore separator is normalised", locale: "de_DE", want: "de-DE"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" (json)", func(t *testing.T) {
			stub := newDeepgramListenStub(t, "")
			h := mustHandler(t, Options{
				Processor:     &fakeProcessor{result: okAssistResult()},
				Transcriber:   newStubbedDeepgramRouter(stub.URL),
				DefaultLocale: "de",
			})

			rec := postAssistAudioJSON(t, h, tt.locale)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if got := stub.language(); got != tt.want {
				t.Fatalf("provider language = %q, want %q", got, tt.want)
			}
		})
		t.Run(tt.name+" (multipart)", func(t *testing.T) {
			stub := newDeepgramListenStub(t, "")
			h := mustHandler(t, Options{
				Processor:     &fakeProcessor{result: okAssistResult()},
				Transcriber:   newStubbedDeepgramRouter(stub.URL),
				DefaultLocale: "de",
			})

			rec := postAssistAudioMultipart(t, h, tt.locale)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if got := stub.language(); got != tt.want {
				t.Fatalf("provider language = %q, want %q", got, tt.want)
			}
		})
	}
}

// The server default must not leak into the provider request on any input
// shape — that is exactly the defect this file exists to catch.
func TestHandler_Audio_ServerDefaultLocaleNeverReachesProvider(t *testing.T) {
	stub := newDeepgramListenStub(t, "")
	h := mustHandler(t, Options{
		Processor:     &fakeProcessor{result: okAssistResult()},
		Transcriber:   newStubbedDeepgramRouter(stub.URL),
		DefaultLocale: "de",
	})

	rec := postAssistAudioJSON(t, h, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := stub.language(); got == "de" {
		t.Fatalf("provider language = %q; the reply-locale default must not become a transcription override", got)
	}
}
