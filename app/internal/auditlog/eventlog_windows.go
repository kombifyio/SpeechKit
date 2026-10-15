//go:build windows

package auditlog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows/svc/eventlog"

	"github.com/kombifyio/SpeechKit/app/internal/productid"
)

// eventSourceName is the registered product's Event Log source, its display
// name ("kombify SpeechKit" for the reference client), or
// productid.ErrNoIdentity.
func eventSourceName() (string, error) {
	p, err := productid.Current()
	if err != nil {
		return "", err
	}
	return p.DisplayName(), nil
}

// eventID 1000 is used for all SpeechKit audit events; the rec.Event
// string distinguishes the type. Microsoft recommends 0–65535 range;
// 1000 is a safe choice for an application-defined ID.
const eventID = 1000

var (
	eventLogMu     sync.Mutex
	eventLogHandle *eventlog.Log // nil when sink disabled
)

// InstallEventLogSource registers the product's Event Log source.
// Idempotent: calling when already registered is a no-op
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
	source, err := eventSourceName()
	if err != nil {
		return err
	}
	err = eventlog.InstallAsEventCreate(source, eventlog.Info|eventlog.Warning|eventlog.Error)
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
	source, err := eventSourceName()
	if err != nil {
		return err
	}
	elog, err := eventlog.Open(source)
	if err != nil {
		return fmt.Errorf("eventlog.Open(%q): %w", source, err)
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

// emitToEventLog writes one audit Record to the Windows Event Log, best
// effort: failures are dropped (the file sink has the record). Used when
// forward_required is off.
func emitToEventLog(rec Record) {
	eventLogMu.Lock()
	h := eventLogHandle
	eventLogMu.Unlock()
	if h == nil {
		return
	}
	_ = writeEventLog(h, rec)
}

func writeEventLog(h *eventlog.Log, rec Record) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if rec.Outcome == OutcomeFailure {
		return h.Warning(eventID, string(line))
	}
	return h.Info(eventID, string(line))
}

// eventLogForwarder is the Event Log as a forward-required sink. ReportEvent
// returns once the Event Log service has the record; Windows Event
// Forwarding takes it off the device from there.
type eventLogForwarder struct{ h *eventlog.Log }

func (eventLogForwarder) Name() string { return "event_log" }

func (eventLogForwarder) Bound() time.Duration { return EventLogForwardBound }

func (f eventLogForwarder) Forward(ctx context.Context, rec Record) error {
	done := make(chan error, 1)
	go func() { done <- writeEventLog(f.h, rec) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// eventLogSink returns the open Event Log sink, or nil.
func eventLogSink() Forwarder {
	eventLogMu.Lock()
	defer eventLogMu.Unlock()
	if eventLogHandle == nil {
		return nil
	}
	return eventLogForwarder{h: eventLogHandle}
}
