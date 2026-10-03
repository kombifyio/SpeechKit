package store

import (
	"context"
	"path/filepath"
	"testing"
)

// A live dictation extends one history entry instead of adding one per final.
func TestUpdateTranscriptionTextExtendsTheSameEntry(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "session.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	id, err := s.CreateTranscription(ctx, "Erster Satz.", "de", "deepgram", "nova-3", 0, 120, nil)
	if err != nil {
		t.Fatalf("CreateTranscription: %v", err)
	}
	if err := s.UpdateTranscriptionText(ctx, id, "Erster Satz. Zweiter Satz.", 4200, 120); err != nil {
		t.Fatalf("UpdateTranscriptionText: %v", err)
	}

	list, err := s.ListTranscriptions(ctx, ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListTranscriptions: %v", err)
	}
	if len(list) != 1 || list[0].Text != "Erster Satz. Zweiter Satz." || list[0].DurationMs != 4200 {
		t.Fatalf("history = %#v, want one entry with the joined text", list)
	}
}
