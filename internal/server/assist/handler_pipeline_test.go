//go:build linux

package assist

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	assistpkg "github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_Pipeline_ErrorMapsTo503(t *testing.T) {
	fp := &fakeProcessor{err: errors.New("model down")}
	h := mustHandler(t, Options{Processor: fp})
	body := []byte(`{"text":"do the thing"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "pipeline_unavailable") {
		t.Fatalf("expected pipeline_unavailable; got %s", rec.Body.String())
	}
}

func TestHandler_Pipeline_EmptyOutputMapsTo503(t *testing.T) {
	fp := &fakeProcessor{result: speechkit.AssistResult{Text: "", Action: "respond"}}
	h := mustHandler(t, Options{Processor: fp})
	body := []byte(`{"text":"do the thing"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", rec.Code, rec.Body.String())
	}
	var bodyOut struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bodyOut); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if bodyOut.Error.Code != "pipeline_unavailable" {
		t.Fatalf("code = %q, want pipeline_unavailable", bodyOut.Error.Code)
	}
	if got := bodyOut.Error.Details["category"]; got != "empty_result" {
		t.Fatalf("category = %#v, want empty_result; body=%s", got, rec.Body.String())
	}
}

func TestHandler_Pipeline_MissingModelClassified(t *testing.T) {
	fp := &fakeProcessor{err: assistpkg.ErrMissingHandler}
	h := mustHandler(t, Options{Processor: fp})
	body := []byte(`{"text":"tell me a story"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d want 503", rec.Code)
	}
	var bodyOut struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bodyOut); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if bodyOut.Error.Code != "pipeline_unavailable" {
		t.Fatalf("code = %q", bodyOut.Error.Code)
	}
	if got := bodyOut.Error.Details["category"]; got != "missing_model" {
		t.Fatalf("category = %#v, want missing_model; body=%s", got, rec.Body.String())
	}
	if got := bodyOut.Error.Details["stage"]; got != "llm" {
		t.Fatalf("stage = %#v, want llm", got)
	}
	if got := bodyOut.Error.Details["retryable"]; got != false {
		t.Fatalf("retryable = %#v, want false", got)
	}
}

func TestHandler_Pipeline_ConfigErrorClassified(t *testing.T) {
	fp := &fakeProcessor{err: errors.New("assist: LLM failed: invalid configuration")}
	h := mustHandler(t, Options{Processor: fp})
	body := []byte(`{"text":"do the thing"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d want 503", rec.Code)
	}
	var bodyOut struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bodyOut); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if bodyOut.Error.Code != "pipeline_unavailable" {
		t.Fatalf("code = %q", bodyOut.Error.Code)
	}
	if got := bodyOut.Error.Details["category"]; got != "provider_config" {
		t.Fatalf("category = %#v, want provider_config; body=%s", got, rec.Body.String())
	}
	if got := bodyOut.Error.Details["retryable"]; got != false {
		t.Fatalf("retryable = %#v, want false", got)
	}
}

func TestHandler_SelfTest_HappyPath(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	h := mustHandler(t, Options{Processor: fp})
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/self-test", nil)
	rec := httptest.NewRecorder()
	h.ServeSelfTest(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fp.lastTranscr != "Reply with exactly the single word: pong." {
		t.Fatalf("self-test transcript = %q", fp.lastTranscr)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("expected ok self-test response; got %s", rec.Body.String())
	}
}

func TestHandler_UnsupportedMediaType(t *testing.T) {
	h := mustHandler(t, Options{Processor: &fakeProcessor{result: okAssistResult()}})
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", strings.NewReader("foo"))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d want 415", rec.Code)
	}
}

// The server default is a reply/TTS locale, not a transcription language.
// Filling it in before STT made it a request-level override — the highest
// precedence tier — so every audio assist request was pinned to
// general.language even though no server setting says so.
func TestHandler_AudioInput_DoesNotPinSTTToDefaultLocale(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	ft := &fakeTranscriber{result: &stt.Result{Text: "hello there", Language: "en", Provider: "fake"}}
	h := mustHandler(t, Options{Processor: fp, Transcriber: ft, DefaultLocale: "de"})

	wav := wrapWAV(synthSine(16000, 250), 16000)
	body, _ := json.Marshal(map[string]any{
		"audio_base64": base64Encode(wav),
		"format":       "wav",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := ft.lastOpts.Language; got != "" {
		t.Fatalf("STT language = %q; a caller that sent no locale must not be pinned to the server default", got)
	}
}

// An explicit request locale must still reach STT untouched.
func TestHandler_AudioInput_ForwardsRequestedLocaleToSTT(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	ft := &fakeTranscriber{result: &stt.Result{Text: "hello there", Language: "en", Provider: "fake"}}
	h := mustHandler(t, Options{Processor: fp, Transcriber: ft, DefaultLocale: "de"})

	wav := wrapWAV(synthSine(16000, 250), 16000)
	body, _ := json.Marshal(map[string]any{
		"audio_base64": base64Encode(wav),
		"format":       "wav",
		"locale":       "en-GB",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := ft.lastOpts.Language; got != "en-GB" {
		t.Fatalf("STT language = %q, want en-GB", got)
	}
}

// Providers echo routing pseudo-values back on the result — Deepgram returns
// "multi" for code-switching — and those must not become the reply locale.
func TestHandler_AudioInput_MultiIsNotAdoptedAsReplyLocale(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	ft := &fakeTranscriber{result: &stt.Result{Text: "hello there", Language: "multi", Provider: "fake"}}
	h := mustHandler(t, Options{Processor: fp, Transcriber: ft, DefaultLocale: "de"})

	wav := wrapWAV(synthSine(16000, 250), 16000)
	body, _ := json.Marshal(map[string]any{
		"audio_base64": base64Encode(wav),
		"format":       "wav",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := fp.lastReq.Locale; got != "de" {
		t.Fatalf("reply locale = %q, want the server default rather than the routing value", got)
	}
}
