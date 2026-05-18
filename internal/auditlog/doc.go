// Package auditlog provides the dedicated audit-event stream for SpeechKit.
//
// Audit events are separate from the runtime log (cmd/speechkit/logging.go):
// the runtime log is for operator troubleshooting (debug-level slog JSON);
// the audit log is the customer's source of truth for SOC 2 / ISO 27001 /
// BSI C5 evidence and DSGVO Art. 30 verification.
//
// Schema is documented in docs/compliance/audit-event-catalog.md and versioned
// via the _schema_version field. Major-version bumps require a CHANGELOG entry
// with SIEM-ingestion migration notes.
//
// Usage:
//
//	if err := auditlog.AppendEvent(ctx, auditlog.Record{
//	    Event: auditlog.EventProviderSelected,
//	    Actor: auditlog.Actor{UserSID: sid},
//	    Resource: map[string]any{
//	        "provider_name": "whisper-cpp",
//	        "provider_kind": "stt",
//	        "endpoint_host": "127.0.0.1:8123",
//	        "strategy":      "local-only",
//	    },
//	    Outcome: auditlog.OutcomeSuccess,
//	}); err != nil {
//	    slog.Warn("audit-log append failed", "err", err)
//	}
//
// AppendEvent never returns an error that should crash the call site; it
// reports the failure via the returned error so the runtime log captures it.
// Call sites log-and-continue rather than abort the user action.
package auditlog
