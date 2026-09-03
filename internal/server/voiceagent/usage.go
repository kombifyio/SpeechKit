//go:build linux

package voiceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// VoiceUsage is provider-authoritative connected time. The server starts the
// clock only after the realtime provider accepts the session and stops it when
// the adapter terminates.
type VoiceUsage struct {
	SessionID   string
	AISessionID string
	Provider    string
	Duration    time.Duration
}

type UsageReporter interface {
	Report(context.Context, string, VoiceUsage) error
}

type HTTPUsageReporter struct {
	Endpoint string
	Client   *http.Client
}

func NewHTTPUsageReporter(endpoint string) *HTTPUsageReporter {
	return &HTTPUsageReporter{
		Endpoint: strings.TrimSpace(endpoint),
		Client:   &http.Client{Timeout: 5 * time.Second},
	}
}

func (r *HTTPUsageReporter) Report(ctx context.Context, credential string, usage VoiceUsage) error {
	if r == nil || strings.TrimSpace(r.Endpoint) == "" {
		return errors.New("voiceagent usage endpoint is not configured")
	}
	if strings.TrimSpace(credential) == "" {
		return errors.New("voiceagent usage credential is missing")
	}
	if usage.Duration <= 0 || strings.TrimSpace(usage.SessionID) == "" {
		return errors.New("voiceagent usage is invalid")
	}
	payload, err := json.Marshal(map[string]any{
		"tool": "SPEECHKIT",
		"events": []map[string]any{{
			"event_id": "voice-session:" + usage.SessionID + ":connected",
			"metric":   "audio_minutes",
			"value":    usage.Duration.Minutes(),
			"metadata": map[string]string{
				"session_id":    usage.SessionID,
				"ai_session_id": usage.AISessionID,
				"provider":      usage.Provider,
			},
		}},
	})
	if err != nil {
		return fmt.Errorf("encode voiceagent usage: %w", err)
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Endpoint, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("create voiceagent usage request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+credential)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("send voiceagent usage: %w", err)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("voiceagent usage rejected with status %d", resp.StatusCode)
		if resp.StatusCode < 500 {
			break
		}
	}
	return lastErr
}
