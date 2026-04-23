package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/kombifyio/SpeechKit/internal/netsec"
)

const (
	hfBaseURL        = "https://router.huggingface.co/hf-inference/models"
	maxResponseBytes = 1 << 20 // 1 MB limit for API responses
)

// hfModelPattern restricts HuggingFace model IDs to safe characters.
// Model IDs follow the form "owner/name" or bare "name", optionally with
// dots, dashes, and underscores. Rejects path-traversal or URL-injection
// attempts (e.g. "..", "?", "#", "@", spaces).
var hfModelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-]*(?:/[A-Za-z0-9._\-]+)?$`)

// HuggingFaceProvider implements STTProvider for Tier 3: HuggingFace Inference API.
//
// BaseURL is user-configurable. It is validated against Validation on every
// request. Default Validation is strict (public https only).
type HuggingFaceProvider struct {
	Model      string
	Token      string
	BaseURL    string // Override for testing; defaults to hfBaseURL
	Validation netsec.ValidationOptions
	client     *http.Client
}

func NewHuggingFaceProvider(model, token string) *HuggingFaceProvider {
	p := &HuggingFaceProvider{
		Model:   model,
		Token:   token,
		BaseURL: hfBaseURL,
		// Validation zero-value = strict: public https only.
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second, DialValidation: &p.Validation})
	return p
}

// hfEndpoint builds a validated HuggingFace inference URL for the given model.
func (p *HuggingFaceProvider) hfEndpoint(model string) (string, error) {
	if !hfModelPattern.MatchString(model) {
		return "", fmt.Errorf("hf: invalid model id %q", model)
	}
	base := p.BaseURL
	if base == "" {
		base = hfBaseURL
	}
	return netsec.BuildEndpoint(base, model, p.Validation)
}

func (p *HuggingFaceProvider) Transcribe(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error) {
	model := p.Model
	if opts.Model != "" {
		model = opts.Model
	}
	endpoint, err := p.hfEndpoint(model)
	if err != nil {
		return nil, fmt.Errorf("hf endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(audio)) //nolint:gosec // G704: endpoint is HuggingFace API URL from user config (by design)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "audio/wav")
	req.Header.Set("Authorization", "Bearer "+p.Token)

	start := time.Now()
	resp, err := p.client.Do(req) //nolint:gosec // G704: SSRF by design, endpoint configured by user
	if err != nil {
		return nil, fmt.Errorf("hf request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	duration := time.Since(start)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	switch {
	case resp.StatusCode == 503:
		return nil, netsec.ProviderStatusError("hf", resp.StatusCode, respBody)
	case resp.StatusCode == 429:
		return nil, netsec.ProviderStatusError("hf", resp.StatusCode, respBody)
	case resp.StatusCode != http.StatusOK:
		return nil, netsec.ProviderStatusError("hf", resp.StatusCode, respBody)
	}

	var hfResp struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &hfResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	lang := opts.Language
	if lang == "" {
		lang = "de"
	}

	return &Result{
		Text:     hfResp.Text,
		Language: lang,
		Duration: duration,
		Provider: p.Name(),
		Model:    model,
	}, nil
}

func (p *HuggingFaceProvider) Name() string {
	return "huggingface"
}

func (p *HuggingFaceProvider) Health(ctx context.Context) error {
	endpoint, err := p.hfEndpoint(p.Model)
	if err != nil {
		return fmt.Errorf("hf endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)

	resp, err := p.client.Do(req) //nolint:gosec // G704: SSRF by design, endpoint configured by user
	if err != nil {
		return fmt.Errorf("hf health: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))

	if resp.StatusCode == 503 {
		return fmt.Errorf("hf model loading")
	}
	if resp.StatusCode != http.StatusOK {
		return netsec.ProviderStatusError("hf health", resp.StatusCode, body)
	}
	return nil
}
