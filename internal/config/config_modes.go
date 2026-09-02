package config

// Per-mode model selection and Server-Connection (device -> remote
// SpeechKit server) configuration types and helpers.

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/hostconfig"
)

type ModelSelectionConfig struct {
	Dictate    ModeModelSelection `toml:"dictate"`
	Assist     ModeModelSelection `toml:"assist"`
	VoiceAgent ModeModelSelection `toml:"voice_agent"`
	// TTS pins the Voice-Output provider profile + optional fallback that
	// Assist and Voice-Agent use when speaking back to the user. Same shape
	// as the three product-mode selections so the catalog API stays
	// symmetric. Added in v0.37 alongside the hands-free Voice-Companion
	// flow so Thalia + Companion-Live deployments can pick a stable voice
	// (e.g. Google Studio-O DE) without editing the lower-level
	// [tts.providers.*] blocks.
	TTS ModeModelSelection `toml:"tts"`
}

// Mode source values for ModeModelSelection.ModeSource. "local" means the
// desktop app runs the mode against the in-process Framework kernel (default,
// preserves all pre-0.26 behaviour). "server" routes the mode through
// ServerConnection to a remote speechkit-server.
// The canonical values live in the public hostconfig package so embedders
// and the desktop app normalise the same TOML identically.
const (
	ModeSourceLocal  = hostconfig.ModeSourceLocal
	ModeSourceServer = hostconfig.ModeSourceServer
)

const (
	ServerConnectionAuthModeBearer   = hostconfig.ServerConnectionAuthModeBearer
	ServerConnectionAuthModeAPIKey   = hostconfig.ServerConnectionAuthModeAPIKey
	ServerConnectionAuthModeEdgeBeta = hostconfig.ServerConnectionAuthModeEdgeBeta
)

const (
	DefaultBetaInstallIDEnv     = hostconfig.DefaultBetaInstallIDEnv
	DefaultBetaInstallSecretEnv = hostconfig.DefaultBetaInstallSecretEnv
)

type ModeModelSelection struct {
	PrimaryProfileID  string `toml:"primary_profile_id"`
	FallbackProfileID string `toml:"fallback_profile_id"`

	// ModeSource selects whether this mode runs locally (Framework kernel
	// in-process, default) or against a remote SpeechKit Server-Target
	// configured under [server_connection]. Empty string is treated as
	// ModeSourceLocal so existing configs keep behaving as before.
	ModeSource string `toml:"mode_source"`
}

// ServerConnectionConfig describes how the device/local-target reaches a
// remote SpeechKit server. Read by cmd/speechkit (and any embedded library
// caller) when a ModeModelSelection has mode_source = "server"; the
// Server-Target itself ignores this section.
type ServerConnectionConfig struct {
	// Enabled is a compatibility mirror for clients that still display a
	// top-level server toggle. Runtime routing is determined by each mode's
	// mode_source; do not use Enabled as a second execution gate.
	Enabled bool `toml:"enabled"`

	// URL is the base URL of the speechkit-server, e.g.
	// "https://speechkit.example.com" or "http://localhost:8080".
	URL string `toml:"url"`

	// BearerTokenEnv names the env var that holds the bearer token sent in
	// the Authorization header. Defaults to SPEECHKIT_SERVER_TOKEN. The
	// value is never read from the TOML file itself — only the env var name
	// is configured here.
	BearerTokenEnv string `toml:"bearer_token_env"`

	// AuthMode selects how the resolved token is attached to outbound
	// requests. "bearer" sends Authorization: Bearer <token>. "api_key"
	// sends X-Api-Key: <token> for servers that use header-based API keys.
	// "edge_beta" sends only anonymous per-install beta headers to a managed
	// edge broker; it never reads or sends a shared server/provider token.
	// Empty/missing defaults to "bearer".
	AuthMode string `toml:"auth_mode"`

	// BetaInstallIDEnv and BetaInstallSecretEnv name the local secret slots
	// used only when auth_mode = "edge_beta". The values are generated on
	// first use and stored via the host secret store (DPAPI on Windows). They
	// are anonymous per-install identifiers, not provider/server credentials.
	BetaInstallIDEnv     string `toml:"beta_install_id_env"`
	BetaInstallSecretEnv string `toml:"beta_install_secret_env"`

	// FallbackToLocal makes the device app fall back to the in-process
	// Framework kernel if a server call fails or the server is unreachable.
	// Useful for laptop deployments that may be offline; should be false
	// for kiosks that must never silently downgrade to local processing.
	FallbackToLocal bool `toml:"fallback_to_local"`

	// RequestTimeoutSec caps non-streaming HTTP calls (Dictation, Assist).
	// 0 means no explicit timeout (the underlying http.Client default
	// applies). Voice Agent WebSocket sessions are not affected.
	RequestTimeoutSec int `toml:"request_timeout_sec"`

	// ActiveTargetID selects the registered server target copied into the
	// compatibility fields above. Empty means the top-level URL/env/auth fields
	// are an ad-hoc single target.
	ActiveTargetID string `toml:"active_target_id"`

	// Targets is the optional local registry of SpeechKit server endpoints the
	// device can switch between. These are user/operator configured; product
	// builds must not inject private gateway/origin entries here.
	Targets []ServerConnectionTargetConfig `toml:"targets"`
}

type ServerConnectionTargetConfig struct {
	ID                   string `toml:"id"`
	Label                string `toml:"label"`
	URL                  string `toml:"url"`
	BearerTokenEnv       string `toml:"bearer_token_env"`
	AuthMode             string `toml:"auth_mode"`
	BetaInstallIDEnv     string `toml:"beta_install_id_env"`
	BetaInstallSecretEnv string `toml:"beta_install_secret_env"`
	FallbackToLocal      bool   `toml:"fallback_to_local"`
	RequestTimeoutSec    int    `toml:"request_timeout_sec"`
}

// ResolvedModeSource returns the effective ModeSource for this mode,
// normalising the empty default to ModeSourceLocal. Use this everywhere
// instead of reading sel.ModeSource directly so a missing TOML field does
// not silently mean "server".
func (sel ModeModelSelection) ResolvedModeSource() string {
	switch strings.TrimSpace(strings.ToLower(sel.ModeSource)) {
	case ModeSourceServer:
		return ModeSourceServer
	default:
		return ModeSourceLocal
	}
}

func NormalizeServerConnectionAuthMode(mode string) string {
	return hostconfig.NormalizeServerConnectionAuthMode(mode)
}
