// Package cloudlink links a self-hosted SpeechKit server to a person's
// account and keeps it checked in with that account's device registry.
//
// The server holds its own credential: it runs the OAuth 2.0 device
// authorization grant (RFC 8628) with a public client, the person approves on
// another device, and the server keeps only the resulting refresh token,
// owner-only in its state directory. It then checks in periodically with
// PUT <gateway_url>/v1/connect/edge-devices/<instance id> (schema
// kombify.edge_device.checkin.v1, kind speechkit-server). The registry is
// pull only: nothing calls the server, and the record grants nothing.
//
// Off by default ([server.cloud_link] enabled = false). Nothing calls out
// until an operator enables the section and starts a link, and a restricted
// [privacy] network_scope (local_network, device_only, Full Local or a
// local-only machine policy) suspends the link entirely.
package cloudlink

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/kombifyio/SpeechKit/app/internal/config"
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// DefaultScope asks for a refresh token (offline_access) next to the
// identity scopes.
const DefaultScope = "openid profile email offline_access"

// Kind is the device kind a server checks in as.
const Kind = "speechkit-server"

// CredentialFile is the owner-only (0600) credential in the state directory.
const CredentialFile = "cloud-link-credential.json"

// ErrDisabled means [server.cloud_link] is off.
var ErrDisabled = errors.New("the account link is off: set [server.cloud_link] enabled = true")

// Settings are the validated [server.cloud_link] values.
type Settings struct {
	Issuer     string
	ClientID   string
	Audience   string
	Scope      string
	GatewayURL string
	StateDir   string
}

// Resolve validates [server.cloud_link] and the privacy scope. Every missing
// value is named with the key that sets it; nothing is defaulted silently
// except the standard OAuth scope.
func Resolve(cfg *config.Config) (Settings, error) {
	if cfg == nil {
		return Settings{}, errors.New("cloud link: no configuration")
	}
	c := cfg.Server.CloudLink
	// The state directory is known even when the link cannot run, so an
	// unlink can always remove a stored credential.
	dirOnly := Settings{StateDir: config.ServerStateDir(cfg)}
	if !c.Enabled {
		return dirOnly, ErrDisabled
	}
	if err := ScopeAllows(cfg); err != nil {
		return dirOnly, err
	}
	s := Settings{
		Issuer:     strings.TrimSpace(c.Issuer),
		ClientID:   strings.TrimSpace(c.ClientID),
		Audience:   strings.TrimSpace(c.Audience),
		Scope:      strings.TrimSpace(c.Scope),
		GatewayURL: strings.TrimRight(strings.TrimSpace(c.GatewayURL), "/"),
		StateDir:   config.ServerStateDir(cfg),
	}
	if s.Scope == "" {
		s.Scope = DefaultScope
	}
	var missing []string
	for _, field := range []struct{ key, value string }{
		{"issuer", s.Issuer}, {"client_id", s.ClientID}, {"audience", s.Audience}, {"gateway_url", s.GatewayURL},
	} {
		if field.value == "" {
			missing = append(missing, field.key)
		}
	}
	if len(missing) > 0 {
		return dirOnly, fmt.Errorf("the account link is enabled but [server.cloud_link] %s is not set; "+
			"set it to the values of your account provider", strings.Join(missing, ", "))
	}
	for _, field := range []struct{ key, value string }{{"issuer", s.Issuer}, {"gateway_url", s.GatewayURL}} {
		if err := checkEndpointURL(field.value); err != nil {
			return dirOnly, fmt.Errorf("[server.cloud_link] %s: %w", field.key, err)
		}
	}
	return s, nil
}

// ScopeAllows reports whether the effective network scope lets the server
// reach a public account provider at all. The account link is a public cloud
// endpoint, so a restricted scope suspends it like every other cloud call.
func ScopeAllows(cfg *config.Config) error {
	scope := cfg.NetworkScope()
	if policy := scope.EndpointPolicyFor(framework.EndpointClassCloud); !policy.Allowed {
		return fmt.Errorf("the account link is unavailable while the network scope is %s "+
			"(a local-only server never calls a public endpoint); set [privacy] network_scope = \"open\" to link this server", scope)
	}
	return nil
}

// checkEndpointURL accepts https, and http only for a loopback host (a local
// test double).
func checkEndpointURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%q is not an absolute URL", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
	}
	return fmt.Errorf("%q must use https", raw)
}
