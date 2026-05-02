package stt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/kombifyio/SpeechKit/internal/netsec"
)

const (
	openRouterSTTBaseURL     = "https://openrouter.ai/api/v1"
	openRouterDefaultSTT     = "openai/whisper-1"
	openRouterMaxResponse    = 1 << 20
	openRouterAudioFormatWAV = "wav"
)

// OpenRouterSTTProvider implements OpenRouter's JSON speech-to-text endpoint.
// OpenRouter is a cloud gateway, not a direct model provider, and its STT API
// accepts base64 audio rather than OpenAI's multipart Whisper shape.
type OpenRouterSTTProvider struct {
	BaseURL    string
	APIKey     string
	Model      string
	Validation netsec.ValidationOptions
	client     *http.Client
}

func NewOpenRouterSTTProvider(apiKey, model string) *OpenRouterSTTProvider {
	if model == "" {
		model = openRouterDefaultSTT
	}
	p := &OpenRouterSTTProvider{
		BaseURL: openRouterSTTBaseURL,
		APIKey:  apiKey,
		Model:   model,
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second, DialValidation: &p.Validation})
	return p
}

type openRouterTranscriptionRequest struct {
	InputAudio openRouterInputAudio `json:"input_audio"`
	Model      string               `json:"model"`
	Language   string               `json:"language,omitempty"`
}

type openRouterInputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

type openRouterTranscriptionResponse struct {
	Text string `json:"text"`
}

func (p *OpenRouterSTTProvider) endpoint(path string) (string, error) {
	baseURL := p.BaseURL
	if baseURL == "" {
		baseURL = openRouterSTTBaseURL
	}
	return netsec.BuildEndpoint(baseURL, path, p.Validation)
}

func (p *OpenRouterSTTProvider) Transcribe(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error) {
	endpoint, err := p.endpoint("audio/transcriptions")
	if err != nil {
		return nil, fmt.Errorf("openrouter endpoint: %w", err)
	}

	model := p.Model
	if opts.Model != "" {
		model = opts.Model
	}
	requestBody := openRouterTranscriptionRequest{
		InputAudio: openRouterInputAudio{
			Data:   base64.StdEncoding.EncodeToString(ensureTranscriptionWAV(audio)),
			Format: openRouterAudioFormatWAV,
		},
		Model: model,
	}
	if opts.Language != "" && opts.Language != "auto" {
		requestBody.Language = opts.Language
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal openrouter request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create openrouter request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	duration := time.Since(start)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, openRouterMaxResponse))
	if err != nil {
		return nil, fmt.Errorf("read openrouter response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, netsec.ProviderStatusError("openrouter", resp.StatusCode, respBody)
	}

	var result openRouterTranscriptionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse openrouter response: %w", err)
	}

	lang := opts.Language
	if lang == "" {
		lang = "de"
	}

	return &Result{
		Text:     result.Text,
		Language: lang,
		Duration: duration,
		Provider: p.Name(),
		Model:    model,
	}, nil
}

func (p *OpenRouterSTTProvider) Name() string {
	return "openrouter"
}

func (p *OpenRouterSTTProvider) Health(ctx context.Context) error {
	endpoint, err := p.endpoint("models")
	if err != nil {
		return fmt.Errorf("openrouter endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("openrouter health: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openrouter health: status %d", resp.StatusCode)
	}
	return nil
}
