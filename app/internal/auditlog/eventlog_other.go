//go:build !windows

package auditlog

import (
	"context"
	"time"
)

// InstallEventLogSource is a no-op on non-Windows platforms.
func InstallEventLogSource() error { return nil }

// OpenEventLog is a no-op on non-Windows platforms.
func OpenEventLog() error { return nil }

// CloseEventLog is a no-op on non-Windows platforms.
func CloseEventLog() {}

// emitToEventLog is a no-op on non-Windows platforms.
func emitToEventLog(_ Record) {}

// eventLogForwarder exists on every platform so isBuiltinSink compiles; it is
// never constructed off Windows.
type eventLogForwarder struct{}

func (eventLogForwarder) Name() string { return "event_log" }

func (eventLogForwarder) Bound() time.Duration { return EventLogForwardBound }

func (eventLogForwarder) Forward(context.Context, Record) error { return errNoForwardSink }

// eventLogSink is always nil on non-Windows platforms.
func eventLogSink() Forwarder { return nil }
