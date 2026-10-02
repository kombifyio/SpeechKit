//go:build !windows

package auditlog

// InstallEventLogSource is a no-op on non-Windows platforms.
func InstallEventLogSource() error { return nil }

// OpenEventLog is a no-op on non-Windows platforms.
func OpenEventLog() error { return nil }

// CloseEventLog is a no-op on non-Windows platforms.
func CloseEventLog() {}

// emitToEventLog is a no-op on non-Windows platforms.
func emitToEventLog(_ Record) {}
