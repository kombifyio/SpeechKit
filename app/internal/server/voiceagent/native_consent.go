//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

// NativeVoiceConsent is an authenticated snapshot or a current owner decision.
// Recording grants apply to this Voice Agent surface, independently of calls.
type NativeVoiceConsent struct {
	Verified           bool
	CloudProcessing    bool
	RecordingAllowed   bool
	RecordingUpdatedAt string
	ExpiresAt          int64
}

// NativeConsentReader checks the current authority for the retained binding.
// A reader must not derive identity, target or authority from callback input.
type NativeConsentReader func(context.Context, LiveConfigFrame) (NativeVoiceConsent, error)

// NativeConsentHTTPReader uses a trusted endpoint and request signer supplied
// by the server operator. Redirects, large responses and stalled reads fail closed.
type NativeConsentHTTPReader struct {
	Endpoint  string
	Authorize func(*http.Request, LiveConfigFrame) error
	Client    *http.Client
}

// Read obtains one bounded current decision; it never retries a failed read.
func (r NativeConsentHTTPReader) Read(parent context.Context, cfg LiveConfigFrame) (NativeVoiceConsent, error) {
	parsed, err := url.Parse(r.Endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || r.Authorize == nil {
		return NativeVoiceConsent{}, errors.New("voiceagent: current consent authority is unavailable")
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.Endpoint, http.NoBody)
	if err != nil {
		return NativeVoiceConsent{}, err
	}
	if err := r.Authorize(req, cfg); err != nil {
		return NativeVoiceConsent{}, err
	}
	req.Header.Set("Accept", "application/json")
	client := http.Client{}
	if r.Client != nil {
		client = *r.Client
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		return NativeVoiceConsent{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return NativeVoiceConsent{}, errors.New("voiceagent: current consent authority denied")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(body) > 4096 {
		return NativeVoiceConsent{}, errors.New("voiceagent: invalid current consent response")
	}
	var result struct {
		CloudProcessing  *bool   `json:"cloud_processing"`
		RecordingAllowed *bool   `json:"voice_agent_recording"`
		RecordingStamp   *string `json:"voice_agent_recording_updated_at"`
		ExpiresAt        int64   `json:"expiresAt"`
	}
	if json.Unmarshal(body, &result) != nil || result.CloudProcessing == nil || result.RecordingAllowed == nil || result.ExpiresAt <= time.Now().Unix() {
		return NativeVoiceConsent{}, errors.New("voiceagent: invalid current consent response")
	}
	stamp := ""
	if result.RecordingStamp != nil {
		stamp = *result.RecordingStamp
	}
	return NativeVoiceConsent{Verified: true, CloudProcessing: *result.CloudProcessing, RecordingAllowed: *result.RecordingAllowed, RecordingUpdatedAt: stamp, ExpiresAt: result.ExpiresAt}, nil
}

func validRecordingStamp(stamp string) bool {
	when, err := time.Parse(time.RFC3339Nano, stamp)
	return err == nil && !when.After(time.Now())
}
