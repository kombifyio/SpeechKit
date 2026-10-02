//go:build windows

package auditlog

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sys/windows/svc/eventlog"
)

const eventSourceName = "kombify SpeechKit"

// eventID 1000 is used for all SpeechKit audit events; the rec.Event
// string distinguishes the type. Microsoft recommends 0–65535 range;
// 1000 is a safe choice for an application-defined ID.
const eventID = 1000

var (
	eventLogMu     sync.Mutex
	eventLogHandle *eventlog.Log // nil when sink disabled
)

// InstallEventLogSource registers the "kombify SpeechKit" Event Log
// source. Idempotent: calling when already registered is a no-op
// (eventlog.InstallAsEventCreate returns an error containing
// "already exists" for re-registration — we treat that as success).
//
// Typical install patterns:
//   - MSI custom action calls this once during install (elevated)
//   - Admin runs `SpeechKit.exe --install-event-log-source` manually
//
// Non-admin users cannot register; their AuditConfig.EventLogEnabled
// will silently fail OpenEventLog at startup (logged at slog.Warn).
func InstallEventLogSource() error {
	err := eventlog.InstallAsEventCreate(eventSourceName, eventlog.Info|eventlog.Warning|eventlog.Error)
	if err != nil {
		// Treat "already installed" as success — idempotent re-registration.
		if isAlreadyInstalledError(err) {
			return nil
		}
		return fmt.Errorf("install event log source: %w", err)
	}
	return nil
}

// isAlreadyInstalledError returns true if err indicates the registry
// key for the source already exists from a prior install.
func isAlreadyInstalledError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "registry key already exists")
}

// OpenEventLog opens the registered source for writes. Called from
// Configure when AuditConfig.EventLogEnabled = true. Failure is
// non-fatal — the file sink remains active regardless.
func OpenEventLog() error {
	eventLogMu.Lock()
	defer eventLogMu.Unlock()
	if eventLogHandle != nil {
		return nil // already open
	}
	elog, err := eventlog.Open(eventSourceName)
	if err != nil {
		return fmt.Errorf("eventlog.Open(%q): %w", eventSourceName, err)
	}
	eventLogHandle = elog
	return nil
}

// CloseEventLog releases the Event Log handle. Used by ResetForTests
// and during graceful shutdown.
func CloseEventLog() {
	eventLogMu.Lock()
	defer eventLogMu.Unlock()
	if eventLogHandle != nil {
		_ = eventLogHandle.Close()
		eventLogHandle = nil
	}
}

// emitToEventLog writes one audit Record to the Windows Event Log.
// Called from AppendEvent when eventLogHandle is non-nil. Failures
// are silently dropped — the file sink already has the record.
func emitToEventLog(rec Record) {
	eventLogMu.Lock()
	h := eventLogHandle
	eventLogMu.Unlock()
	if h == nil {
		return
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	if rec.Outcome == OutcomeFailure {
		_ = h.Warning(eventID, string(line))
	} else {
		_ = h.Info(eventID, string(line))
	}
}
