package auditlog

import "time"

// SchemaVersion is bumped when the audit-event JSON shape changes in a way
// that breaks downstream SIEM ingestion. Increment requires a CHANGELOG.md
// entry under "## Audit-Log Schema" with migration notes.
const SchemaVersion = "1"

// Outcome is the success/failure state of the audited action.
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
)

// Event is the namespaced identifier of an audit event type. Every event used
// in code MUST appear here and in docs/compliance/audit-event-catalog.md.
type Event string

const (
	EventProviderSelected       Event = "provider.selected"
	EventVoiceAgentSessionStart Event = "voiceagent.session.start"
	EventVoiceAgentSessionEnd   Event = "voiceagent.session.end"
	EventSettingsChanged        Event = "settings.changed"
	EventUpdateInstalled        Event = "update.installed"
	EventAuthFailed             Event = "auth.failed"
	EventPrivacyExport          Event = "privacy.export"
	EventPrivacyDelete          Event = "privacy.delete"
	// EventPolicyApplied is emitted once at startup after config is loaded,
	// when registry policy values are present. Resource fields: "source"
	// (registry hive that first contributed a value) and "keys_locked_count"
	// (total number of registry values that overrode config.toml).
	EventPolicyApplied Event = "policy.applied"

	// EventBYOKKeyUpdated is emitted when the customer sets a new BYOK API key
	// for any provider (OpenAI, Groq, Google, HuggingFace). Resource fields:
	// "provider_name" (string), "region" (string, empty for non-regional
	// providers), "fingerprint_truncated" (first 16 hex chars of SHA-256 of the
	// key — correlates events without leaking the key itself).
	EventBYOKKeyUpdated Event = "byok.key_updated"
)

// Actor identifies who or what triggered the audited action.
type Actor struct {
	UserSID   string `json:"user_sid,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// Record is the serialised shape of one audit-log line.
type Record struct {
	SchemaVersion string         `json:"_schema_version"`
	Timestamp     time.Time      `json:"ts"`
	Event         Event          `json:"event"`
	Actor         Actor          `json:"actor"`
	Resource      map[string]any `json:"resource,omitempty"`
	Outcome       Outcome        `json:"outcome"`
	TraceID       string         `json:"trace_id,omitempty"`
}
