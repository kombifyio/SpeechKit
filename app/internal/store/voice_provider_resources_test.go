package store

import (
	"context"
	"path/filepath"
	"testing"
)

// Sensitive migration boundary: released provider intents survive restart and
// a stale create acknowledgement cannot reset their cleanup state.
func TestVoiceProviderJournalPreservesReleasedIdentityAcrossRestart(t *testing.T) {
	ctx := context.Background()
	cfg := StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "voice.db")}
	db, err := NewSQLiteStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveVoiceProviderResource(ctx, VoiceProviderResource{Name: "opaque", ExpiresAt: 123, Released: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = NewSQLiteStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.SaveVoiceProviderResource(ctx, VoiceProviderResource{Name: "opaque", VendorID: "late-ack", ExpiresAt: 123}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.ListVoiceProviderResources(ctx)
	if err != nil || len(rows) != 1 || !rows[0].Released || rows[0].VendorID != "late-ack" {
		t.Fatal("restart or stale acknowledgement lost released identity")
	}
	if err := db.DeleteVoiceProviderResource(ctx, "opaque"); err != nil {
		t.Fatal(err)
	}
	rows, err = db.ListVoiceProviderResources(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatal("acknowledged cleanup stayed pending")
	}
}
