# SpeechKit ADMX Templates

Group Policy administrative templates for kombify SpeechKit.

## Quick install (single host)

Copy the files to the local Policy Definitions store:

```powershell
Copy-Item SpeechKit.admx "$env:SystemRoot\PolicyDefinitions\"
Copy-Item en-US\SpeechKit.adml "$env:SystemRoot\PolicyDefinitions\en-US\"
# Optionally also copy de-DE\SpeechKit.adml for German language packs.
```

Open `gpedit.msc`, navigate to:
**Computer Configuration > Administrative Templates > kombify > SpeechKit**

## Domain-wide deployment (Central Store / GPO)

For domain-wide deployment via the Group Policy Central Store, follow the complete runbook:

**[docs/runbooks/admx-deployment.md](../../docs/runbooks/admx-deployment.md)**

It covers: Central Store deployment, precedence verification, rollback, registry audit via PowerShell, and integration into SCCM / Intune.

## Available policies

| Policy | Registry key | config.toml override |
|--------|-------------|---------------------|
| Enable automatic updates | `Update\Enabled` (DWORD) | `[update] enabled` |
| Update manifest URL | `Update\ManifestURL` (REG_SZ) | `[update] manifest_url` |
| Enable update-check telemetry | `Telemetry\UpdateCheck` (DWORD) | `[telemetry] update_check` |
| Enforce local-only STT | `Providers\EnforceLocalOnly` (DWORD) | `[routing] strategy = "local-only"` |
| Allow cloud Voice Agent | `VoiceAgent\AllowCloudProviders` (DWORD) | `[voice_agent] provider = "local-cascaded"` when 0 |
| Audit retention (days) | `Audit\RetentionDays` (DWORD) | `[audit] retention_days` |
| Audit Event Log mirror | `Audit\EventLogEnabled` (DWORD) | `[audit] event_log_enabled` |
| Audit OTLP endpoint | `Audit\OTLPEndpoint` (REG_SZ) | `[audit] otlp_endpoint` |

All policies live under `HKLM\SOFTWARE\Policies\kombify\SpeechKit\` (GPO-locked).
Admin defaults (user-overridable) use `HKLM\SOFTWARE\kombify\SpeechKit\`.
