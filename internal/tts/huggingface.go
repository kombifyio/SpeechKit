package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	hfTTSEndpoint     = "https://router.huggingface.co/hf-inference/models/"
	hfDefaultTTSModel = "parler-tts/parler-tts-mini-multilingual-v1.1"
	hfTTSSampleRate   = 24000
	hfMaxResponseBody = 50 * 1024 * 1024 // 50 MB
)

// HuggingFace implements Provider using the HuggingFace Inference API
// with text-to-speech models (e.g. parler-tts).
type HuggingFace struct {
	token  string
	model  string
	client *http.Client
}

// HuggingFaceOpts configures the HuggingFace TTS provider.
type HuggingFaceOpts struct {
	Token string // HF API token
	Model string // Model ID, e.g. "parler-tts/parler-tts-mini-multilingual-v1.1"
}

// NewHuggingFace creates a HuggingFace TTS provider.
func NewHuggingFace(opts HuggingFaceOpts) *HuggingFace {
	model := opts.Model
	if model == "" {
		model = hfDefaultTTSModel
	}
	return &HuggingFace{
		token:  opts.Token,
		model:  model,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// hfTTSRequest is the JSON body for the HF Inference API text-to-speech task.
type hfTTSRequest struct {
	Inputs     string            `json:"inputs"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

// voiceDescriptionForLocale returns a Parler-TTS style voice description prompt.
// Parler-TTS uses natural language descriptions to control voice characteristics.
func voiceDescriptionForLocale(locale string) string {
	switch locale {
	case "de", "de-DE":
		return "A clear female voice with a moderate pace, speaking in German."
	case "fr", "fr-FR":
		return "A clear female voice with a moderate pace, speaking in French."
	case "es", "es-ES":
		return "A clear female voice with a moderate pace, speaking in Spanish."
	default:
		return "A clear female voice with a moderate pace."
	}
}

func (h *HuggingFace) Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (*Result, error) {
	if text == "" {
		return nil, fmt.Errorf("huggingface tts: empty text")
	}

	reqBody := hfTTSRequest{
		Inputs: text,
	}

	// Parler-TTS accepts a voice description via the "generate_kwargs" or
	// directly as a description parameter. The HF Inference API for
	// parler-tts expects description in parameters.
	if opts.Locale != "" && opts.Locale != "auto" {
		desc := voiceDescriptionForLocale(opts.Locale)
		reqBody.Parameters = map[string]string{
			"description": desc,
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("huggingface tts: marshal: %w", err)
	}

	endpoint := hfTTSEndpoint + h.model
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("huggingface tts: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("huggingface tts: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("huggingface tts: HTTP %d: %s", resp.StatusCode, string(errBody))
	}

	// HF Inference API returns raw audio bytes (FLAC by default for TTS models).
	audio, err := io.ReadAll(io.LimitReader(resp.Body, hfMaxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("huggingface tts: read response: %w", err)
	}

	// Detect format from Content-Type header.
	format := "flac"
	ct := resp.Header.Get("Content-Type")
	switch {
	case ct == "audio/wav" || ct == "audio/x-wav":
		format = "wav"
	case ct == "audio/mpeg":
		format = "mp3"
	case ct == "audio/ogg":
		format = "ogg"
	}

	return &Result{
		Audio:      audio,
		Format:     format,
		SampleRate: hfTTSSampleRate,
		Provider:   "huggingface",
		Voice:      h.model,
	}, nil
}

func (h *HuggingFace) Name() string { return "huggingface" }

func (h *HuggingFace) Health(ctx context.Context) error {
	if h.token == "" {
		return fmt.Errorf("huggingface tts: no token configured")
	}
	return nil
}
