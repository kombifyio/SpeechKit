package config

import (
	"errors"
	"fmt"
)

// Policy sources, in precedence order. A value from an earlier source is
// never replaced by a later one.
const (
	PolicySourceMachinePolicies = "hklm-policies"
	PolicySourceMachineDefaults = "hklm-defaults"
	PolicySourceUser            = "hkcu"
)

// PolicyValues holds the registry-resolved subset of Config that can be
// pinned via ADMX/GPO on Windows. Pointer fields signal "set vs not set"
// for booleans and ints; empty strings signal "not set" for REG_SZ values.
//
// The struct and the merge/enforcement logic live here (build-tag-neutral)
// so they are tested on every target. Only the registry walk is
// platform-specific: see policy_windows.go (real registry read) and
// policy_other.go (stub returning an empty overlay).
//
// Registry layout consumed by ReadPolicyValues (highest-priority first):
//
//	HKLM\SOFTWARE\Policies\kombify\SpeechKit\  — machine policy (GPO)
//	HKLM\SOFTWARE\kombify\SpeechKit\           — machine-wide admin values
//	HKCU\Software\kombify\SpeechKit\           — per-user values, restrict-only
//
// Both HKLM hives are writable only by administrators and are enforced for
// the life of the process. HKCU is writable by the user (and by anything that
// runs as the user), so it is trusted no more than config.toml: mergePolicyHives
// accepts from it only values that restrict SpeechKit further and drops every
// value that could loosen a security setting (see restrictUserHive).
type PolicyValues struct {
	UpdateEnabled             *bool
	UpdateManifestURL         string
	TelemetryUpdateCheck      *bool
	ProvidersEnforceLocalOnly *bool
	VoiceAgentAllowCloud      *bool
	AuditRetentionDays        *int
	AuditEventLogEnabled      *bool
	AuditOTLPEndpoint         string
	// FoundryEntraClientID and FoundryEntraTenantID let a company pin the
	// Microsoft Entra app registration SpeechKit signs in with (see
	// docs/entra-app-registration.md) for a whole fleet. They land in
	// [providers.foundry] entra_client_id / entra_tenant_id and beat the
	// build default and the environment.
	FoundryEntraClientID string
	FoundryEntraTenantID string

	// Origin describes which registry hive first contributed a value.
	// One of "hklm-policies" | "hklm-defaults" | "hkcu" | "none" | "non-windows".
	Origin string
	// KeysFound counts the registry values that ended up in this overlay
	// (across all hives). Every one of them is enforced for the life of the
	// process. Used in the policy.applied audit event.
	KeysFound int
	// MachineKeys and UserKeys split KeysFound by trust level: values from
	// either HKLM hive vs. restrict-only values from HKCU.
	MachineKeys int
	UserKeys    int
	// IgnoredUserKeys counts HKCU values that were dropped because they would
	// have loosened a setting (or are security-relevant and machine-only).
	IgnoredUserKeys int
}

// EnforcesLocalOnly reports whether Providers\EnforceLocalOnly=1 is in force.
func (p PolicyValues) EnforcesLocalOnly() bool {
	return p.ProvidersEnforceLocalOnly != nil && *p.ProvidersEnforceLocalOnly
}

// BlocksCloudVoiceAgent reports whether VoiceAgent\AllowCloudProviders=0 is
// in force.
func (p PolicyValues) BlocksCloudVoiceAgent() bool {
	return p.VoiceAgentAllowCloud != nil && !*p.VoiceAgentAllowCloud
}

func (p PolicyValues) hasValues() bool {
	for _, f := range policyFields {
		if f.pinned(p) {
			return true
		}
	}
	return false
}

// policyHive is the raw read result for one registry hive root. Nil pointer
// fields and empty strings mean "not present in this hive".
type policyHive struct {
	updateEnabled         *bool
	updateManifestURL     string
	telemetryUpdateCheck  *bool
	providersEnforceLocal *bool
	voiceAgentAllowCloud  *bool
	auditRetentionDays    *int
	auditEventLogEnabled  *bool
	auditOTLPEndpoint     string
	foundryEntraClientID  string
	foundryEntraTenantID  string
}

