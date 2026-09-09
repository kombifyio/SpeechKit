// Package pairing is the speechkit.pairing.v1 payload a homelab client
// consumes into a stored self_host session. DNS-SD announcements stay
// credential-free; this JSON is how a token leaves /setup.
package pairing

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	Version    = "speechkit.pairing.v1"
	AuthBearer = "bearer"
)

// Payload is the operator-facing pairing card. Token is a bearer secret;
// it must never appear in `_speechkit._tcp` TXT.
type Payload struct {
	V         string `json:"v"`
	ServerURL string `json:"server_url"`
	Auth      string `json:"auth"`
	Token     string `json:"token"`
	Name      string `json:"name,omitempty"`
}

var (
	errMissingURL   = errors.New("pairing: server_url required")
	errMissingToken = errors.New("pairing: token required")
	errVersion      = errors.New("pairing: unsupported version")
	errAuth         = errors.New("pairing: auth must be bearer")
)

// New builds a v1 bearer payload. Missing URL or token is not a session.
func New(serverURL, token, name string) (Payload, error) {
	url := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	tok := strings.TrimSpace(token)
	if url == "" {
		return Payload{}, errMissingURL
	}
	if tok == "" {
		return Payload{}, errMissingToken
	}
	return Payload{
		V:         Version,
		ServerURL: url,
		Auth:      AuthBearer,
		Token:     tok,
		Name:      strings.TrimSpace(name),
	}, nil
}

// Parse reads a v1 payload. Wrong version, non-bearer auth, or a missing
// URL/token is rejected so callers cannot persist Cloud or tester mode.
func Parse(raw []byte) (Payload, error) {
	var payload Payload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Payload{}, err
	}
	if strings.TrimSpace(payload.V) != Version {
		return Payload{}, errVersion
	}
	if strings.ToLower(strings.TrimSpace(payload.Auth)) != AuthBearer {
		return Payload{}, errAuth
	}
	return New(payload.ServerURL, payload.Token, payload.Name)
}
