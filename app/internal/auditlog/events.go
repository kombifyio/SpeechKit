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
	// EventAuthIdentityChanged is emitted when the resolved Entra client id
	// or tenant id changes while the process runs and the previous account
	// is signed out. Resource fields: "reason" (string), "signed_out"
	// (bool). Never carries ids, account names or tokens.
	EventAuthIdentityChanged Event = "auth.identity_changed"
	EventPrivacyExport       Event = "privacy.export"
	EventPrivacyDelete       Event = "privacy.delete"
	// EventPolicyApplied is emitted once at startup after config is loaded,
	// when registry policy values are present. Resource fields: "source"
	// (registry hive that first contributed a value) and "keys_locked_count"
	// (total number of registry values that overrode config.toml).
	EventPolicyApplied Event = "policy.applied"
	// EventPolicyRefreshed is emitted when the machine policy changes while
	// SpeechKit runs. Resource fields: "source", "tightened_keys" (applied at
	// once), "deferred_keys" (loosenings that wait for the next start) and the
	// key counts of the policy now bound.
	EventPolicyRefreshed Event = "policy.refreshed"

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

	// EventPrivacyRetentionSweep is emitted when the [privacy] retention
	// scope (ephemeral) or the retention TTL removes stored content. Resource
	// fields: "reason" (scope | ttl), "trigger" (session_end | maintenance),
	// "session_id" (number, session_end only) and one count per kind of
	// content removed. Counts only — never transcript text, titles or paths.
	EventPrivacyRetentionSweep Event = "privacy.retention_sweep"
	// EventAuditStarted is the first record every configuration of the audit
	// log writes. Resource fields: "last_seq" (the sequence number of the last
	// record found in the log directory, 0 when none), "reason"
	// (AuditStartReason values), "forward_required" (bool) and "sinks" (array
	// of forward sink names). An off-box consumer reads it to tell a restart
	// from a gap: the restart record's seq is last_seq+1.
	EventAuditStarted Event = "audit.started"

	// EventAuditForwardDegraded is written when a forward-required record was
	// acknowledged by no forward sink within the bound. It is written to the
	// local file and queued for redelivery; cloud sessions are refused until a
	// forward succeeds. Resource fields: "seq" (the record that failed),
	// "reason" (no_sink | timeout | sink_error).
	EventAuditForwardDegraded Event = "audit.forward_degraded"

	// EventAuditForwardRestored is written when a forward succeeds again after
	// a degraded period. Resource fields: "degraded_seconds" (number),
	// "pending_seq_from" and "pending_seq_to" (the range that had not been
	// acknowledged when forwarding recovered; redelivery follows).
	EventAuditForwardRestored Event = "audit.forward_restored"

	// EventDataExport is emitted when one stored item leaves the store as a
	// copy: a transcript copied to the clipboard, a meeting write-up exported
	// to a file or downloaded. Resource fields: "item_kind"
	// (transcription | meeting_writeup), "item_id" (number, 0 when the item
	// has no stored id), "action" (copy | export | download), "destination"
	// (clipboard | file | http_response), "bytes" (number). Never content,
	// file paths or titles.
	EventDataExport Event = "data.export"

	// EventDataDelete is emitted when one stored item is deleted by the user.
	// Resource fields: "item_kind" (transcription | meeting), "item_id"
	// (number), and on failure "error_code" (not_found | store_error).
	EventDataDelete Event = "data.delete"

	// EventMeetingTranscriptPasted is emitted when a transcript the user pasted
	// or uploaded is appended to a finished meeting as Teams segments. Resource
	// fields: "session_id" (number), "source_format" (webvtt | teams_text),
	// "input" (text | file), "cue_count" (number) and "replaced" (bool). Never
	// transcript text, speaker names or titles.
	EventMeetingTranscriptPasted Event = "meeting.transcript_pasted"

	// EventMeetingSnapshotCaptured is emitted when a screenshot was saved to a
	// meeting. Resource fields: "session_id" (number), "source" (window |
	// monitor) and "bytes" (number, the size of the image). Never a window
	// title, a process name, a path or image content.
	EventMeetingSnapshotCaptured Event = "meeting.snapshot_captured"

	// EventMeetingExport is emitted when the notes export of a meeting to an
	// export target (OneNote) starts, succeeds or fails. Resource fields:
	// "session_id" (number), "target" (the target id, such as onenote),
	// "stage" (started | synced | failed) and, on a failure, "status_id" (the
	// stable sk.export.* id). Outcome is failure for a failed export. Never a
	// meeting title, notes, page content, a notebook or section name, or a
	// destination id.
	EventMeetingExport Event = "meeting.export"

	// EventPolicyBlocked is emitted when a policy refuses an action the user
	// or the app asked for. Resource fields: "policy" (the policy name, e.g.
	// VoiceAgent\AllowCloudProviders or Audit\ForwardRequired), "action"
	// (the refused action, e.g. voiceagent.session.start), "detail"
	// (optional, a non-secret qualifier such as the pinned config key).
	EventPolicyBlocked Event = "policy.blocked"

	// EventModelLoad is emitted when a model file is downloaded or loaded by
	// a local runtime. Resource fields: "action" (download | load),
	// "runtime" (whisper | llama | ollama | http), "model_id" (catalog id when
	// known), "model_file" (base file name only, never a path), "bytes"
	// (number), "sha256_verified" (bool, downloads only), and on failure
	// "error_code".
	EventModelLoad Event = "model.load"
)

// AuditStartReason values for EventAuditStarted's "reason" field.
const (
	// AuditStartFirst: no earlier record exists in the log directory (a fresh
	// install, or the local files were removed; an off-box consumer that has
	// seen earlier sequence numbers treats the latter as a reset).
	AuditStartFirst = "first_start"
	// AuditStartProcess: the process started and continued an existing log.
	AuditStartProcess = "process_start"
	// AuditStartReconfigured: the running process configured the log again.
	AuditStartReconfigured = "reconfigured"
)

// Actor identifies who or what triggered the audited action.
type Actor struct {
	UserSID   string `json:"user_sid,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// Record is the serialised shape of one audit-log line.
type Record struct {
	SchemaVersion string `json:"_schema_version"`
	// Seq is the per-install sequence number: every record carries the
	// previous record's Seq plus one, across restarts and day files.
	// AppendEvent sets it; never set it by hand. Zero only on records written
	// before sequence numbers existed.
	Seq       uint64         `json:"seq,omitempty"`
	Timestamp time.Time      `json:"ts"`
	Event     Event          `json:"event"`
	Actor     Actor          `json:"actor"`
	Resource  map[string]any `json:"resource,omitempty"`
	Outcome   Outcome        `json:"outcome"`
	TraceID   string         `json:"trace_id,omitempty"`

	// PrevHash and Hash form a tamper-evidence chain. Hash binds this record's
	// content together with PrevHash (the previous record's Hash), so any
	// modification, reordering, or truncation breaks the chain at a verifiable
	// point. Hash is HMAC-SHA256 when an integrity key is configured, plain
	// SHA-256 otherwise. Both are populated by AppendEvent — never set them by
	// hand; they are excluded from the hash input when recomputed.
	PrevHash string `json:"prev_hash,omitempty"`
	Hash     string `json:"hash,omitempty"`
}