// mergePolicyHives resolves the effective overlay from the three registry
// hives. Precedence is per key: HKLM\Policies beats HKLM\SpeechKit beats
// HKCU. The HKCU hive is first reduced to restrict-only values.
func mergePolicyHives(machinePolicies, machineDefaults, user policyHive) PolicyValues {
	var result PolicyValues
	result.MachineKeys += mergePolicyHive(&result, machinePolicies, PolicySourceMachinePolicies)
	result.MachineKeys += mergePolicyHive(&result, machineDefaults, PolicySourceMachineDefaults)
	restricted, ignored := restrictUserHive(user)
	result.UserKeys = mergePolicyHive(&result, restricted, PolicySourceUser)
	result.IgnoredUserKeys = ignored
	result.KeysFound = result.MachineKeys + result.UserKeys
	if result.Origin == "" {
		result.Origin = "none"
	}
	return result
}

// restrictUserHive drops every HKCU value that could loosen SpeechKit's
// behaviour. The user can already change config.toml, so HKCU must not be a
// way to override machine policy, to redirect the update or audit channel,
// or to swap the Microsoft sign-in app registration. What remains are the
// values that only ever restrict: turning update checks off, enforcing
// local-only processing, blocking cloud Voice Agent providers and mirroring
// audit events to the Event Log.
func restrictUserHive(h policyHive) (policyHive, int) {
	var out policyHive
	ignored := 0
	keepIf := func(v *bool, restrictive bool) *bool {
		if v == nil {
			return nil
		}
		if *v != restrictive {
			ignored++
			return nil
		}
		return v
	}
	out.updateEnabled = keepIf(h.updateEnabled, false)
	out.telemetryUpdateCheck = keepIf(h.telemetryUpdateCheck, false)
	out.providersEnforceLocal = keepIf(h.providersEnforceLocal, true)
	out.voiceAgentAllowCloud = keepIf(h.voiceAgentAllowCloud, false)
	out.auditEventLogEnabled = keepIf(h.auditEventLogEnabled, true)
	// Machine-only: an update source, an audit sink, an audit retention
	// period and the sign-in app registration are security decisions.
	for _, set := range []bool{
		h.updateManifestURL != "",
		h.auditOTLPEndpoint != "",
		h.auditRetentionDays != nil,
		h.foundryEntraClientID != "",
		h.foundryEntraTenantID != "",
	} {
		if set {
			ignored++
		}
	}
	return out, ignored
}

// mergePolicyHive applies one hive into result, respecting precedence: a key
// already set by a higher-priority hive is not overwritten. It sets
// result.Origin to source when the hive is the first to contribute and
// returns how many keys it contributed.
func mergePolicyHive(result *PolicyValues, t policyHive, source string) int {
	contributed := 0
	if t.updateEnabled != nil && result.UpdateEnabled == nil {
		result.UpdateEnabled = t.updateEnabled
		contributed++
	}
	if t.updateManifestURL != "" && result.UpdateManifestURL == "" {
		result.UpdateManifestURL = t.updateManifestURL
		contributed++
	}
	if t.telemetryUpdateCheck != nil && result.TelemetryUpdateCheck == nil {
		result.TelemetryUpdateCheck = t.telemetryUpdateCheck
		contributed++
	}
	if t.providersEnforceLocal != nil && result.ProvidersEnforceLocalOnly == nil {
		result.ProvidersEnforceLocalOnly = t.providersEnforceLocal
		contributed++
	}
	if t.voiceAgentAllowCloud != nil && result.VoiceAgentAllowCloud == nil {
		result.VoiceAgentAllowCloud = t.voiceAgentAllowCloud
		contributed++
	}
	if t.auditRetentionDays != nil && result.AuditRetentionDays == nil {
		result.AuditRetentionDays = t.auditRetentionDays
		contributed++
	}
	if t.auditEventLogEnabled != nil && result.AuditEventLogEnabled == nil {
		result.AuditEventLogEnabled = t.auditEventLogEnabled
		contributed++
	}
	if t.auditOTLPEndpoint != "" && result.AuditOTLPEndpoint == "" {
		result.AuditOTLPEndpoint = t.auditOTLPEndpoint
		contributed++
	}
	if t.foundryEntraClientID != "" && result.FoundryEntraClientID == "" {
		result.FoundryEntraClientID = t.foundryEntraClientID
		contributed++
	}
	if t.foundryEntraTenantID != "" && result.FoundryEntraTenantID == "" {
		result.FoundryEntraTenantID = t.foundryEntraTenantID
		contributed++
	}
	if contributed > 0 && result.Origin == "" {
		result.Origin = source
	}
	return contributed
}

// applyPolicyOverlay mutates cfg in place with the values from policy.
// Policy values always win over config.toml values.
func applyPolicyOverlay(cfg *Config, policy PolicyValues) {
	for _, f := range policyFields {
		if f.pinned(policy) {
			f.apply(cfg, policy)
		}
	}
}

