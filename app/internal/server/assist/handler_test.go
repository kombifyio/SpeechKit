//go:build linux

package assist

import (
	"bytes"
	"encoding/json"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── tests ───────────────────────────────────────────────────────────────────

func TestHandler_New_RejectsNilProcessor(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatalf("expected error when processor is nil")
	}
}

func TestHandler_JSON_WindowContextForwarded(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	h := mustHandler(t, Options{Processor: fp, DefaultLocale: "en"})

	body, _ := json.Marshal(map[string]any{
		"text":         "summarize this",
		"locale":       "en",
		"app":          "chrome",
		"window_title": "Inbox (2) - Gmail",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	want := "Active application: chrome\nActive window: Inbox (2) - Gmail"
	if fp.lastReq.Context != want {
		t.Fatalf("request context = %q, want the composed window block %q", fp.lastReq.Context, want)
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
	result := okAssistResult()
	result.MessageID = localization.CompanionHomeAssistantUnavailable
	result.ReasonCode = "unavailable"
	fp := &fakeProcessor{result: result}
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
	if fp.lastReq.Locale != "en" {
		t.Fatalf("locale = %q", fp.lastReq.Locale)
	}

	var resp struct {
		Text        string                 `json:"text"`
		Action      string                 `json:"action"`
		Locale      string                 `json:"locale"`
		Transcript  string                 `json:"transcript"`
		AudioBase64 string                 `json:"audio_base64"`
		AudioFormat string                 `json:"audio_format"`
		LatencyMs   int64                  `json:"latency_ms"`
		MessageID   localization.MessageID `json:"message_id"`
		ReasonCode  string                 `json:"reason_code"`
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
	if resp.MessageID != localization.CompanionHomeAssistantUnavailable || resp.ReasonCode != "unavailable" {
		t.Fatalf("response metadata = %q/%q", resp.MessageID, resp.ReasonCode)
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

func TestHandler_JSON_AudioInputSpeakerOptions(t *testing.T) {
	fp := &fakeProcessor{result: okAssistResult()}
	ft := &fakeTranscriber{result: &stt.Result{
		Text:     "hello there",
		Language: "en",
		Provider: "fake",
		Speakers: &speaker.DiarizationResult{
			Provider: "fake",
			Level:    speaker.IdentificationDiarization,
			Segments: []speaker.SpeakerSegment{{Text: "hello there", SpeakerLabel: "speaker_0"}},
		},
	}}
	h := mustHandler(t, Options{Processor: fp, Transcriber: ft, DefaultLocale: "en"})

	pcm := synthSine(16000, 100)
	wav := wrapWAV(pcm, 16000)
	body, _ := json.Marshal(map[string]any{
		"audio_base64": base64Encode(wav),
		"format":       "wav",
		"locale":       "en",
		"speaker": map[string]any{
			"diarization":         true,
			"minSpeakersExpected": 2,
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/assist/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !ft.lastOpts.Speaker.WantsDiarization() {
		t.Fatalf("speaker options not forwarded: %+v", ft.lastOpts.Speaker)
	}
	if !strings.Contains(fp.lastReq.Context, "Speaker transcript") {
		t.Fatalf("speaker context not appended: %q", fp.lastReq.Context)
	}
	var resp struct {
		Speakers *speaker.DiarizationResult `json:"speakers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Speakers == nil || len(resp.Speakers.Segments) != 1 {
		t.Fatalf("speakers = %+v", resp.Speakers)
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
