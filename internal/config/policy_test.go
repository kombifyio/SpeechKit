package config

import (
	"testing"
)

func ptrBool(b bool) *bool { return &b }
func ptrInt(i int) *int    { return &i }

func TestApplyPolicyOverlayHKLMOverridesConfig(t *testing.T) {
	cfg := &Config{}
	cfg.Update.Enabled = true
	cfg.Update.ManifestURL = "https://from-toml.example.com"
	cfg.Audit.RetentionDays = 90

	policy := PolicyValues{
		UpdateEnabled:      ptrBool(false),
		UpdateManifestURL:  "https://from-policy.example.com",
		AuditRetentionDays: ptrInt(365),
		Origin:             "hklm-policies",
		KeysFound:          3,
	}
	applyPolicyOverlay(cfg, policy)

	if cfg.Update.Enabled {
		t.Errorf("Update.Enabled: policy should force false")
	}
	if cfg.Update.ManifestURL != "https://from-policy.example.com" {
		t.Errorf("Update.ManifestURL: got %q, want %q", cfg.Update.ManifestURL, "https://from-policy.example.com")
	}
	if cfg.Audit.RetentionDays != 365 {
		t.Errorf("Audit.RetentionDays: want 365, got %d", cfg.Audit.RetentionDays)
	}
}

func TestApplyPolicyOverlayEmptyLeavesConfigAlone(t *testing.T) {
	cfg := &Config{}
	cfg.Update.Enabled = true
	cfg.Audit.RetentionDays = 90

	applyPolicyOverlay(cfg, PolicyValues{}) // empty policy — no fields set

	if !cfg.Update.Enabled {
		t.Errorf("Update.Enabled: empty policy should not change TOML value")
	}
	if cfg.Audit.RetentionDays != 90 {
		t.Errorf("Audit.RetentionDays: want 90, got %d", cfg.Audit.RetentionDays)
	}
}

func TestApplyPolicyOverlayProvidersEnforceLocalOnly(t *testing.T) {
	cfg := &Config{}
	cfg.Routing.Strategy = "dynamic"

	applyPolicyOverlay(cfg, PolicyValues{ProvidersEnforceLocalOnly: ptrBool(true)})

	if cfg.Routing.Strategy != "local-only" {
		t.Errorf("Routing.Strategy: want %q, got %q", "local-only", cfg.Routing.Strategy)
	}
}

func TestApplyPolicyOverlayProvidersEnforceLocalOnlyFalseNoOp(t *testing.T) {
	cfg := &Config{}
	cfg.Routing.Strategy = "dynamic"

	// EnforceLocalOnly=false (DWORD=0) should NOT force local-only.
	applyPolicyOverlay(cfg, PolicyValues{ProvidersEnforceLocalOnly: ptrBool(false)})

	if cfg.Routing.Strategy != "dynamic" {
		t.Errorf("Routing.Strategy: false enforce-local-only should not change strategy, got %q", cfg.Routing.Strategy)
	}
}

func TestApplyPolicyOverlayVoiceAgentBlocksCloud(t *testing.T) {
	cfg := &Config{}
	cfg.VoiceAgent.Provider = "gemini"

	applyPolicyOverlay(cfg, PolicyValues{VoiceAgentAllowCloud: ptrBool(false)})

	if cfg.VoiceAgent.Provider != "local-cascaded" {
		t.Errorf("VoiceAgent.Provider: want %q, got %q", "local-cascaded", cfg.VoiceAgent.Provider)
	}
}

func TestApplyPolicyOverlayVoiceAgentAllowCloudTrueNoOp(t *testing.T) {
	cfg := &Config{}
	cfg.VoiceAgent.Provider = "gemini"

	// AllowCloudProviders=true (DWORD=1) should NOT override the provider.
	applyPolicyOverlay(cfg, PolicyValues{VoiceAgentAllowCloud: ptrBool(true)})

	if cfg.VoiceAgent.Provider != "gemini" {
		t.Errorf("VoiceAgent.Provider: AllowCloud=true should not change provider, got %q", cfg.VoiceAgent.Provider)
	}
}

func TestApplyPolicyOverlayAuditFields(t *testing.T) {
	cfg := &Config{}
	cfg.Audit.RetentionDays = 30
	cfg.Audit.EventLogEnabled = false
	cfg.Audit.OTLPEndpoint = ""

	applyPolicyOverlay(cfg, PolicyValues{
		AuditRetentionDays:   ptrInt(180),
		AuditEventLogEnabled: ptrBool(true),
		AuditOTLPEndpoint:    "https://otel.internal.example.com:4317",
	})

	if cfg.Audit.RetentionDays != 180 {
		t.Errorf("Audit.RetentionDays: want 180, got %d", cfg.Audit.RetentionDays)
	}
	if !cfg.Audit.EventLogEnabled {
		t.Errorf("Audit.EventLogEnabled: want true")
	}
	if cfg.Audit.OTLPEndpoint != "https://otel.internal.example.com:4317" {
		t.Errorf("Audit.OTLPEndpoint: got %q", cfg.Audit.OTLPEndpoint)
	}
}

func TestApplyPolicyOverlayTelemetryUpdateCheck(t *testing.T) {
	cfg := &Config{}
	cfg.Telemetry.UpdateCheck = true

	applyPolicyOverlay(cfg, PolicyValues{TelemetryUpdateCheck: ptrBool(false)})

	if cfg.Telemetry.UpdateCheck {
		t.Errorf("Telemetry.UpdateCheck: policy should force false")
	}
}
