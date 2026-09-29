//go:build linux

package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/dictation"
	"github.com/kombifyio/SpeechKit/internal/server/ttsapi"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

type openAICompatTranscriber struct {
	pcmBytes int
	language string
}

func (f *openAICompatTranscriber) Route(_ context.Context, pcm []byte, _ float64, opts stt.TranscribeOpts) (*stt.Result, error) {
	f.pcmBytes = len(pcm)
	f.language = opts.Language
	return &stt.Result{Text: "hello world", Provider: "fake-local"}, nil
}

type openAICompatTTS struct{ calls int }

func (p *openAICompatTTS) Synthesize(context.Context, string, tts.SynthesizeOpts) (*tts.Result, error) {
	p.calls++
	return &tts.Result{Audio: []byte("RIFF-fake-wav"), Format: "wav", Provider: "piper"}, nil
}
func (*openAICompatTTS) Name() string                 { return "piper" }
func (*openAICompatTTS) Kind() tts.ProviderKind       { return tts.ProviderKindLocalBuiltIn }
func (*openAICompatTTS) Health(context.Context) error { return nil }

// Open WebUI (AUDIO_STT_ENGINE=openai / AUDIO_TTS_ENGINE=openai) calls these
// two routes with the server bearer token; the requests below mirror what its
// v0.11.3 audio router sends.
func TestOpenAIAudioCompat_OpenWebUIRequestsBehindBearerAuth(t *testing.T) {
	transcriber := &openAICompatTranscriber{}
	speaker := &openAICompatTTS{}
	handler := newAdapterTestHandler(t, func(mux *http.ServeMux) {
		h, err := dictation.New(dictation.Options{Router: transcriber})
		if err != nil {
			t.Fatalf("dictation handler: %v", err)
		}
		h.MountOpenAICompat(mux)
		ttsapi.New(&config.Config{}, tts.NewRouter(tts.StrategyLocalOnly, speaker)).MountOpenAICompat(mux)
	})

	fixture := openAICompatWAV(16000, 16000) // 1 s of 16 kHz mono PCM16
	transcriptionRequest := func() *http.Request {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		_ = form.WriteField("model", "")
		_ = form.WriteField("language", "en")
		// CreateFormFile tags the part application/octet-stream, as Open
		// WebUI's streamed upload does; the extension carries the format.
		part, _ := form.CreateFormFile("file", "0b7c.wav")
		_, _ = part.Write(fixture)
		_ = form.Close()
		req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		return req
	}
	speechRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/v1/audio/speech",
			strings.NewReader(`{"model":"tts-1","input":"Hello there.","voice":"alloy"}`))
		req.Header.Set("Content-Type", "application/json")
		return req
	}
	serve := func(req *http.Request, bearer bool) *httptest.ResponseRecorder {
		if bearer {
			req.Header.Set("Authorization", "Bearer adapter-test-token")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	t.Run("unauthenticated requests are rejected", func(t *testing.T) {
		for _, req := range []*http.Request{transcriptionRequest(), speechRequest()} {
			if rec := serve(req, false); rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s without bearer = %d, want 401", req.URL.Path, rec.Code)
			}
		}
		if transcriber.pcmBytes != 0 || speaker.calls != 0 {
			t.Fatal("unauthenticated request reached a provider")
		}
	})

	t.Run("transcription decodes the upload and returns OpenAI json", func(t *testing.T) {
		rec := serve(transcriptionRequest(), true)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var got struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Text != "hello world" {
			t.Fatalf("body = %s (err %v), want text \"hello world\"", rec.Body.String(), err)
		}
		// The kernel must receive the decoded samples, not the RIFF container
		// misread as raw PCM (the octet-stream part type alone would do that).
		if transcriber.pcmBytes != 32000 || transcriber.language != "en" {
			t.Fatalf("kernel got %d PCM bytes language %q from a %d-byte WAV", transcriber.pcmBytes, transcriber.language, len(fixture))
		}
	})

	t.Run("speech returns raw audio with its real content type", func(t *testing.T) {
		rec := serve(speechRequest(), true)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "audio/wav" {
			t.Fatalf("Content-Type = %q, want audio/wav", ct)
		}
		if rec.Body.String() != "RIFF-fake-wav" {
			t.Fatalf("body = %q, want provider audio bytes", rec.Body.String())
		}
	})
}

// openAICompatWAV builds a PCM16 mono WAV with a quiet square wave.
func openAICompatWAV(sampleRate, samples int) []byte {
	var buf bytes.Buffer
	dataLen := samples * 2
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+dataLen))
	buf.WriteString("WAVEfmt ")
	for _, field := range []any{uint32(16), uint16(1), uint16(1), uint32(sampleRate), uint32(sampleRate * 2), uint16(2), uint16(16)} {
		_ = binary.Write(&buf, binary.LittleEndian, field)
	}
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(dataLen))
	for i := 0; i < samples; i++ {
		v := int16(1000)
		if (i/40)%2 == 0 {
			v = -1000
		}
		_ = binary.Write(&buf, binary.LittleEndian, v)
	}
	return buf.Bytes()
}
