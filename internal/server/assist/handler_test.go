//go:build linux

package assist

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	assistpkg "github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

// ── fakes ───────────────────────────────────────────────────────────────────

type fakeProcessor struct {
	result      *assistpkg.Result
	err         error
	lastCalled  bool
	lastTranscr string
	lastOpts    assistpkg.ProcessOpts
}

func (f *fakeProcessor) Process(_ context.Context, transcript string, opts assistpkg.ProcessOpts) (*assistpkg.Result, error) {
	f.lastCalled = true
	f.lastTranscr = transcript
	f.lastOpts = opts
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

type fakeTranscriber struct {
	result *stt.Result
	err    error
}

func (f *fakeTranscriber) Route(_ context.Context, _ []byte, _ float64, _ stt.TranscribeOpts) (*stt.Result, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func okAssistResult() *assistpkg.Result {
	return &assistpkg.Result{
		Text:      "Sure, here is a quick summary.",
		SpeakText: "Sure, here is a quick summary.",
		Action:    "respond",
		Locale:    "en",
		Surface:   assistpkg.ResultSurfacePanel,
		Kind:      assistpkg.ResultKindAnswer,
		Audio:     []byte{0x00, 0x01, 0x02, 0x03},
		Format:    "mp3",
	}
}

func mustHandler(t *testing.T, opts Options) *Handler {
	t.Helper()
	if opts.MaxUploadMB == 0 {
		opts.MaxUploadMB = 25
	}
	h, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func synthSine(rate, ms int) []byte {
	frames := rate * ms / 1000
	out := make([]byte, frames*2)
	for f := 0; f < frames; f++ {
		tSec := float64(f) / float64(rate)
		val := int16(math.Sin(2*math.Pi*440.0*tSec) * 8000)
		binary.LittleEndian.PutUint16(out[f*2:], uint16(val))
	}
	return out
}

func wrapWAV(pcm []byte, rate int) []byte {
	dataSize := uint32(len(pcm))
	buf := make([]byte, 44+len(pcm))
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], 36+dataSize)
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1)
	binary.LittleEndian.PutUint16(buf[22:], 1)
	binary.LittleEndian.PutUint32(buf[24:], uint32(rate))
	binary.LittleEndian.PutUint32(buf[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(buf[32:], 2)
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], dataSize)
	copy(buf[44:], pcm)
	return buf
}

// ── tests ───────────────────────────────────────────────────────────────────

func TestHandler_New_RejectsNilProcessor(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatalf("expected error when processor is nil")
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	h := mustHandler(t, Options{Processor: &fakeProcessor{result: okAssistResult()}})
	req := httptest.NewRequest(http.MethodGet, "/v1/assist/process", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d want 405", rec.Code)
	}
}

