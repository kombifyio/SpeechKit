package stt

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
	hfBaseURL        = "https://router.huggingface.co/hf-inference/models"
	maxResponseBytes = 1 << 20 // 1 MB limit for API responses
)

// HuggingFaceProvider implements STTProvider for Tier 3: HuggingFace Inference API.
type HuggingFaceProvider struct {
	Model   string
	Token   string
	BaseURL string // Override for testing; defaults to hfBaseURL
	client  *http.Client
}

func NewHuggingFaceProvider(model, token string) *HuggingFaceProvider {
	return &HuggingFaceProvider{
		Model:   model,
		Token:   token,
		BaseURL: hfBaseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (p *HuggingFaceProvider) Transcribe(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error) {
	base := p.BaseURL
	if base == "" {
		base = hfBaseURL
	}
	model := p.Model
	if opts.Model != "" {
		model = opts.Model
	}
	endpoint := fmt.Sprintf("%s/%s", base, model)

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(audio))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "audio/wav")
	req.Header.Set("Authorization", "Bearer "+p.Token)

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hf request: %w", err)
	}
	defer resp.Body.Close()
	duration := time.Since(start)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	switch {
	case resp.StatusCode == 503:
		return nil, fmt.Errorf("hf model loading (503): %s", string(respBody))
	case resp.StatusCode == 429:
		return nil, fmt.Errorf("hf rate limit (429): %s", string(respBody))
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("hf error (%d): %s", resp.StatusCode, string(respBody))
	}

	var hfResp struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &hfResp); err != nil {
		return nil, fmt.Errorf("parse response: %w (body: %s)", err, string(respBody))
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
	base := p.BaseURL
	if base == "" {
		base = hfBaseURL
	}
	endpoint := fmt.Sprintf("%s/%s", base, p.Model)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("hf health: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))

	if resp.StatusCode == 503 {
		return fmt.Errorf("hf model loading")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hf health check failed: status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
