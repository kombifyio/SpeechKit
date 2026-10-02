package store

import (
	"context"
	"path/filepath"
	"testing"

	"go.uber.org/goleak"
)

// Every write schedules retention maintenance. The daily maintenance loop must
// start once per store and stop on Close; it used to start once per write and
// only the last one was ever stopped, leaking a goroutine per saved note.
func TestStoreCloseStopsMaintenanceAfterManyWrites(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "maintenance.db"), MeetingRetentionDays: 30})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		if _, err := s.SaveQuickNote(ctx, "note", "en", "local", 1, 1, nil); err != nil {
			t.Fatalf("SaveQuickNote: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
