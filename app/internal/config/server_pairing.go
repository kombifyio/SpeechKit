package config

import (
	"net"
	"net/url"
	"path/filepath"
	"strings"
)

// ServerPairingConfig configures LAN device pairing ([server.pairing]).
type ServerPairingConfig struct {
	// DeviceRequests opens POST /v1/pairing/device-requests to devices on
	// the local network. Unset (nil) resolves through DeviceRequestsEnabled:
	// on for a LAN-scoped bearer server, off otherwise.
	DeviceRequests *bool `toml:"device_requests"`
	// DevicesPath is the paired-device registry file (token hashes only).
	// Empty keeps it next to the server settings file.
	DevicesPath string `toml:"devices_path"`
}

// DeviceRequestsEnabled resolves [server.pairing].device_requests for the
// auth mode in effect. An explicit value wins. The default is on only when
// the server authenticates with the static bearer (the homelab shape, where
// paired devices would otherwise need that shared secret) and nothing in the
// configuration says it is served on a public origin: no Kombify deployment
// defaults and no public URL outside the local network. Hosted, edge-signed
// and OIDC deployments stay closed unless the operator opts in. public_url
// and public_base_url both affect this default, independently of network reach.
// LAN admission still refuses a loopback peer unless it is explicitly listed
// as a trusted proxy and supplies a LAN-only forwarding chain; the installer
// default loopback bind therefore has no pairing access until configured.
func DeviceRequestsEnabled(cfg *Config, authMode string) bool {
	if cfg == nil {
		return false
	}
	if v := cfg.Server.Pairing.DeviceRequests; v != nil {
		return *v
	}
	if !strings.EqualFold(strings.TrimSpace(authMode), "bearer") {
		return false
	}
	if KombifyDeploymentDefaultsRequested() {
		return false
	}
	for _, raw := range []string{cfg.Server.PublicBaseURL, cfg.Server.PublicURL} {
		if !lanScopedURL(raw) {
			return false
		}
	}
	return true
}

// PairedDevicesPath is where the paired-device registry lives.
func PairedDevicesPath(cfg *Config) string {
	if cfg != nil {
		if p := strings.TrimSpace(cfg.Server.Pairing.DevicesPath); p != "" {
			return p
		}
	}
	return filepath.Join(ServerStateDir(cfg), "paired-devices.json")
}

// lanScopedURL reports an empty URL or one whose host is a private,
// loopback or link-local address, or a local-only DNS name.
func lanScopedURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
	}
	if !strings.Contains(host, ".") {
		return true
	}
	for _, suffix := range []string{".local", ".lan", ".home", ".internal", ".home.arpa", ".localdomain", ".fritz.box"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}
