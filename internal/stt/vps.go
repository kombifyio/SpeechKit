package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const vpsMaxResponseBytes = 1 << 20

// VPSProvider implements STTProvider for Tier 2: remote whisper-server on VPS.
type VPSProvider struct {
	BaseURL string
	APIKey  string
	client  *http.Client
}

func NewVPSProvider(baseURL, apiKey string) *VPSProvider {
	return &VPSProvider{
		BaseURL: baseURL,
		APIKey:  apiKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (p *VPSProvider) Transcribe(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error) {
	endpoint := fmt.Sprintf("%s/v1/audio/transcriptions", p.BaseURL)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	part.Write(audio)

	if opts.Language != "" && opts.Language != "auto" {
		writer.WriteField("language", opts.Language)
	}
	writer.WriteField("model", "whisper-1")
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vps request: %w", err)
	}
	defer resp.Body.Close()
	duration := time.Since(start)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, vpsMaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vps error (%d): %s", resp.StatusCode, string(respBody))
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
		Model:    opts.Model,
	}, nil
}

func (p *VPSProvider) Name() string {
	return "vps"
}

func (p *VPSProvider) Health(ctx context.Context) error {
	healthURL := fmt.Sprintf("%s/health", p.BaseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("vps health: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("vps health: status %d", resp.StatusCode)
	}
	return nil
}
