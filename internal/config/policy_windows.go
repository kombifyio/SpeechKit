//go:build windows

package config

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	hklmPoliciesRoot = `SOFTWARE\Policies\kombify\SpeechKit`
	hklmDefaultsRoot = `SOFTWARE\kombify\SpeechKit`
	hkcuUserRoot     = `Software\kombify\SpeechKit`
)

// ReadPolicyValues walks the three registry roots in priority order and
// returns the effective policy overlay. HKLM\Policies values take precedence
// over HKLM\Defaults values; both take precedence over HKCU values.
// Missing keys and missing values are silently ignored — no registry entry
// means no override, and the TOML config.toml value is kept.
func ReadPolicyValues() PolicyValues {
	var result PolicyValues

	// 1. HKLM\Policies — locked overrides (highest priority)
	policies := readRegistryTree(registry.LOCAL_MACHINE, hklmPoliciesRoot)
	mergePolicy(&result, policies, "hklm-policies")

	// 2. HKLM\SpeechKit — admin defaults (user-overridable)
	defaults := readRegistryTree(registry.LOCAL_MACHINE, hklmDefaultsRoot)
	mergePolicy(&result, defaults, "hklm-defaults")

	// 3. HKCU — per-user preferences
	user := readRegistryTree(registry.CURRENT_USER, hkcuUserRoot)
	mergePolicy(&result, user, "hkcu")

	if result.Origin == "" {
		result.Origin = "none"
	}
	return result
}

// registryTree is the raw read result for one registry hive root.
// Nil pointer fields mean "not present in this hive".
type registryTree struct {
	updateEnabled         *bool
	updateManifestURL     string
	telemetryUpdateCheck  *bool
	providersEnforceLocal *bool
	voiceAgentAllowCloud  *bool
	auditRetentionDays    *int
	auditEventLogEnabled  *bool
	auditOTLPEndpoint     string
	keysFound             int
}

// readRegistryTree opens each sub-key under rootPath and reads the policy
// values we care about. Missing keys and missing values are not errors.
func readRegistryTree(hive registry.Key, rootPath string) registryTree {
	var t registryTree

	// Update\Enabled (DWORD) + Update\ManifestURL (REG_SZ)
	if k, err := registry.OpenKey(hive, rootPath+`\Update`, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if v, _, err := k.GetIntegerValue("Enabled"); err == nil {
			b := v != 0
			t.updateEnabled = &b
			t.keysFound++
		}
		if v, _, err := k.GetStringValue("ManifestURL"); err == nil && strings.TrimSpace(v) != "" {
			t.updateManifestURL = v
			t.keysFound++
		}
	}

	// Telemetry\UpdateCheck (DWORD)
	if k, err := registry.OpenKey(hive, rootPath+`\Telemetry`, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if v, _, err := k.GetIntegerValue("UpdateCheck"); err == nil {
			b := v != 0
			t.telemetryUpdateCheck = &b
			t.keysFound++
		}
	}

	// Providers\EnforceLocalOnly (DWORD)
	if k, err := registry.OpenKey(hive, rootPath+`\Providers`, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if v, _, err := k.GetIntegerValue("EnforceLocalOnly"); err == nil {
			b := v != 0
			t.providersEnforceLocal = &b
			t.keysFound++
		}
	}

	// VoiceAgent\AllowCloudProviders (DWORD)
	if k, err := registry.OpenKey(hive, rootPath+`\VoiceAgent`, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if v, _, err := k.GetIntegerValue("AllowCloudProviders"); err == nil {
			b := v != 0
			t.voiceAgentAllowCloud = &b
			t.keysFound++
		}
	}

	// Audit\RetentionDays (DWORD) + Audit\EventLogEnabled (DWORD) + Audit\OTLPEndpoint (REG_SZ)
	if k, err := registry.OpenKey(hive, rootPath+`\Audit`, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if v, _, err := k.GetIntegerValue("RetentionDays"); err == nil {
			i := int(v) //nolint:gosec // registry DWORD is always <= math.MaxUint32, fits int on any supported platform
			t.auditRetentionDays = &i
			t.keysFound++
		}
		if v, _, err := k.GetIntegerValue("EventLogEnabled"); err == nil {
			b := v != 0
			t.auditEventLogEnabled = &b
			t.keysFound++
		}
		if v, _, err := k.GetStringValue("OTLPEndpoint"); err == nil && strings.TrimSpace(v) != "" {
			t.auditOTLPEndpoint = v
			t.keysFound++
		}
	}

	return t
}

// mergePolicy applies a registryTree into result, respecting precedence: if a
// field in result is already set (by a higher-priority hive), it is not
// overwritten. Sets result.Origin to source when ANY field first contributes.
func mergePolicy(result *PolicyValues, t registryTree, source string) {
	contributed := false

	if t.updateEnabled != nil && result.UpdateEnabled == nil {
		result.UpdateEnabled = t.updateEnabled
		result.KeysFound++
		contributed = true
	}
	if t.updateManifestURL != "" && result.UpdateManifestURL == "" {
		result.UpdateManifestURL = t.updateManifestURL
		result.KeysFound++
		contributed = true
	}
	if t.telemetryUpdateCheck != nil && result.TelemetryUpdateCheck == nil {
		result.TelemetryUpdateCheck = t.telemetryUpdateCheck
		result.KeysFound++
		contributed = true
	}
	if t.providersEnforceLocal != nil && result.ProvidersEnforceLocalOnly == nil {
		result.ProvidersEnforceLocalOnly = t.providersEnforceLocal
		result.KeysFound++
		contributed = true
	}
	if t.voiceAgentAllowCloud != nil && result.VoiceAgentAllowCloud == nil {
		result.VoiceAgentAllowCloud = t.voiceAgentAllowCloud
		result.KeysFound++
		contributed = true
	}
	if t.auditRetentionDays != nil && result.AuditRetentionDays == nil {
		result.AuditRetentionDays = t.auditRetentionDays
		result.KeysFound++
		contributed = true
	}
	if t.auditEventLogEnabled != nil && result.AuditEventLogEnabled == nil {
		result.AuditEventLogEnabled = t.auditEventLogEnabled
		result.KeysFound++
		contributed = true
	}
	if t.auditOTLPEndpoint != "" && result.AuditOTLPEndpoint == "" {
		result.AuditOTLPEndpoint = t.auditOTLPEndpoint
		result.KeysFound++
		contributed = true
	}

	if contributed && result.Origin == "" {
		result.Origin = source
	}
}
