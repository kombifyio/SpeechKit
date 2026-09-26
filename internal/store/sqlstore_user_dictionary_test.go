package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestUserDictionaryEntriesReplaceListAndRecordUsage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	dictionaryStore, ok := s.(UserDictionaryStore)
	if !ok {
		t.Fatal("sqlite store does not implement UserDictionaryStore")
	}

	ctx := context.Background()
	err = dictionaryStore.ReplaceUserDictionaryEntries(ctx, "de", []UserDictionaryEntry{
		{Spoken: "kombi fire", Canonical: "Kombify", Source: "settings"},
		{Spoken: "AcmeOS", Canonical: "AcmeOS", Source: "settings"},
	})
	if err != nil {
		t.Fatalf("ReplaceUserDictionaryEntries: %v", err)
	}

	entries, err := dictionaryStore.ListUserDictionaryEntries(ctx, "de")
	if err != nil {
		t.Fatalf("ListUserDictionaryEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Spoken != "kombi fire" || entries[0].Canonical != "Kombify" || entries[0].Language != "de" {
		t.Fatalf("first entry = %+v", entries[0])
	}

	if err := dictionaryStore.RecordUserDictionaryUsage(ctx, "Kombify", "de"); err != nil {
		t.Fatalf("RecordUserDictionaryUsage: %v", err)
	}
	entries, err = dictionaryStore.ListUserDictionaryEntries(ctx, "de")
	if err != nil {
		t.Fatalf("ListUserDictionaryEntries after usage: %v", err)
	}
	if entries[0].UsageCount != 1 {
		t.Fatalf("usage count = %d, want 1", entries[0].UsageCount)
	}

	err = dictionaryStore.ReplaceUserDictionaryEntries(ctx, "de", []UserDictionaryEntry{
		{Spoken: "AcmeOS", Canonical: "AcmeOS", Source: "settings"},
	})
	if err != nil {
		t.Fatalf("ReplaceUserDictionaryEntries second pass: %v", err)
	}
	entries, err = dictionaryStore.ListUserDictionaryEntries(ctx, "de")
	if err != nil {
		t.Fatalf("ListUserDictionaryEntries second pass: %v", err)
	}
	if len(entries) != 1 || entries[0].Canonical != "AcmeOS" {
		t.Fatalf("entries after replace = %+v, want only AcmeOS", entries)
	}
}
