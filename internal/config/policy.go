package config

// PolicyValues holds the registry-resolved subset of Config that can be
// overridden via ADMX/GPO on Windows. Pointer fields signal "set vs not set"
// for booleans and ints; empty strings signal "not set" for REG_SZ values.
//
// The struct is defined here (build-tag-neutral) so platform-neutral tests
// of applyPolicyOverlay compile on every target. ReadPolicyValues remains
// platform-specific: see policy_windows.go (real registry walk) and
// policy_other.go (stub returning zero-value overlay).
//
// Registry layout consumed by ReadPolicyValues (highest-priority first):
//
//	HKLM\SOFTWARE\Policies\kombify\SpeechKit\  — admin-locked (GPO)
//	HKLM\SOFTWARE\kombify\SpeechKit\           — admin defaults (user-overridable)
//	HKCU\Software\kombify\SpeechKit\           — user-only (UI prefs)
type PolicyValues struct {
	UpdateEnabled             *bool
	UpdateManifestURL         string
	TelemetryUpdateCheck      *bool
	ProvidersEnforceLocalOnly *bool
	VoiceAgentAllowCloud      *bool
	AuditRetentionDays        *int
	AuditEventLogEnabled      *bool
	AuditOTLPEndpoint         string

	// Origin describes which registry hive first contributed a value.
	// One of "hklm-policies" | "hklm-defaults" | "hkcu" | "none" | "non-windows".
	Origin string
	// KeysFound counts all registry values that contributed to this overlay
	// (across all hives). Used in the policy.applied audit event.
	KeysFound int
}

// applyPolicyOverlay mutates cfg in place with the values from policy.
// It is called from Load() after TOML decode and before the existing
// backfill chain. Policy values always win over config.toml values.
//
// On non-Windows, ReadPolicyValues returns a zero-value PolicyValues,
// so this function is effectively a no-op there.
func applyPolicyOverlay(cfg *Config, policy PolicyValues) {
	if policy.UpdateEnabled != nil {
		cfg.Update.Enabled = *policy.UpdateEnabled
	}
	if policy.UpdateManifestURL != "" {
		cfg.Update.ManifestURL = policy.UpdateManifestURL
	}
	if policy.TelemetryUpdateCheck != nil {
		cfg.Telemetry.UpdateCheck = *policy.TelemetryUpdateCheck
	}
	// EnforceLocalOnly=1 forces the STT routing strategy to "local-only".
	if policy.ProvidersEnforceLocalOnly != nil && *policy.ProvidersEnforceLocalOnly {
		cfg.Routing.Strategy = "local-only"
	}
	// AllowCloudProviders=0 forces Voice Agent to the on-prem cascaded pipeline.
	if policy.VoiceAgentAllowCloud != nil && !*policy.VoiceAgentAllowCloud {
		cfg.VoiceAgent.Provider = "local-cascaded"
	}
	if policy.AuditRetentionDays != nil {
		cfg.Audit.RetentionDays = *policy.AuditRetentionDays
	}
	if policy.AuditEventLogEnabled != nil {
		cfg.Audit.EventLogEnabled = *policy.AuditEventLogEnabled
	}
	if policy.AuditOTLPEndpoint != "" {
		cfg.Audit.OTLPEndpoint = policy.AuditOTLPEndpoint
	}
}