// policyField describes one config value a policy can pin.
type policyField struct {
	name   string
	pinned func(p PolicyValues) bool
	apply  func(cfg *Config, p PolicyValues)
	equal  func(a, b *Config) bool
	copy   func(dst, src *Config)
}

var policyFields = []policyField{
	{
		name:   "update.enabled",
		pinned: func(p PolicyValues) bool { return p.UpdateEnabled != nil },
		apply:  func(c *Config, p PolicyValues) { c.Update.Enabled = *p.UpdateEnabled },
		equal:  func(a, b *Config) bool { return a.Update.Enabled == b.Update.Enabled },
		copy:   func(d, s *Config) { d.Update.Enabled = s.Update.Enabled },
	},
	{
		name:   "update.manifest_url",
		pinned: func(p PolicyValues) bool { return p.UpdateManifestURL != "" },
		apply:  func(c *Config, p PolicyValues) { c.Update.ManifestURL = p.UpdateManifestURL },
		equal:  func(a, b *Config) bool { return a.Update.ManifestURL == b.Update.ManifestURL },
		copy:   func(d, s *Config) { d.Update.ManifestURL = s.Update.ManifestURL },
	},
	{
		name:   "telemetry.update_check",
		pinned: func(p PolicyValues) bool { return p.TelemetryUpdateCheck != nil },
		apply:  func(c *Config, p PolicyValues) { c.Telemetry.UpdateCheck = *p.TelemetryUpdateCheck },
		equal:  func(a, b *Config) bool { return a.Telemetry.UpdateCheck == b.Telemetry.UpdateCheck },
		copy:   func(d, s *Config) { d.Telemetry.UpdateCheck = s.Telemetry.UpdateCheck },
	},
	{
		// EnforceLocalOnly=1 pins the STT routing strategy to the on-device
		// model. NetworkScope additionally caps the scope for every other
		// surface (see privacy.go).
		name:   "routing.strategy",
		pinned: PolicyValues.EnforcesLocalOnly,
		apply:  func(c *Config, _ PolicyValues) { c.Routing.Strategy = "local-only" },
		equal:  func(a, b *Config) bool { return a.Routing.Strategy == b.Routing.Strategy },
		copy:   func(d, s *Config) { d.Routing.Strategy = s.Routing.Strategy },
	},
	{
		// AllowCloudProviders=0 pins Voice Agent to the cascaded pipeline.
		name:   "voice_agent.provider",
		pinned: PolicyValues.BlocksCloudVoiceAgent,
		apply:  func(c *Config, _ PolicyValues) { c.VoiceAgent.Provider = "local-cascaded" },
		equal:  func(a, b *Config) bool { return a.VoiceAgent.Provider == b.VoiceAgent.Provider },
		copy:   func(d, s *Config) { d.VoiceAgent.Provider = s.VoiceAgent.Provider },
	},
	{
		name:   "audit.retention_days",
		pinned: func(p PolicyValues) bool { return p.AuditRetentionDays != nil },
		apply:  func(c *Config, p PolicyValues) { c.Audit.RetentionDays = *p.AuditRetentionDays },
		equal:  func(a, b *Config) bool { return a.Audit.RetentionDays == b.Audit.RetentionDays },
		copy:   func(d, s *Config) { d.Audit.RetentionDays = s.Audit.RetentionDays },
	},
	{
		name:   "audit.event_log_enabled",
		pinned: func(p PolicyValues) bool { return p.AuditEventLogEnabled != nil },
		apply:  func(c *Config, p PolicyValues) { c.Audit.EventLogEnabled = *p.AuditEventLogEnabled },
		equal:  func(a, b *Config) bool { return a.Audit.EventLogEnabled == b.Audit.EventLogEnabled },
		copy:   func(d, s *Config) { d.Audit.EventLogEnabled = s.Audit.EventLogEnabled },
	},
	{
		name:   "audit.otlp_endpoint",
		pinned: func(p PolicyValues) bool { return p.AuditOTLPEndpoint != "" },
		apply:  func(c *Config, p PolicyValues) { c.Audit.OTLPEndpoint = p.AuditOTLPEndpoint },
		equal:  func(a, b *Config) bool { return a.Audit.OTLPEndpoint == b.Audit.OTLPEndpoint },
		copy:   func(d, s *Config) { d.Audit.OTLPEndpoint = s.Audit.OTLPEndpoint },
	},
	{
		name:   "providers.foundry.entra_client_id",
		pinned: func(p PolicyValues) bool { return p.FoundryEntraClientID != "" },
		apply:  func(c *Config, p PolicyValues) { c.Providers.Foundry.EntraClientID = p.FoundryEntraClientID },
		equal: func(a, b *Config) bool {
			return a.Providers.Foundry.EntraClientID == b.Providers.Foundry.EntraClientID
		},
		copy: func(d, s *Config) { d.Providers.Foundry.EntraClientID = s.Providers.Foundry.EntraClientID },
	},
	{
		name:   "providers.foundry.entra_tenant_id",
		pinned: func(p PolicyValues) bool { return p.FoundryEntraTenantID != "" },
		apply:  func(c *Config, p PolicyValues) { c.Providers.Foundry.EntraTenantID = p.FoundryEntraTenantID },
		equal: func(a, b *Config) bool {
			return a.Providers.Foundry.EntraTenantID == b.Providers.Foundry.EntraTenantID
		},
		copy: func(d, s *Config) { d.Providers.Foundry.EntraTenantID = s.Providers.Foundry.EntraTenantID },
	},
}

