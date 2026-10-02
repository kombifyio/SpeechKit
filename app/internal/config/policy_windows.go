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

// ReadPolicyValues reads the three registry roots and returns the effective
// policy overlay. The precedence and the "HKCU may only restrict" rule live in
// the platform-neutral mergePolicyHives (policy.go); this file only reads.
// Missing keys and missing values are silently ignored — no registry entry
// means no override, and the config.toml value is kept.
func ReadPolicyValues() PolicyValues {
	return mergePolicyHives(
		readRegistryTree(registry.LOCAL_MACHINE, hklmPoliciesRoot),
		readRegistryTree(registry.LOCAL_MACHINE, hklmDefaultsRoot),
		readRegistryTree(registry.CURRENT_USER, hkcuUserRoot),
	)
}

// readRegistryTree opens each sub-key under rootPath and reads the policy
// values we care about. Missing keys and missing values are not errors.
func readRegistryTree(hive registry.Key, rootPath string) policyHive {
	var t policyHive

	// Update\Enabled (DWORD) + Update\ManifestURL (REG_SZ)
	if k, err := registry.OpenKey(hive, rootPath+`\Update`, registry.QUERY_VALUE); err == nil {
		defer k.Close() //nolint:errcheck // Windows registry key close error is not actionable for read-only policy probes
		if v, _, err := k.GetIntegerValue("Enabled"); err == nil {
			b := v != 0
			t.updateEnabled = &b
		}
		if v, _, err := k.GetStringValue("ManifestURL"); err == nil && strings.TrimSpace(v) != "" {
			t.updateManifestURL = v
		}
	}

	// Telemetry\UpdateCheck (DWORD)
	if k, err := registry.OpenKey(hive, rootPath+`\Telemetry`, registry.QUERY_VALUE); err == nil {
		defer k.Close() //nolint:errcheck // Windows registry key close error is not actionable for read-only policy probes
		if v, _, err := k.GetIntegerValue("UpdateCheck"); err == nil {
			b := v != 0
			t.telemetryUpdateCheck = &b
		}
	}

	// Providers\EnforceLocalOnly (DWORD)
	if k, err := registry.OpenKey(hive, rootPath+`\Providers`, registry.QUERY_VALUE); err == nil {
		defer k.Close() //nolint:errcheck // Windows registry key close error is not actionable for read-only policy probes
		if v, _, err := k.GetIntegerValue("EnforceLocalOnly"); err == nil {
			b := v != 0
			t.providersEnforceLocal = &b
		}
	}

	// VoiceAgent\AllowCloudProviders (DWORD)
	if k, err := registry.OpenKey(hive, rootPath+`\VoiceAgent`, registry.QUERY_VALUE); err == nil {
		defer k.Close() //nolint:errcheck // Windows registry key close error is not actionable for read-only policy probes
		if v, _, err := k.GetIntegerValue("AllowCloudProviders"); err == nil {
			b := v != 0
			t.voiceAgentAllowCloud = &b
		}
	}

	// Audit\RetentionDays (DWORD) + Audit\EventLogEnabled (DWORD) + Audit\OTLPEndpoint (REG_SZ)
	if k, err := registry.OpenKey(hive, rootPath+`\Audit`, registry.QUERY_VALUE); err == nil {
		defer k.Close() //nolint:errcheck // Windows registry key close error is not actionable for read-only policy probes
		if v, _, err := k.GetIntegerValue("RetentionDays"); err == nil {
			if v <= uint64(int(^uint(0)>>1)) {
				i := int(v) // #nosec G115 -- guarded against int overflow before conversion.
				t.auditRetentionDays = &i
			}
		}
		if v, _, err := k.GetIntegerValue("EventLogEnabled"); err == nil {
			b := v != 0
			t.auditEventLogEnabled = &b
		}
		if v, _, err := k.GetStringValue("OTLPEndpoint"); err == nil && strings.TrimSpace(v) != "" {
			t.auditOTLPEndpoint = v
		}
	}

	// Foundry\EntraClientId (REG_SZ) + Foundry\EntraTenantId (REG_SZ): the
	// company's own Microsoft Entra app registration for the Foundry sign-in.
	if k, err := registry.OpenKey(hive, rootPath+`\Foundry`, registry.QUERY_VALUE); err == nil {
		defer k.Close() //nolint:errcheck // Windows registry key close error is not actionable for read-only policy probes
		if v, _, err := k.GetStringValue("EntraClientId"); err == nil && strings.TrimSpace(v) != "" {
			t.foundryEntraClientID = strings.TrimSpace(v)
		}
		if v, _, err := k.GetStringValue("EntraTenantId"); err == nil && strings.TrimSpace(v) != "" {
			t.foundryEntraTenantID = strings.TrimSpace(v)
		}
	}

	return t
}
