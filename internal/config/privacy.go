package config

// privacy.go holds the [privacy] config section and the helpers every
// enforcement point uses to resolve the effective network scope. The scope is
// a Device-Target concept; the Server-Target keeps its own deployment
// hardening and ignores this block.

import (
	"fmt"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// Canonical network scope values, mirrored from pkg/speechkit for TOML/docs.
const (
	NetworkScopeOpen         = string(framework.NetworkScopeOpen)
	NetworkScopeLocalNetwork = string(framework.NetworkScopeLocalNetwork)
	NetworkScopeDeviceOnly   = string(framework.NetworkScopeDeviceOnly)
)

// Canonical retention scope values, mirrored from pkg/speechkit for TOML/docs.
const (
	RetentionScopeRetain    = string(framework.RetentionScopeRetain)
	RetentionScopeEphemeral = string(framework.RetentionScopeEphemeral)
)

// PrivacyConfig is the [privacy] TOML section.
type PrivacyConfig struct {
	// NetworkScope is "open" (default), "local_network", or "device_only".
	// Missing/empty means open (backwards compatible); unknown values make
	// config loading fail so a typo can never silently widen or narrow
	// network access.
	NetworkScope string `toml:"network_scope"`

	// AllowSetupTraffic opts setup/maintenance traffic (model downloads,
	// update checks) back in while a restricted scope is active. Ignored in
	// the open scope, where such traffic follows its own existing toggles.
	// Default false: restricted scopes are fully quiet unless the user
	// explicitly consents.
	AllowSetupTraffic bool `toml:"allow_setup_traffic"`

	// RetentionScope is "retain" (default) or "ephemeral". It is orthogonal to
	// NetworkScope: one says where the process may reach, the other what
	// survives the work. Missing/empty means retain (backwards compatible);
	// unknown values make config loading fail rather than being guessed.
	RetentionScope string `toml:"retention_scope"`
}

// NetworkScope resolves the effective scope for enforcement points. Invalid
// stored values fail closed to device_only — they should never survive
// Load/Save, but a runtime mutation must not widen access.
func (c *Config) NetworkScope() framework.NetworkScope {
	if c == nil {
		return framework.NetworkScopeOpen
	}
	return framework.NormalizeNetworkScope(c.Privacy.NetworkScope)
}

// SetupTrafficAllowed reports whether model downloads and update checks may
// use the network right now. Always true in the open scope (the existing
// [update]/[telemetry] toggles keep governing there); in restricted scopes it
// requires the explicit allow_setup_traffic opt-in.
func (c *Config) SetupTrafficAllowed() bool {
	scope := c.NetworkScope()
	if !scope.Restricted() {
		return true
	}
	return c.Privacy.AllowSetupTraffic
}

// RetentionScope resolves the effective retention scope for enforcement
// points. Invalid stored values fail closed to ephemeral — they should never
// survive Load/Save, but a runtime mutation must not quietly start keeping
// recordings.
func (c *Config) RetentionScope() framework.RetentionScope {
	if c == nil {
		return framework.RetentionScopeRetain
	}
	return framework.NormalizeRetentionScope(c.Privacy.RetentionScope)
}

// NormalizePrivacyConfig canonicalizes the [privacy] section and returns an
// error for unknown scope values. Load and Save both call it, so a config
// file with a typo'd scope is rejected instead of being reinterpreted.
func NormalizePrivacyConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	scope, err := framework.ParseNetworkScope(cfg.Privacy.NetworkScope)
	if err != nil {
		return fmt.Errorf("[privacy] network_scope: %w", err)
	}
	cfg.Privacy.NetworkScope = string(scope)
	retention, err := framework.ParseRetentionScope(cfg.Privacy.RetentionScope)
	if err != nil {
		return fmt.Errorf("[privacy] retention_scope: %w", err)
	}
	cfg.Privacy.RetentionScope = string(retention)
	return nil
}
