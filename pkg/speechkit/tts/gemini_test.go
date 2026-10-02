package tts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

func newTestGeminiTTS(baseURL string, opts GeminiOpts) *Gemini {
	opts.APIKey = "test-key"
	g := NewGemini(opts)
	g.BaseURL = baseURL
	g.Validation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}
	return g
}

// geminiAudioResponse serves an interaction whose model_output step carries
// audio, the shape the Interactions API returns for a unary TTS call.
func geminiAudioResponse(w http.ResponseWriter, audio []byte, mimeType string) {
	_, _ = fmt.Fprintf(w, `{"id":"i1","status":"completed","steps":[{"type":"user_input","content":[{"type":"text","text":"x"}]},{"type":"model_output","content":[{"type":"audio","mime_type":%q,"data":%q}]}]}`,
		mimeType, base64.StdEncoding.EncodeToString(audio))
}

func TestGeminiSynthesizeSendsInteractionAndReturnsWAV(t *testing.T) {
	wav := encodeAuraWAV([]byte{1, 0, 2, 0, 3, 0})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1beta/interactions" {
			t.Errorf("request = %s %s, want POST /v1beta/interactions", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "test-key" {
			t.Errorf("x-goog-api-key = %q, want test-key", got)
		}
		var body struct {
			Model          string `json:"model"`
			Input          string `json:"input"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
			GenerationConfig struct {
				SpeechConfig []struct {
					Voice string `json:"voice"`
				} `json:"speech_config"`
			} `json:"generation_config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Model != GeminiTTSModelFlash || body.Input != "Hello there" || body.ResponseFormat.Type != "audio" {
			t.Errorf("body = %+v, want flash model, input text and audio response format", body)
		}
		if len(body.GenerationConfig.SpeechConfig) == 0 || body.GenerationConfig.SpeechConfig[0].Voice != "Puck" {
			t.Errorf("speech_config = %+v, want voice Puck", body.GenerationConfig.SpeechConfig)
		}
		geminiAudioResponse(w, wav, "audio/wav")
	}))
	defer srv.Close()

	g := newTestGeminiTTS(srv.URL, GeminiOpts{Model: GeminiTTSModelFlash})
	result, err := g.Synthesize(context.Background(), "Hello there", SynthesizeOpts{Voice: "Puck"})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if result.Format != "wav" || !bytes.Equal(result.Audio, wav) {
		t.Errorf("result = %s/%d bytes, want the returned WAV unchanged", result.Format, len(result.Audio))
	}
}

// Streaming-style responses carry raw L16 PCM rather than a RIFF file; the
// provider must still hand callers a playable WAV.
func TestGeminiSynthesizeWrapsRawPCMAsWAV(t *testing.T) {
	pcm := []byte{9, 0, 8, 0, 7, 0, 6, 0}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		geminiAudioResponse(w, pcm, "audio/l16")
	}))
	defer srv.Close()

	result, err := newTestGeminiTTS(srv.URL, GeminiOpts{}).Synthesize(context.Background(), "Hi", SynthesizeOpts{})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	data, ok := wavPCMData(result.Audio)
	if result.Format != "wav" || !ok || !bytes.Equal(data, pcm) {
		t.Errorf("result = %s, want a WAV whose data chunk is the returned PCM", result.Format)
	}
}
