//go:build windows

package auditlog

import (
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/app/internal/productid"
)

// The reference client keeps the Event Log source its installer registered,
// so collectors that filter on it keep receiving the audit trail.
func TestReferenceKeepsTheEventLogSource(t *testing.T) {
	productid.UseForTest(t, productid.Reference)
	source, err := eventSourceName()
	if err != nil || source != "kombify SpeechKit" {
		t.Fatalf("event log source = %q, %v", source, err)
	}
}

// Without a product identity there is no source to register or write to.
func TestEventLogWithoutProductIdentityFailsClosed(t *testing.T) {
	productid.ClearForTest(t)
	if err := InstallEventLogSource(); !errors.Is(err, productid.ErrNoIdentity) {
		t.Fatalf("install without identity: want ErrNoIdentity, got %v", err)
	}
}
