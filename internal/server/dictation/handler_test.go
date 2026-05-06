//go:build linux

package dictation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/stt"
)

// fakeRouter is the test double we wire into the handler. It records the last
// invocation so tests can assert both request shape and handler response.
type fakeRouter struct {
	result   *stt.Result
	err      error
	lastPCM  []byte
	lastSecs float64
	lastOpts stt.TranscribeOpts
	called   bool
}

func (f *fakeRouter) Route(_ context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error) {
	f.called = true
	f.lastPCM = append(f.lastPCM[:0], audio...)
	f.lastSecs = audioDurationSecs
	f.lastOpts = opts
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestHandler_New_RejectsNilRouter(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatalf("expected error when router is nil")
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	h := mustHandler(t, &fakeRouter{result: okResult()})
	req := httptest.NewRequest(http.MethodGet, "/v1/dictation/transcribe", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHandler_UnsupportedMediaType(t *testing.T) {
	h := mustHandler(t, &fakeRouter{result: okResult()})
	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", strings.NewReader("foo"))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestHandler_MultipartWAVHappyPath(t *testing.T) {
	fake := &fakeRouter{result: okResult()}
	h := mustHandler(t, fake)

	pcm := synthSine(16000, 1, 440.0, 250) // 250 ms
	wav := wrapWAV(pcm, 16000, 1)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	partHeader := make(map[string][]string)
	partHeader["Content-Type"] = []string{"audio/wav"}
	partHeader["Content-Disposition"] = []string{`form-data; name="audio"; filename="hello.wav"`}
	part, err := mw.CreatePart(partHeader)
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	if _, err := part.Write(wav); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.WriteField("language", "en"); err != nil {
		t.Fatalf("write field language: %v", err)
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !fake.called {
		t.Fatalf("router was not called")
	}
	if fake.lastOpts.Language != "en" {
		t.Fatalf("router got language=%q, want en", fake.lastOpts.Language)
	}
	// 16k mono WAV should pass through unchanged (4000 samples = 8000 bytes).
	if len(fake.lastPCM) != 8000 {
		t.Fatalf("router PCM length = %d, want 8000", len(fake.lastPCM))
	}

	var resp struct {
		Text       string `json:"text"`
		Provider   string `json:"provider"`
		Language   string `json:"language"`
		DurationMs int64  `json:"duration_ms"`
		LatencyMs  int64  `json:"latency_ms"`
		Source     struct {
			Format     string `json:"format"`
			SampleRate int    `json:"sample_rate"`
			Channels   int    `json:"channels"`
		} `json:"source"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Text != "hello world" {
		t.Fatalf("text = %q, want 'hello world'", resp.Text)
	}
	if resp.Provider != "fake" {
		t.Fatalf("provider = %q", resp.Provider)
	}
	if resp.Language != "en" {
		t.Fatalf("language = %q, want en", resp.Language)
	}
	if resp.Source.Format != "wav" || resp.Source.SampleRate != 16000 || resp.Source.Channels != 1 {
		t.Fatalf("source meta = %+v", resp.Source)
	}
	// Duration ~250 ms Â± a few.
	if resp.DurationMs < 240 || resp.DurationMs > 260 {
		t.Fatalf("DurationMs = %d, want ~250", resp.DurationMs)
	}
}

func TestHandler_MultipartMissingAudioPart(t *testing.T) {
	h := mustHandler(t, &fakeRouter{result: okResult()})

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("language", "en")
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "missing_audio") {
		t.Fatalf("body should contain error code 'missing_audio', got %s", rec.Body.String())
	}
}

func TestHandler_JSONBase64HappyPath(t *testing.T) {
	fake := &fakeRouter{result: okResult()}
	h := mustHandler(t, fake)

	pcm := synthSine(16000, 1, 440.0, 100)
	wav := wrapWAV(pcm, 16000, 1)
	payload := map[string]string{
		"audio_base64": base64.StdEncoding.EncodeToString(wav),
		"format":       "wav",
		"language":     "de",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastOpts.Language != "de" {
		t.Fatalf("language not forwarded; got %q", fake.lastOpts.Language)
	}
}

func TestHandler_DefaultPromptFeedsDictionaryHint(t *testing.T) {
	fake := &fakeRouter{result: okResult()}
	h, err := New(Options{
		Router:        fake,
		MaxUploadMB:   25,
		DefaultPrompt: "Prefer these dictionary terms: Kombify, AcmeOS.",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pcm := synthSine(16000, 1, 440.0, 100)
	wav := wrapWAV(pcm, 16000, 1)
	body, _ := json.Marshal(map[string]string{
		"audio_base64": base64.StdEncoding.EncodeToString(wav),
		"format":       "wav",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastOpts.Prompt != "Prefer these dictionary terms: Kombify, AcmeOS." {
		t.Fatalf("prompt = %q", fake.lastOpts.Prompt)
	}
}

func TestHandler_JSONInvalidBase64(t *testing.T) {
	h := mustHandler(t, &fakeRouter{result: okResult()})
	body := []byte(`{"audio_base64":"!!!not-base64!!!", "format":"wav"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_base64") {
		t.Fatalf("expected invalid_base64 in body, got %s", rec.Body.String())
	}
}

func TestHandler_JSONMissingAudio(t *testing.T) {
	h := mustHandler(t, &fakeRouter{result: okResult()})
	body := []byte(`{"format":"wav","language":"en"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_UnsupportedAudioFormat(t *testing.T) {
	h := mustHandler(t, &fakeRouter{result: okResult()})
	// 5 bytes of garbage with no recognizable Content-Type.
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	partHeader := make(map[string][]string)
	partHeader["Content-Type"] = []string{"application/octet-stream"}
	partHeader["Content-Disposition"] = []string{`form-data; name="audio"; filename="nope.bin"`}
	part, _ := mw.CreatePart(partHeader)
	// Odd length so raw PCM is rejected. `application/octet-stream` is mapped
	// to pcm16 by our detector.
	_, _ = part.Write([]byte{0x01, 0x02, 0x03})
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Our decoder treats odd-length PCM as an invalid_audio (400).
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_RouterError_Mapped503(t *testing.T) {
	fake := &fakeRouter{err: errors.New("upstream down")}
	h := mustHandler(t, fake)

	pcm := synthSine(16000, 1, 440.0, 100)
	wav := wrapWAV(pcm, 16000, 1)
	body, _ := json.Marshal(map[string]string{
		"audio_base64": base64.StdEncoding.EncodeToString(wav),
		"format":       "wav",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("router errors should map to 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "provider_unavailable") {
		t.Fatalf("expected provider_unavailable error code, got %s", rec.Body.String())
	}
}

func TestHandler_PayloadTooLarge_JSON(t *testing.T) {
	h, err := New(Options{Router: &fakeRouter{result: okResult()}, MaxUploadMB: 1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Build a 2 MB fake "audio" base64 payload â€” well above the 1 MB limit.
	big := bytes.Repeat([]byte{0xAA}, 2<<20)
	body, _ := json.Marshal(map[string]string{
		"audio_base64": base64.StdEncoding.EncodeToString(big),
		"format":       "wav",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/dictation/transcribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

// â”€â”€ helpers â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

func mustHandler(t *testing.T, router Transcriber) *Handler {
	t.Helper()
	h, err := New(Options{Router: router, MaxUploadMB: 25})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func okResult() *stt.Result {
	return &stt.Result{
		Text:       "hello world",
		Language:   "en",
		Provider:   "fake",
		Model:      "fake-v1",
		Duration:   123 * time.Millisecond,
		Confidence: 0.95,
	}
}

func synthSine(rate, ch int, hz float64, ms int) []byte {
	frames := rate * ms / 1000
	out := make([]byte, frames*ch*2)
	for f := 0; f < frames; f++ {
		tSec := float64(f) / float64(rate)
		val := int16(math.Sin(2*math.Pi*hz*tSec) * 10000)
		for c := 0; c < ch; c++ {
			o := (f*ch + c) * 2
			binary.LittleEndian.PutUint16(out[o:], uint16(val))
		}
	}
	return out
}

func wrapWAV(pcm []byte, rate, channels int) []byte {
	dataSize := uint32(len(pcm))
	buf := make([]byte, 44+len(pcm))
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], 36+dataSize)
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1)
	binary.LittleEndian.PutUint16(buf[22:], uint16(channels))
	binary.LittleEndian.PutUint32(buf[24:], uint32(rate))
	binary.LittleEndian.PutUint32(buf[28:], uint32(rate*channels*2))
	binary.LittleEndian.PutUint16(buf[32:], uint16(channels*2))
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], dataSize)
	copy(buf[44:], pcm)
	return buf
}

var _ io.Reader = (*bytes.Reader)(nil) // keep io import if refactored
