package deviceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

func (a *Agent) postServerJSON(ctx context.Context, path string, body, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolve(a.serverURL, path), &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", a.userAgent)
	req.Header.Set("Authorization", "Bearer "+a.cfg.PairingToken)
	req.Header.Set("X-SpeechKit-Device-ID", a.cfg.Device.DeviceID)
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // response body is fully read below
	responseInstanceID := strings.TrimSpace(resp.Header.Get(ServerInstanceHeader))
	if responseInstanceID == "" {
		return ErrServerIdentityMissing
	}
	if responseInstanceID != "" && responseInstanceID != a.cfg.ExpectedServerInstanceID {
		return fmt.Errorf("%w: expected %q, got %q", ErrServerIdentityMismatch, a.cfg.ExpectedServerInstanceID, responseInstanceID)
	}
	limit := int64(maxJSONResponseBytes)
	if path == "/v1/device-agent/tts" {
		limit = maxTTSResponseBytes
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limit = maxErrorResponseBytes
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > limit {
		return ErrResponseTooLarge
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		httpErr := &HTTPError{Method: http.MethodPost, Path: path, StatusCode: resp.StatusCode}
		_ = json.Unmarshal(raw, &httpErr.Envelope)
		return httpErr
	}
	if out != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode %s response: %w", path, err)
		}
	}
	return nil
}

func parseLocalBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return nil, errors.New("empty URL")
	}
	if err := netsec.ValidateProviderURL(raw, localValidation()); err != nil {
		return nil, err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("server URL must be an origin without path, query, or fragment")
	}
	if strings.EqualFold(u.Scheme, "http") && !localHTTPHostAllowed(u.Hostname()) {
		return nil, ErrInsecureServerTransport
	}
	return u, nil
}

func localHTTPHostAllowed(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

func validPairingToken(value string) bool {
	if len(value) < minimumPairingTokenBytes || len(value) > 512 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-', character == '.', character == '_', character == '~':
		case character == '+', character == '/', character == '=':
		default:
			return false
		}
	}
	return true
}

func localValidation() netsec.ValidationOptions {
	return netsec.ValidationOptions{
		AllowLoopback: true,
		AllowPrivate:  true,
		AllowHTTP:     true,
		RequireLocal:  true,
	}
}

func resolve(base *url.URL, path string) string {
	ref := &url.URL{Path: path}
	return base.ResolveReference(ref).String()
}
