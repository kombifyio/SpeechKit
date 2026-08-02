//go:build linux

package dictation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/server/wssession"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

// The tests in this file assert on the query string that reaches the provider,
// not on the router boundary. A language is only honoured if it survives
// handler → router → provider adapter → wire, and a defect that pinned every
// server transcription to one language sat in the segment the other tests stop
// short of.

// deepgramStub stands in for Deepgram's Listen API and records the query of
// every request it serves.
type deepgramStub struct {
	*httptest.Server

	mu    sync.Mutex
	query url.Values
}

// newDeepgramListenStub serves the batch endpoint.
func newDeepgramListenStub(t *testing.T) *deepgramStub {
	t.Helper()
	stub := &deepgramStub{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.record(r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"channels":[{"alternatives":[{"transcript":"hallo welt","confidence":0.9}]}]}}`))
	}))
	t.Cleanup(stub.Close)
	return stub
}

// newDeepgramStreamStub serves the realtime endpoint: it records the handshake
// query and then holds the socket open, which is all the adapter needs to
// report `ready`.
func newDeepgramStreamStub(t *testing.T) *deepgramStub {
	t.Helper()
	stub := &deepgramStub{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.record(r.URL.Query())
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done") //nolint:errcheck // stub teardown
		_, _, _ = conn.Read(r.Context())
	}))
	t.Cleanup(stub.Close)
	return stub
}

func (s *deepgramStub) record(q url.Values) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.query = q
}

func (s *deepgramStub) language() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.query.Get("language")
}

// newStubbedDeepgram builds the real Deepgram adapter against a stub endpoint.
// providerLanguage is the configured [providers.deepgram] stt_language, i.e.
// the tier between the request and the multilingual default.
func newStubbedDeepgram(baseURL, providerLanguage string) *stt.DeepgramProvider {
	provider := stt.NewDeepgramProvider("deepgram-test-key", "nova-3")
	provider.BaseURL = baseURL
	provider.Validation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}
	provider.ApplyOptions(stt.DeepgramOptions{
		Configured:            true,
		SmartFormat:           true,
		UseVocabularyKeyterms: true,
		LanguageOverride:      providerLanguage,
	})
	return provider
}

func newStubbedDeepgramRouter(baseURL, providerLanguage string) *router.Router {
	r := &router.Router{Strategy: router.StrategyCloudOnly}
	r.AddCloud(newStubbedDeepgram(baseURL, providerLanguage))
	return r
}

func mustProviderBackedHandler(t *testing.T, baseURL, providerLanguage string) *Handler {
	t.Helper()
	h, err := New(Options{Router: newStubbedDeepgramRouter(baseURL, providerLanguage), MaxUploadMB: 25})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func postTranscribeLanguageJSON(t *testing.T, h *Handler, language string) *httptest.ResponseRecorder {
	t.Helper()
	wav := wrapWAV(synthSine(16000, 1, 440.0, 100), 16000, 1)
	payload := map[string]string{
		"audio_base64": base64.StdEncoding.EncodeToString(wav),
		"format":       "wav",
	}
	if language != "" {
		payload["language"] = language
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func postTranscribeLanguageMultipart(t *testing.T, h *Handler, language string) *httptest.ResponseRecorder {
	t.Helper()
	wav := wrapWAV(synthSine(16000, 1, 440.0, 100), 16000, 1)

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
	if language != "" {
		if err := mw.WriteField("language", language); err != nil {
			t.Fatalf("write field language: %v", err)
		}
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// languageCase covers the precedence chain shared by every entry point:
// request > configured provider language > multilingual code-switching.
type languageCase struct {
	name             string
	requestLanguage  string
	providerLanguage string
	want             string
}

func languagePrecedenceCases() []languageCase {
	return []languageCase{
		{name: "request language reaches the provider", requestLanguage: "en", want: "en"},
		{name: "request language beats the configured one", requestLanguage: "en", providerLanguage: "de", want: "en"},
		{name: "configured language fills an omitted request language", providerLanguage: "de", want: "de"},
		{name: "neither falls back to code-switching", want: "multi"},
		{name: "requested region subtag survives", requestLanguage: "de-DE", want: "de-DE"},
		{name: "requested en-GB stays en-GB", requestLanguage: "en-GB", want: "en-GB"},
		{name: "requested underscore separator is normalised", requestLanguage: "de_DE", want: "de-DE"},
		{name: "configured region subtag survives", providerLanguage: "en-GB", want: "en-GB"},
		{name: "configured underscore separator is normalised", providerLanguage: "de_DE", want: "de-DE"},
	}
}

func TestBatchTranscribe_LanguageReachesProviderJSON(t *testing.T) {
	for _, tt := range languagePrecedenceCases() {
		t.Run(tt.name, func(t *testing.T) {
			stub := newDeepgramListenStub(t)
			h := mustProviderBackedHandler(t, stub.URL, tt.providerLanguage)

			rec := postTranscribeLanguageJSON(t, h, tt.requestLanguage)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if got := stub.language(); got != tt.want {
				t.Fatalf("provider language = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBatchTranscribe_LanguageReachesProviderMultipart(t *testing.T) {
	for _, tt := range languagePrecedenceCases() {
		t.Run(tt.name, func(t *testing.T) {
			stub := newDeepgramListenStub(t)
			h := mustProviderBackedHandler(t, stub.URL, tt.providerLanguage)

			rec := postTranscribeLanguageMultipart(t, h, tt.requestLanguage)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if got := stub.language(); got != tt.want {
				t.Fatalf("provider language = %q, want %q", got, tt.want)
			}
		})
	}
}

// "auto" means "no explicit choice", so it must land on the same multilingual
// default as an omitted language rather than on a provider-side default. The
// streaming surface is deliberately absent here: it drops the language
// parameter entirely for "auto" instead of sending the code-switching value.
func TestBatchTranscribe_AutoResolvesToCodeSwitching(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		stub := newDeepgramListenStub(t)
		h := mustProviderBackedHandler(t, stub.URL, "")

		rec := postTranscribeLanguageJSON(t, h, "auto")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		if got := stub.language(); got != "multi" {
			t.Fatalf("provider language = %q, want multi", got)
		}
	})
	t.Run("multipart", func(t *testing.T) {
		stub := newDeepgramListenStub(t)
		h := mustProviderBackedHandler(t, stub.URL, "")

		rec := postTranscribeLanguageMultipart(t, h, "auto")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		if got := stub.language(); got != "multi" {
			t.Fatalf("provider language = %q, want multi", got)
		}
	})
}

// The streaming surface resolves its own precedence (the start frame carries
// the language), so it needs the same trace independently of the batch path.
func TestStreamWS_LanguageReachesProvider(t *testing.T) {
	for _, tt := range languagePrecedenceCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(wssession.EnvAllowEmptyOrigin, "1")
			stub := newDeepgramStreamStub(t)
			manager := mustStreamManager(t)
			handler := mustStreamHandler(t, manager, newStubbedDeepgramRouter(stub.URL, tt.providerLanguage), nil)
			mux := http.NewServeMux()
			handler.Mount(mux)
			server := httptest.NewServer(mux)
			defer server.Close()

			session, ticket, err := manager.Create(wssession.Identity{UserID: "user-1"})
			if err != nil {
				t.Fatalf("create session: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			conn := dialStreamWS(t, ctx, server.URL, session.ID, ticket)
			defer conn.Close(websocket.StatusNormalClosure, "") //nolint:errcheck

			writeStreamJSON(t, ctx, conn, StreamStartFrame{Type: StreamMsgStart, Language: tt.requestLanguage})
			if frame := readStreamJSON(t, ctx, conn); frame["type"] != StreamMsgReady {
				t.Fatalf("expected ready, got %v", frame)
			}
			if got := stub.language(); got != tt.want {
				t.Fatalf("provider language = %q, want %q", got, tt.want)
			}
		})
	}
}
