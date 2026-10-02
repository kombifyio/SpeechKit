package auditlog

import (
	"testing"
)

func TestEmitToEventLogNoOpWhenNotConfigured(t *testing.T) {
	// eventLogHandle is nil by default; must not panic.
	emitToEventLog(Record{Event: EventProviderSelected})
}

func TestCloseEventLogIsIdempotent(t *testing.T) {
	CloseEventLog()
	CloseEventLog()
}
