package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

const (
	foundryTTSPath       = "v1/audio/speech"
	foundryDefaultModel  = "gpt-4o-mini-tts"
	foundryDefaultVoice  = "alloy"
	foundryDefaultFormat = "mp3"
)

// Foundry implements Provider using the Microsoft Foundry OpenAI-compatible
// audio/speech API. BaseURL is the OpenAI-compatible base derived from the
// project endpoint (https://<host>/openai); the model is the deployment name.
//
// Foundry's v1 surface accepts Bearer auth, so the API key rides the standard
// Authorization header. Validation is strict by default (public https only).
type Foundry struct {
	apiKey     string
	model      string
	voice      string
	BaseURL    string
	Validation netsec.ValidationOptions
	client     *http.Client
}

// FoundryOpts configures the Microsoft Foundry TTS provider.
type FoundryOpts struct {
	APIKey  string
	BaseURL string // OpenAI-compatible base, e.g. https://<host>/openai (required)
	Model   string // deployment name; defaults to "gpt-4o-mini-tts"
	Voice   string // alloy, echo, fable, onyx, nova, shimmer; defaults to "alloy"
}

// NewFoundry creates a Microsoft Foundry TTS provider.
func NewFoundry(opts FoundryOpts) *Foundry {
	model := opts.Model
	if model == "" {
		model = foundryDefaultModel
	}
	voice := opts.Voice
	if voice == "" {
		voice = foundryDefaultVoice
	}
	p := &Foundry{
		apiKey:  opts.APIKey,
		model:   model,
		voice:   voice,
		BaseURL: opts.BaseURL,
		// Validation zero-value = strict: public https only.
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second, DialValidation: &p.Validation})
	return p
}

func (f *Foundry) Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (*Result, error) {
	if text == "" {
		return nil, fmt.Errorf("foundry tts: empty text")
	}
	if f.BaseURL == "" {
		return nil, fmt.Errorf("foundry tts: no endpoint configured (set the project endpoint)")
	}
	endpoint, err := netsec.BuildEndpoint(f.BaseURL, foundryTTSPath, f.Validation)
	if err != nil {
		return nil, fmt.Errorf("foundry tts: endpoint: %w", err)
	}

	resolved := ResolveSynthesizeOptions("foundry", "", opts, provideropts.Values{
		provideropts.OptionVoice:       f.voice,
		provideropts.OptionSpeed:       1.0,
		provideropts.OptionAudioFormat: foundryDefaultFormat,
	}, nil)

	voice := resolved.Voice

	format := resolved.Format
	if format == "" {
		format = foundryDefaultFormat
	}
	// Map generic formats to the OpenAI-compatible values Foundry accepts.
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

	speed := resolved.Speed
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
		Model:          f.model,
		Input:          text,
		Voice:          voice,
		ResponseFormat: responseFormat,
		Speed:          speed,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("foundry tts: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("foundry tts: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+f.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("foundry tts: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, netsec.ProviderStatusError("foundry tts", resp.StatusCode, errBody)
	}

	const maxAudioSize = 50 * 1024 * 1024 // 50 MB
	audio, err := io.ReadAll(io.LimitReader(resp.Body, maxAudioSize))
	if err != nil {
		return nil, fmt.Errorf("foundry tts: read response: %w", err)
	}

	return &Result{
		Audio:      audio,
		Format:     format,
		SampleRate: openAISampleRateMP3,
		Provider:   "foundry",
		Voice:      voice,
	}, nil
}

func (f *Foundry) Name() string { return "foundry" }

func (f *Foundry) Kind() ProviderKind { return ProviderKindDirectProvider }

func (f *Foundry) CloseIdleConnections() {
	if f != nil && f.client != nil {
		f.client.CloseIdleConnections()
	}
}

func (f *Foundry) Health(ctx context.Context) error {
	if f.apiKey == "" {
		return fmt.Errorf("foundry tts: no API key configured")
	}
	if f.BaseURL == "" {
		return fmt.Errorf("foundry tts: no endpoint configured")
	}
	// Lightweight check: synthesize a tiny text.
	_, err := f.Synthesize(ctx, "ok", SynthesizeOpts{Format: "mp3"})
	if err != nil {
		return fmt.Errorf("foundry tts: health check failed: %w", err)
	}
	return nil
}
