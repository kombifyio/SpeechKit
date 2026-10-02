package auditlog

import "time"

// SchemaVersion is bumped when the audit-event JSON shape changes in a way
// that breaks downstream SIEM ingestion. Increment requires a CHANGELOG.md
// entry under "## Audit-Log Schema" with migration notes.
//
// v2 (2026-06): added the prev_hash/hash tamper-evidence chain fields. The
// pre-v2 fields are unchanged, so v1 ingestion keeps working; consumers that
// want integrity verification read the new fields.
const SchemaVersion = "2"

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

	// EventModeStart is emitted when a SpeechKit mode runtime transitions to
	// Running (Outcome=success) or to Failed from Starting (Outcome=failure).
	// Source: subscribers of pkg/speechkit/lifecycle.Registry. Resource
	// fields: "mode" (string), "requires" (array of SharedDepKey, omitted
	// when empty), "error" (string, only on failure). See
	// docs/compliance/audit-event-catalog.md "mode.start" for the full
	// contract incl. the mode.start/mode.stop pairing rule.
	EventModeStart Event = "mode.start"

	// EventModeStop is emitted when a SpeechKit mode runtime transitions to
	// Stopped (Outcome=success) or to Failed from Stopping (Outcome=failure).
	// Resource fields mirror EventModeStart with "released" instead of
	// "requires" — the shared deps the runtime relinquished as it stopped.
	EventModeStop Event = "mode.stop"

	// EventPrivacyScopeChanged is emitted when the user changes the
	// [privacy] network_scope through the Settings surface. Resource fields:
	// "from" and "to" (scope identifiers only — never endpoint URLs, hosts,
	// or tokens) and "allow_setup_traffic" (bool, value after the change).
	EventPrivacyScopeChanged Event = "privacy.scope_changed"
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

	// PrevHash and Hash form a tamper-evidence chain. Hash binds this record's
	// content together with PrevHash (the previous record's Hash), so any
	// modification, reordering, or truncation breaks the chain at a verifiable
	// point. Hash is HMAC-SHA256 when an integrity key is configured, plain
	// SHA-256 otherwise. Both are populated by AppendEvent — never set them by
	// hand; they are excluded from the hash input when recomputed.
	PrevHash string `json:"prev_hash,omitempty"`
	Hash     string `json:"hash,omitempty"`
}