// policyBinding is the policy a Config was loaded under. It travels with
// every copy of the Config (nextCfg := *cfg) so enforcement points and Save
// see it without a global.
type policyBinding struct {
	values PolicyValues
	// user holds the config values as they were before the overlay, so Save
	// writes the user's own choices for pinned keys and removing the policy
	// restores them.
	user *Config
}

// ErrPolicyLocked is returned when an edit tries to change a value an
// administrator policy pins.
var ErrPolicyLocked = errors.New("this setting is managed by an administrator policy and cannot be changed here")

// AttachPolicy binds policy to cfg and applies it. The values stay enforced
// for the life of the process: EnforcePolicy re-applies them, NetworkScope
// honours EnforceLocalOnly, and Save never persists them over the user's own
// values. Load calls it; tests and embedders that build a Config by hand use
// it to simulate a machine policy.
func AttachPolicy(cfg *Config, policy PolicyValues) {
	if cfg == nil {
		return
	}
	if cfg.policy != nil {
		// Re-attaching starts from the user's own values, not from the
		// previous overlay.
		for _, f := range policyFields {
			if f.pinned(cfg.policy.values) {
				f.copy(cfg, cfg.policy.user)
			}
		}
		cfg.policy = nil
	}
	if !policy.hasValues() {
		return
	}
	user := *cfg
	cfg.policy = &policyBinding{values: policy, user: &user}
	applyPolicyOverlay(cfg, policy)
}

// Policy returns the policy cfg was loaded under (zero value when none).
func (c *Config) Policy() PolicyValues {
	if c == nil || c.policy == nil {
		return PolicyValues{}
	}
	return c.policy.values
}

// EnforcePolicy re-applies the bound policy to cfg. Call it after code that
// derives pinned values from other settings (routing reconciliation, profile
// selection, presets) so a derived value can never undo the policy.
func EnforcePolicy(cfg *Config) {
	if cfg == nil || cfg.policy == nil {
		return
	}
	applyPolicyOverlay(cfg, cfg.policy.values)
}

// PolicyLockedFields names the config keys the bound policy pins, for the
// settings UI. "privacy.network_scope" is listed when EnforceLocalOnly caps
// the effective network scope.
func (c *Config) PolicyLockedFields() []string {
	if c == nil || c.policy == nil {
		return nil
	}
	var names []string
	for _, f := range policyFields {
		if f.pinned(c.policy.values) {
			names = append(names, f.name)
		}
	}
	if c.policy.values.EnforcesLocalOnly() {
		names = append(names, "privacy.network_scope")
	}
	return names
}

// CheckPolicyEdit rejects next when it changes a value the policy bound to c
// pins. Unchanged pinned values pass, so a form that echoes the enforced
// value back is not an error.
func (c *Config) CheckPolicyEdit(next *Config) error {
	if c == nil || c.policy == nil || next == nil {
		return nil
	}
	for _, f := range policyFields {
		if f.pinned(c.policy.values) && !f.equal(c, next) {
			return fmt.Errorf("%w (%s)", ErrPolicyLocked, f.name)
		}
	}
	return nil
}

// persistable returns the Config Save writes: cfg with every pinned key
// replaced by the user's own pre-policy value.
func persistable(cfg *Config) *Config {
	if cfg == nil || cfg.policy == nil {
		return cfg
	}
	out := *cfg
	for _, f := range policyFields {
		if f.pinned(cfg.policy.values) {
			f.copy(&out, cfg.policy.user)
		}
	}
	return &out
}
