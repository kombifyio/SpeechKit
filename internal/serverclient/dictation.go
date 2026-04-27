package serverclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/kombifyio/SpeechKit/internal/stt"
)

// DictationProvider implements stt.STTProvider against a remote
// SpeechKit Server-Target's POST /v1/dictation/transcribe endpoint. It is
// a drop-in replacement for any local stt.STTProvider in the kernel's
// router — when ModelSelection.Dictate.ModeSource = "server", the device
// app constructs one of these instead of the in-process whisper.cpp /
// HuggingFace / etc. provider.
type DictationProvider struct {
	client *Client
	// audioFormat is the format hint we send to the server. The kernel
	// always feeds us 16kHz S16 mono PCM (post-resample), so we declare
	// "pcm16" and the server's audio.Decode treats it as passthrough. If
	// future kernel paths feed us pre-encoded WAV/MP3, the field can be
	// switched per-request.
	audioFormat string
}

// NewDictation constructs a DictationProvider. Calling Transcribe sends
// the audio as base64-encoded PCM to the server so the server skips
// decoding and goes straight to its STT router.
func NewDictation(client *Client) (*DictationProvider, error) {
	if client == nil {
		return nil, errors.New("serverclient: NewDictation requires a non-nil Client")
	}
	return &DictationProvider{client: client, audioFormat: "pcm16"}, nil
}

// Name identifies this provider in router logs. The "server:" prefix makes
// it obvious in mixed-provider logs that the request was delegated.
func (p *DictationProvider) Name() string {
	return "server-dictation"
}

// Health calls the server's /readyz which only returns 200 when the STT
// router is wired and at least one provider is healthy.
func (p *DictationProvider) Health(ctx context.Context) error {
	req, err := p.client.newRequest(ctx, http.MethodGet, "/readyz", nil, "")
	if err != nil {
		return err
	}
	resp, err := p.client.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("serverclient: GET /readyz: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return decodeErrorEnvelope(resp, 4096)
	}
	return nil
}

// transcribeRequest mirrors the server's transcribeJSONRequest (private
// type in internal/server/dictation). Field names + tags must stay aligned
// with the server's wire contract, which is documented in
// docs/server/openapi.v1.yaml under POST /v1/dictation/transcribe.
type transcribeRequest struct {
	AudioBase64 string `json:"audio_base64"`
	Format      string `json:"format"`
	Language    string `json:"language,omitempty"`
	Model       string `json:"model,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

// transcribeResponse mirrors the server's wire response. Unmarshalling
// breaks if the server adds required fields, but the test suite catches
// this — see dictation_test.go.
type transcribeResponse struct {
	Text       string  `json:"text"`
	Language   string  `json:"language"`
	DurationMs int64   `json:"duration_ms"`
	LatencyMs  int64   `json:"latency_ms"`
	Provider   string  `json:"provider"`
	Model      string  `json:"model"`
	Confidence float64 `json:"confidence"`
}

// Transcribe sends audio bytes to the server and returns the transcription.
// audio is expected to be already-resampled 16kHz S16 mono PCM (kernel
// convention). Returns wrapped *ServerError on 4xx/5xx for callers that
// want to branch on error codes.
func (p *DictationProvider) Transcribe(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	if len(audio) == 0 {
		return nil, errors.New("serverclient: Transcribe called with empty audio")
	}
	body, err := json.Marshal(transcribeRequest{
		AudioBase64: base64.StdEncoding.EncodeToString(audio),
		Format:      p.audioFormat,
		Language:    opts.Language,
		Model:       opts.Model,
		Prompt:      opts.Prompt,
	})
	if err != nil {
		return nil, fmt.Errorf("serverclient: marshal request: %w", err)
	}

	req, err := p.client.newRequest(ctx, http.MethodPost, "/v1/dictation/transcribe",
		bytes.NewReader(body), "application/json")
	if err != nil {
		return nil, err
	}

	resp, err := p.client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serverclient: POST /v1/dictation/transcribe: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Wrap the server's error envelope so callers can inspect the code
		// without re-reading the body.
		return nil, decodeErrorEnvelope(resp, 16<<10)
	}

	var out transcribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("serverclient: decode response: %w", err)
	}
	return &stt.Result{
		Text:       out.Text,
		Language:   out.Language,
		Duration:   time.Duration(out.DurationMs) * time.Millisecond,
		Provider:   out.Provider,
		Model:      out.Model,
		Confidence: out.Confidence,
	}, nil
}

// Compile-time assertion that DictationProvider satisfies stt.STTProvider.
var _ stt.STTProvider = (*DictationProvider)(nil)
