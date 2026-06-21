//go:build linux

package ttsapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/tts"
)

// fakeProvider is a controllable tts.Provider for router-backed handler tests.
type fakeProvider struct {
	result *tts.Result
	err    error
	lastTx string
}

func (f *fakeProvider) Synthesize(_ context.Context, text string, _ tts.SynthesizeOpts) (*tts.Result, error) {
	f.lastTx = text
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func (f *fakeProvider) Name() string           { return "fake" }
func (f *fakeProvider) Kind() tts.ProviderKind { return tts.ProviderKindCloudProvider }
func (f *fakeProvider) Health(_ context.Context) error {
	return nil
}

func TestSynthesize_NonPOST_MethodNotAllowed(t *testing.T) {
	h := New(&config.Config{}, tts.NewRouter(tts.StrategyCloudFirst, &fakeProvider{}))
	rec := httptest.NewRecorder()
	h.synthesize(rec, httptest.NewRequest(http.MethodGet, "/v1/tts/synthesize", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestSynthesize_NilRouter_Unavailable(t *testing.T) {
	h := New(&config.Config{}, nil)
	rec := httptest.NewRecorder()
	h.synthesize(rec, httptest.NewRequest(http.MethodPost, "/v1/tts/synthesize", strings.NewReader(`{"text":"hi"}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSynthesize_MissingText_BadRequest(t *testing.T) {
	h := New(&config.Config{}, tts.NewRouter(tts.StrategyCloudFirst, &fakeProvider{}))
	rec := httptest.NewRecorder()
	h.synthesize(rec, httptest.NewRequest(http.MethodPost, "/v1/tts/synthesize", strings.NewReader(`{"text":"   "}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSynthesize_InvalidJSON_BadRequest(t *testing.T) {
	h := New(&config.Config{}, tts.NewRouter(tts.StrategyCloudFirst, &fakeProvider{}))
	rec := httptest.NewRecorder()
	h.synthesize(rec, httptest.NewRequest(http.MethodPost, "/v1/tts/synthesize", strings.NewReader(`{bad`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSynthesize_RouterError_Unavailable(t *testing.T) {
	provider := &fakeProvider{err: errors.New("provider boom")}
	h := New(&config.Config{}, tts.NewRouter(tts.StrategyCloudFirst, provider))
	rec := httptest.NewRecorder()
	h.synthesize(rec, httptest.NewRequest(http.MethodPost, "/v1/tts/synthesize", strings.NewReader(`{"text":"hello"}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSynthesize_Success(t *testing.T) {
	provider := &fakeProvider{result: &tts.Result{
		Audio:      []byte("RIFFsynthetic"),
		Format:     "wav",
		SampleRate: 24000,
		Duration:   1500 * time.Millisecond,
		Provider:   "fake",
		Voice:      "nova",
	}}
	h := New(&config.Config{}, tts.NewRouter(tts.StrategyCloudFirst, provider))

	rec := httptest.NewRecorder()
	h.synthesize(rec, httptest.NewRequest(http.MethodPost, "/v1/tts/synthesize", strings.NewReader(`{"text":"hello world","format":"wav"}`)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		AudioBase64 string `json:"audio_base64"`
		Format      string `json:"format"`
		SampleRate  int    `json:"sample_rate"`
		DurationMS  int64  `json:"duration_ms"`
		Provider    string `json:"provider"`
		Voice       string `json:"voice"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(body.AudioBase64)
	if err != nil || string(decoded) != "RIFFsynthetic" {
		t.Fatalf("audio_base64 roundtrip failed: err=%v decoded=%q", err, decoded)
	}
	if body.Format != "wav" || body.SampleRate != 24000 || body.DurationMS != 1500 || body.Provider != "fake" || body.Voice != "nova" {
		t.Fatalf("result fields mismatch: %+v", body)
	}
	if provider.lastTx != "hello world" {
		t.Fatalf("provider received text = %q, want trimmed input", provider.lastTx)
	}
}

func TestVoices_GET_ReflectsEnabledProviders(t *testing.T) {
	cfg := &config.Config{}
	cfg.TTS.OpenAI.Enabled = true
	cfg.TTS.OpenAI.Voice = "shimmer"
	cfg.TTS.Google.Enabled = true

	h := New(cfg, nil)
	rec := httptest.NewRecorder()
	h.voices(rec, httptest.NewRequest(http.MethodGet, "/v1/tts/voices", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Voices []map[string]any `json:"voices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Voices) != 2 {
		t.Fatalf("want 2 enabled voices, got %d: %+v", len(body.Voices), body.Voices)
	}
	if body.Voices[0]["provider"] != "openai" || body.Voices[0]["id"] != "shimmer" {
		t.Errorf("first voice mismatch: %+v", body.Voices[0])
	}
}

func TestVoices_NonGET_MethodNotAllowed(t *testing.T) {
	h := New(&config.Config{}, nil)
	rec := httptest.NewRecorder()
	h.voices(rec, httptest.NewRequest(http.MethodPost, "/v1/tts/voices", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
