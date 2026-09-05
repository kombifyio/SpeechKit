package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

const openAICompatMaxResponse = 1 << 20

// Provider implements stt.STTProvider for any endpoint speaking
// the OpenAI /v1/audio/transcriptions API (OpenAI, Groq, VPS whisper-server, etc.).
//
// BaseURL is user-supplied configuration. It is validated against Validation
// on every request (Transcribe, Health). The default Validation is strict:
// only public https:// endpoints are accepted. Self-hosted VPS and local
// whisper-server require relaxing Validation — see pkg/speechkit/stt/vps.
type Provider struct {
	name    string
	BaseURL string
	APIKey  string
	// BearerToken, when set, wins over APIKey: it is called per request and
	// the token rides "Authorization: Bearer". Hosts set it when the endpoint
	// authenticates with an identity provider (Microsoft Entra on Foundry)
	// instead of a static key.
	BearerToken speechkit.BearerTokenFunc
	Model       string
	Validation  netsec.ValidationOptions
	client      *http.Client
}

// authorize attaches the credential: a freshly minted bearer token when a
// token source is configured, otherwise the static API key.
func (p *Provider) authorize(ctx context.Context, req *http.Request) error {
	if p.BearerToken != nil {
		token, err := p.BearerToken(ctx)
		if err != nil {
			return fmt.Errorf("%s: bearer token: %w", p.name, err)
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	return nil
}

// New creates a provider for any OpenAI-compatible STT
// endpoint. Default Validation is strict (public https only). Callers with a
// non-public endpoint (loopback, RFC1918) must set Validation explicitly.
func New(name, baseURL, apiKey, model string) *Provider {
	p := &Provider{
		name:    name,
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		// Validation zero-value = strict: public https only, no loopback, no private IPs.
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second, DialValidation: &p.Validation})
	return p
}

// NewOllama creates a provider for Ollama-compatible local
// transcription endpoints. Ollama runs on loopback by default and can be
// pointed at a user-managed self-hosted URL.
func NewOllama(baseURL, model string) *Provider {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "gemma4:e4b"
	}
	p := New("ollama", baseURL, "", model)
	p.Validation = netsec.ValidationOptions{
		AllowLoopback: true,
		AllowPrivate:  true,
		AllowHTTP:     true,
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 60 * time.Second, DialValidation: &p.Validation})
	return p
}

// NewOpenAI creates a provider for the OpenAI Whisper API.
func NewOpenAI(apiKey string) *Provider {
	return New("openai", "https://api.openai.com", apiKey, "whisper-1")
}

// NewGroq creates a provider for the Groq Whisper API.
func NewGroq(apiKey string) *Provider {
	return New("groq", "https://api.groq.com/openai", apiKey, "whisper-large-v3-turbo")
}

// Transcribe sends audio to the OpenAI-compatible /v1/audio/transcriptions endpoint.
func (p *Provider) Transcribe(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	endpoint, err := netsec.BuildEndpoint(p.BaseURL, "v1/audio/transcriptions", p.Validation)
	if err != nil {
		return nil, fmt.Errorf("%s endpoint: %w", p.name, err)
	}
	resolved := stt.ResolveTranscribeOptions(p.name, "", opts, provideropts.Values{
		provideropts.OptionLanguage: "de",
	}, nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(stt.EnsureTranscriptionWAV(audio)); err != nil {
		return nil, fmt.Errorf("write audio data: %w", err)
	}

	if language := resolved.APILanguage(); language != "" {
		if err := writer.WriteField("language", language); err != nil {
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
	if resolved.Prompt != "" {
		if err := writer.WriteField("prompt", resolved.Prompt); err != nil {
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
	if err := p.authorize(ctx, req); err != nil {
		return nil, err
	}

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
		return nil, netsec.ProviderStatusError(p.name, resp.StatusCode, respBody)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	lang := stt.FirstNonEmptyTrimmed(resolved.Language, "de")

	return &stt.Result{
		Text:     result.Text,
		Language: lang,
		Duration: duration,
		Provider: p.Name(),
		Model:    model,
	}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return p.name
}

// Health checks provider reachability. Tries GET /health first (whisper-server),
// then falls back to GET /v1/models (OpenAI, Groq).
func (p *Provider) Health(ctx context.Context) error {
	healthURL, err := netsec.BuildEndpoint(p.BaseURL, "health", p.Validation)
	if err != nil {
		return fmt.Errorf("%s endpoint: %w", p.name, err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, http.NoBody)
	if err != nil {
		return err
	}
	if err := p.authorize(ctx, req); err != nil {
		return err
	}

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
	if err := p.authorize(ctx, req); err != nil {
		return err
	}

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

// SetHTTPClient replaces the provider's HTTP client. Deployments a user runs
// themselves need a longer timeout than a managed API does, and the client is
// built from validation options the caller has already relaxed.
func (p *Provider) SetHTTPClient(client *http.Client) {
	if client != nil {
		p.client = client
	}
}

// Capabilities reports the speech-to-text baseline every provider satisfies.
func (*Provider) Capabilities() []speechkit.Capability { return stt.BaseCapabilities() }
