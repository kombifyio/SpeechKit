package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/kombifyio/SpeechKit/internal/netsec"
)

const (
	openAITTSBaseURL     = "https://api.openai.com"
	openAITTSPath        = "v1/audio/speech"
	openAIDefaultModel   = "tts-1"
	openAIDefaultVoice   = "nova"
	openAIDefaultFormat  = "mp3"
	openAISampleRateMP3  = 24000
	openAISampleRateOpus = 24000
)

// OpenAI implements Provider using the OpenAI TTS API.
//
// BaseURL is configurable for testing. It is validated against Validation
// on every request. Default Validation is strict (public https only).
type OpenAI struct {
	apiKey     string
	model      string
	voice      string
	BaseURL    string
	Validation netsec.ValidationOptions
	client     *http.Client
}

// OpenAIOpts configures the OpenAI TTS provider.
type OpenAIOpts struct {
	APIKey string
	Model  string // "tts-1" or "tts-1-hd"
	Voice  string // alloy, echo, fable, onyx, nova, shimmer
}

// NewOpenAI creates an OpenAI TTS provider.
func NewOpenAI(opts OpenAIOpts) *OpenAI {
	model := opts.Model
	if model == "" {
		model = openAIDefaultModel
	}
	voice := opts.Voice
	if voice == "" {
		voice = openAIDefaultVoice
	}
	p := &OpenAI{
		apiKey:  opts.APIKey,
		model:   model,
		voice:   voice,
		BaseURL: openAITTSBaseURL,
		// Validation zero-value = strict: public https only.
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second, DialValidation: &p.Validation})
	return p
}

type openAIRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"`
	Speed          float64 `json:"speed,omitempty"`
}

func (o *OpenAI) Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (*Result, error) {
	if text == "" {
		return nil, fmt.Errorf("openai tts: empty text")
	}

	base := o.BaseURL
	if base == "" {
		base = openAITTSBaseURL
	}
	endpoint, err := netsec.BuildEndpoint(base, openAITTSPath, o.Validation)
	if err != nil {
		return nil, fmt.Errorf("openai tts: endpoint: %w", err)
	}

	voice := opts.Voice
	if voice == "" {
		voice = o.voice
	}

	format := opts.Format
	if format == "" {
		format = openAIDefaultFormat
	}
	// Map generic formats to OpenAI-supported values.
	var responseFormat string
	switch format {
	case "wav":
		responseFormat = "wav"
	case "mp3":
		responseFormat = "mp3"
	case "opus":
		responseFormat = "opus"
	case "pcm":
		responseFormat = "pcm"
	default:
		responseFormat = "mp3"
		format = "mp3"
	}

	speed := opts.Speed
	if speed <= 0 {
		speed = 1.0
	}
	if speed < 0.25 {
		speed = 0.25
	}
	if speed > 4.0 {
		speed = 4.0
	}

	reqBody := openAIRequest{
		Model:          o.model,
		Input:          text,
		Voice:          voice,
		ResponseFormat: responseFormat,
		Speed:          speed,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai tts: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("openai tts: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai tts: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, netsec.ProviderStatusError("openai tts", resp.StatusCode, errBody)
	}

	const maxAudioSize = 50 * 1024 * 1024 // 50 MB
	audio, err := io.ReadAll(io.LimitReader(resp.Body, maxAudioSize))
	if err != nil {
		return nil, fmt.Errorf("openai tts: read response: %w", err)
	}

	sampleRate := openAISampleRateMP3
	if format == "pcm" {
		sampleRate = 24000
	}

	return &Result{
		Audio:      audio,
		Format:     format,
		SampleRate: sampleRate,
		Provider:   "openai",
		Voice:      voice,
	}, nil
}

func (o *OpenAI) Name() string { return "openai" }

func (o *OpenAI) Health(ctx context.Context) error {
	if o.apiKey == "" {
		return fmt.Errorf("openai tts: no API key configured")
	}
	// Lightweight check: synthesize a tiny text.
	_, err := o.Synthesize(ctx, "ok", SynthesizeOpts{Format: "mp3"})
	if err != nil {
		return fmt.Errorf("openai tts: health check failed: %w", err)
	}
	return nil
}
