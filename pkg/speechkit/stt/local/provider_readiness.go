package local

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

func (p *Provider) waitForReady(ctx context.Context) error {
	healthURL := fmt.Sprintf("%s/health", p.BaseURL)
	for i := 0; i < whisperHealthRetries; i++ {
		if err := p.runtimeExitError(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, http.NoBody)
		if reqErr != nil {
			return fmt.Errorf("create health request: %w", reqErr)
		}
		resp, err := p.client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(whisperHealthInterval)
	}
	return fmt.Errorf("whisper-server did not become ready after %v", time.Duration(whisperHealthRetries)*whisperHealthInterval)
}

func (p *Provider) waitForInferenceReady(ctx context.Context) error {
	warmupCtx, cancel := context.WithTimeout(ctx, whisperWarmupTimeout)
	defer cancel()

	warmupClient := netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: whisperWarmupTimeout, DialValidation: &p.Validation})
	return p.waitForInferenceReadyWithClient(warmupCtx, warmupClient, whisperWarmupRetries, whisperWarmupInterval)
}

func (p *Provider) waitForInferenceReadyWithRetry(ctx context.Context, retries int, interval time.Duration) error {
	return p.waitForInferenceReadyWithClient(ctx, p.client, retries, interval)
}

func (p *Provider) waitForInferenceReadyWithClient(ctx context.Context, client *http.Client, retries int, interval time.Duration) error {
	if retries <= 0 {
		retries = 1
	}
	if interval <= 0 {
		interval = time.Millisecond
	}
	if client == nil {
		client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second, DialValidation: &p.Validation})
	}

	endpoint := fmt.Sprintf("%s/v1/audio/transcriptions", p.BaseURL)
	warmupAudio := buildWarmupWAV()
	var lastErr error

	for i := 0; i < retries; i++ {
		if err := p.runtimeExitError(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := p.probeInferenceReady(ctx, client, endpoint, warmupAudio)
		if err == nil {
			return nil
		}
		lastErr = err

		if i == retries-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("unknown warmup failure")
	}
	return fmt.Errorf("whisper-server inference not ready: %w", lastErr)
}

func (p *Provider) probeInferenceReady(ctx context.Context, client *http.Client, endpoint string, audioData []byte) error {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "warmup.wav")
	if err != nil {
		return fmt.Errorf("create warmup form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return fmt.Errorf("write warmup audio: %w", err)
	}
	if err := writer.WriteField("model", "whisper-1"); err != nil {
		return fmt.Errorf("write warmup model field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close warmup multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("create warmup request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("POST warmup request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, localMaxResponseBytes))
	if err != nil {
		return fmt.Errorf("read warmup response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return netsec.ProviderStatusError("local warmup", resp.StatusCode, respBody)
	}

	return nil
}

func buildWarmupWAV() []byte {
	// 200ms of silence is enough to verify the inference route without
	// adding noticeable startup cost or depending on user audio.
	pcm := make([]byte, (audio.SampleRate/5)*audio.BytesPerSample)
	return audio.PCMToWAV(pcm)
}
