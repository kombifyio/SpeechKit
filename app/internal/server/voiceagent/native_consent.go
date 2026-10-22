//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
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
	client := *netsec.Default()
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
		return NativeVoiceConsent{}, fmt.Errorf("voiceagent: current consent authority denied (status %d, code %s)", response.StatusCode, denialCode(response.Body))
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

// denialCode reads the closed error code of a denial body ({"error_code":"..."}): lower-case letters,
// digits and underscores only, so a log line can carry it without carrying anything else.
func denialCode(body io.Reader) string {
	var denial struct {
		ErrorCode string `json:"error_code"`
	}
	if json.NewDecoder(io.LimitReader(body, 1024)).Decode(&denial) != nil || denial.ErrorCode == "" || len(denial.ErrorCode) > 64 {
		return "none"
	}
	for _, r := range denial.ErrorCode {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return "invalid"
		}
	}
	return denial.ErrorCode
}

func validRecordingStamp(stamp string) bool {
	when, err := time.Parse(time.RFC3339Nano, stamp)
	return err == nil && !when.After(time.Now())
}
