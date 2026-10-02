package auditlogtest

import "github.com/kombifyio/SpeechKit/app/internal/auditlog"

// Reset clears all audit-log package state so the next Configure starts
// fresh. Use in t.Cleanup to isolate tests that exercise the audit log.
//
// Forwards to app/internal/auditlog.ResetForTests which is exported only so
// this subpackage can reach it; do not call ResetForTests directly from
// other packages or from production code.
func Reset() {
	auditlog.ResetForTests()
}