func TestHandler_JSON_TextInput_HappyPath(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	h := mustHandler(t, Options{Processor: fp, DefaultLocale: "en"})

	body, _ := json.Marshal(map[string]any{
		"text":   "Summarize today's weather",
		"locale": "en",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !fp.lastCalled {
		t.Fatalf("processor was not invoked")
	}
	if fp.lastTranscr != "Summarize today's weather" {
		t.Fatalf("transcript = %q", fp.lastTranscr)
	}
	if fp.lastOpts.Locale != "en" {
		t.Fatalf("locale = %q", fp.lastOpts.Locale)
	}

	var resp struct {
		Text        string `json:"text"`
		Action      string `json:"action"`
		Locale      string `json:"locale"`
		Transcript  string `json:"transcript"`
		AudioBase64 string `json:"audio_base64"`
		AudioFormat string `json:"audio_format"`
		LatencyMs   int64  `json:"latency_ms"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Text != "Sure, here is a quick summary." || resp.Action != "respond" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Transcript != "Summarize today's weather" {
		t.Fatalf("transcript = %q", resp.Transcript)
	}
	if resp.AudioFormat != "mp3" || resp.AudioBase64 == "" {
		t.Fatalf("expected audio in response; got format=%q base64_len=%d", resp.AudioFormat, len(resp.AudioBase64))
	}
}

func TestHandler_JSON_TextInput_TTSOptOut(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	h := mustHandler(t, Options{Processor: fp})

	body, _ := json.Marshal(map[string]any{
		"text": "say something",
		"tts":  false,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"audio_base64"`) {
		t.Fatalf("audio_base64 should be omitted when tts=false; got %s", rec.Body.String())
	}
}

func TestHandler_JSON_MissingInput(t *testing.T) {
	h := mustHandler(t, Options{Processor: &fakeProcessor{result: okAssistResult()}})
	body := []byte(`{"locale":"en"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "missing_input") {
		t.Fatalf("expected missing_input error; got %s", rec.Body.String())
	}
}

func TestHandler_JSON_AudioInput_HappyPath(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	ft := &fakeTranscriber{result: &stt.Result{Text: "hello there", Language: "en", Provider: "fake"}}
	h := mustHandler(t, Options{Processor: fp, Transcriber: ft, DefaultLocale: "en"})

	pcm := synthSine(16000, 250)
	wav := wrapWAV(pcm, 16000)
	body, _ := json.Marshal(map[string]any{
		"audio_base64": base64Encode(wav),
		"format":       "wav",
		"locale":       "en",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fp.lastTranscr != "hello there" {
		t.Fatalf("processor expected STT transcript; got %q", fp.lastTranscr)
	}

	var resp struct {
		Transcript string      `json:"transcript"`
		Source     *sourceMeta `json:"source"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Transcript != "hello there" {
		t.Fatalf("response transcript = %q", resp.Transcript)
	}
	if resp.Source == nil || resp.Source.Format != "wav" {
		t.Fatalf("source meta = %+v", resp.Source)
	}
}

func TestHandler_AudioInput_NoTranscriberReturns503(t *testing.T) {
	h := mustHandler(t, Options{Processor: &fakeProcessor{result: okAssistResult()} /* no transcriber */})
	pcm := synthSine(16000, 100)
	wav := wrapWAV(pcm, 16000)
	body, _ := json.Marshal(map[string]any{
		"audio_base64": base64Encode(wav),
		"format":       "wav",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "stt_unavailable") {
		t.Fatalf("expected stt_unavailable; got %s", rec.Body.String())
	}
}

func TestHandler_AudioInput_EmptyTranscriptReturns422(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	ft := &fakeTranscriber{result: &stt.Result{Text: "", Provider: "fake"}}
	h := mustHandler(t, Options{Processor: fp, Transcriber: ft})
	pcm := synthSine(16000, 100)
	wav := wrapWAV(pcm, 16000)
	body, _ := json.Marshal(map[string]any{
		"audio_base64": base64Encode(wav),
		"format":       "wav",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d want 422", rec.Code)
	}
}

func TestHandler_Multipart_TextField(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	h := mustHandler(t, Options{Processor: fp, DefaultLocale: "en"})

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("text", "compose an email")
	_ = mw.WriteField("locale", "en")
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fp.lastTranscr != "compose an email" {
		t.Fatalf("transcript = %q", fp.lastTranscr)
	}
}

func TestHandler_Multipart_AudioFile(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	ft := &fakeTranscriber{result: &stt.Result{Text: "note for tomorrow", Language: "en"}}
	h := mustHandler(t, Options{Processor: fp, Transcriber: ft, DefaultLocale: "en"})

	pcm := synthSine(16000, 250)
	wav := wrapWAV(pcm, 16000)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	partHeader := make(map[string][]string)
	partHeader["Content-Type"] = []string{"audio/wav"}
	partHeader["Content-Disposition"] = []string{`form-data; name="audio"; filename="clip.wav"`}
	part, err := mw.CreatePart(partHeader)
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	if _, err := part.Write(wav); err != nil {
		t.Fatalf("write part: %v", err)
	}
	_ = mw.WriteField("locale", "en")
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fp.lastTranscr != "note for tomorrow" {
		t.Fatalf("transcript = %q", fp.lastTranscr)
	}
}

func TestHandler_Multipart_MissingAudioAndText(t *testing.T) {
	h := mustHandler(t, Options{Processor: &fakeProcessor{result: okAssistResult()}})
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("locale", "en")
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "missing_input") {
		t.Fatalf("expected missing_input; got %s", rec.Body.String())
	}
}

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
	fp := &fakeProcessor{result: &assistpkg.Result{Text: "", Action: "respond"}}
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

func TestHandler_Pipeline_ConfigErrorClassified(t *testing.T) {
	fp := &fakeProcessor{err: errors.New("assist: LLM failed: INVALID_ARGUMENT: Invalid configuration type: *ai.GenerationCommonConfig. Expected *genai.GenerateContentConfig")}
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

func base64Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
