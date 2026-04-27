package serverclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	assistpkg "github.com/kombifyio/SpeechKit/internal/assist"
)

// AssistProcessor implements the assist.Processor interface (the same
// surface internal/server/assist.Handler depends on) against a remote
// SpeechKit server's POST /v1/assist/process endpoint. The kernel-side
// caller doesn't know whether the pipeline ran in-process or hopped a
// network â€” the abstract type is preserved.
type AssistProcessor struct {
	client *Client
}

// NewAssist constructs an AssistProcessor.
func NewAssist(client *Client) (*AssistProcessor, error) {
	if client == nil {
		return nil, errors.New("serverclient: NewAssist requires a non-nil Client")
	}
	return &AssistProcessor{client: client}, nil
}

// processRequest mirrors the server's processJSONRequest. The text path
// is the only one wired right now â€” Assist on the device side already
// runs STT locally before reaching the Pipeline, so the server adapter
// also enters at the post-STT stage. If a future device path wants to
// hand audio directly to the server (skipping local STT entirely), an
// AudioBase64 field can be added without breaking callers.
type processRequest struct {
	Text      string  `json:"text,omitempty"`
	Locale    string  `json:"locale,omitempty"`
	Selection string  `json:"selection,omitempty"`
	Context   string  `json:"context,omitempty"`
	TTS       *bool   `json:"tts,omitempty"`
	TTSFormat string  `json:"tts_format,omitempty"`
	TTSVoice  string  `json:"tts_voice,omitempty"`
	TTSSpeed  float64 `json:"tts_speed,omitempty"`
}

// processResponse mirrors the server's wire response shape.
type processResponse struct {
	Text        string `json:"text"`
	SpeakText   string `json:"speak_text"`
	Action      string `json:"action"`
	Locale      string `json:"locale"`
	Shortcut    string `json:"shortcut"`
	Surface     string `json:"surface"`
	Kind        string `json:"kind"`
	Transcript  string `json:"transcript"`
	AudioBase64 string `json:"audio_base64"`
	AudioFormat string `json:"audio_format"`
	LatencyMs   int64  `json:"latency_ms"`
}

// Process implements assistpkg.Processor by forwarding the transcript +
// options to the server. Returned *assistpkg.Result mirrors the wire
// response, decoding base64 audio back to bytes when present.
func (p *AssistProcessor) Process(ctx context.Context, transcript string, opts assistpkg.ProcessOpts) (*assistpkg.Result, error) {
	body, err := json.Marshal(processRequest{
		Text:      transcript,
		Locale:    opts.Locale,
		Selection: opts.Selection,
		Context:   opts.Context,
	})
	if err != nil {
		return nil, fmt.Errorf("serverclient: marshal assist request: %w", err)
	}

	req, err := p.client.newRequest(ctx, http.MethodPost, "/v1/assist/process",
		bytes.NewReader(body), "application/json")
	if err != nil {
		return nil, err
	}

	resp, err := p.client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serverclient: POST /v1/assist/process: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeErrorEnvelope(resp, 16<<10)
	}

	var out processResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("serverclient: decode assist response: %w", err)
	}
	result := &assistpkg.Result{
		Text:      out.Text,
		SpeakText: out.SpeakText,
		Action:    out.Action,
		Locale:    out.Locale,
		Shortcut:  out.Shortcut,
		Format:    out.AudioFormat,
	}
	if surface := assistpkg.ResultSurface(out.Surface); surface != "" {
		result.Surface = surface
	}
	if out.AudioBase64 != "" {
		audio, decErr := base64.StdEncoding.DecodeString(out.AudioBase64)
		if decErr != nil {
			return nil, fmt.Errorf("serverclient: decode assist audio: %w", decErr)
		}
		result.Audio = audio
	}
	return result, nil
}

// Compile-time assertion: AssistProcessor satisfies the same shape the
// kernel-side assist Handler depends on. We re-declare the surface here
// rather than importing the server package (which is //go:build linux)
// so the assertion compiles on every OS the device-target ships on.
var _ assistProcessorSurface = (*AssistProcessor)(nil)

type assistProcessorSurface interface {
	Process(ctx context.Context, transcript string, opts assistpkg.ProcessOpts) (*assistpkg.Result, error)
}
