package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/netsec"
)

const openAICompatMaxResponse = 1 << 20

// OpenAICompatibleProvider implements STTProvider for any endpoint speaking
// the OpenAI /v1/audio/transcriptions API (OpenAI, Groq, VPS whisper-server, etc.).
//
// BaseURL is user-supplied configuration. It is validated against Validation
// on every request (Transcribe, Health). The default Validation is strict:
// only public https:// endpoints are accepted. Self-hosted VPS and local
// whisper-server require relaxing Validation — see NewVPSProvider.
type OpenAICompatibleProvider struct {
	name       string
	BaseURL    string
	APIKey     string
	Model      string
	Validation netsec.ValidationOptions
	client     *http.Client
}

// NewOpenAICompatibleProvider creates a provider for any OpenAI-compatible STT
// endpoint. Default Validation is strict (public https only). Callers with a
// non-public endpoint (loopback, RFC1918) must set Validation explicitly.
func NewOpenAICompatibleProvider(name, baseURL, apiKey, model string) *OpenAICompatibleProvider {
	return &OpenAICompatibleProvider{
		name:    name,
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		// Validation zero-value = strict: public https only, no loopback, no private IPs.
		client: netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second}),
	}
}

// NewVPSProvider creates a provider for a self-hosted whisper-server.
// Allows loopback, private IP ranges and plain http:// because self-hosted
// deployments frequently run inside a VPN, on a home LAN, or on localhost.
func NewVPSProvider(baseURL, apiKey string) *OpenAICompatibleProvider {
	p := NewOpenAICompatibleProvider("vps", baseURL, apiKey, "whisper-1")
	p.Validation = netsec.ValidationOptions{
		AllowLoopback: true,
		AllowPrivate:  true,
		AllowHTTP:     true,
	}
	// Rebuild the HTTP client with a longer timeout — self-hosted whisper
	// may take longer to cold-start than managed cloud APIs.
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 60 * time.Second})
	return p
}

// NewOllamaSTTProvider creates a provider for Ollama-compatible local
// transcription endpoints. Ollama runs on loopback by default and can be
// pointed at a user-managed self-hosted URL.
func NewOllamaSTTProvider(baseURL, model string) *OpenAICompatibleProvider {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "gemma4:e4b"
	}
	p := NewOpenAICompatibleProvider("ollama", baseURL, "", model)
	p.Validation = netsec.ValidationOptions{
		AllowLoopback: true,
		AllowPrivate:  true,
		AllowHTTP:     true,
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 60 * time.Second})
	return p
}

// NewOpenAISTTProvider creates a provider for the OpenAI Whisper API.
func NewOpenAISTTProvider(apiKey string) *OpenAICompatibleProvider {
	return NewOpenAICompatibleProvider("openai", "https://api.openai.com", apiKey, "whisper-1")
}

// NewGroqSTTProvider creates a provider for the Groq Whisper API.
func NewGroqSTTProvider(apiKey string) *OpenAICompatibleProvider {
	return NewOpenAICompatibleProvider("groq", "https://api.groq.com/openai", apiKey, "whisper-large-v3-turbo")
}

// Transcribe sends audio to the OpenAI-compatible /v1/audio/transcriptions endpoint.
func (p *OpenAICompatibleProvider) Transcribe(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error) {
	endpoint, err := netsec.BuildEndpoint(p.BaseURL, "v1/audio/transcriptions", p.Validation)
	if err != nil {
		return nil, fmt.Errorf("%s endpoint: %w", p.name, err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return nil, fmt.Errorf("write audio data: %w", err)
	}

	if opts.Language != "" && opts.Language != "auto" {
		if err := writer.WriteField("language", opts.Language); err != nil {
			return nil, fmt.Errorf("write language field: %w", err)
		}
	}

	model := p.Model
	if opts.Model != "" {
		model = opts.Model
	}
	if err := writer.WriteField("model", model); err != nil {
		return nil, fmt.Errorf("write model field: %w", err)
	}
	if opts.Prompt != "" {
		if err := writer.WriteField("prompt", opts.Prompt); err != nil {
			return nil, fmt.Errorf("write prompt field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s request: %w", p.name, err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	duration := time.Since(start)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, openAICompatMaxResponse))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s error (%d): %s", p.name, resp.StatusCode, string(respBody))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
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

// Name returns the provider identifier.
func (p *OpenAICompatibleProvider) Name() string {
	return p.name
}

// Health checks provider reachability. Tries GET /health first (whisper-server),
// then falls back to GET /v1/models (OpenAI, Groq).
func (p *OpenAICompatibleProvider) Health(ctx context.Context) error {
	healthURL, err := netsec.BuildEndpoint(p.BaseURL, "health", p.Validation)
	if err != nil {
		return fmt.Errorf("%s endpoint: %w", p.name, err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s health: %w", p.name, err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// Fallback: try /v1/models (OpenAI, Groq don't have /health).
	modelsURL, err := netsec.BuildEndpoint(p.BaseURL, "v1/models", p.Validation)
	if err != nil {
		return fmt.Errorf("%s endpoint: %w", p.name, err)
	}

	req, err = http.NewRequestWithContext(ctx, "GET", modelsURL, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err = p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s health: %w", p.name, err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s health: status %d", p.name, resp.StatusCode)
	}
	return nil
}
