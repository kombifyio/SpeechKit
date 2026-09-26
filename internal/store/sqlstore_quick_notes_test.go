package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLiteQuickNoteCaptureUpdateReplacesAudioAndCountsRows(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, SaveAudio: true, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	id, err := s.SaveQuickNote(ctx, "draft", "de", "manual", 500, 50, []byte("old audio"))
	if err != nil {
		t.Fatalf("SaveQuickNote: %v", err)
	}
	before, err := s.GetQuickNote(ctx, id)
	if err != nil {
		t.Fatalf("GetQuickNote before update: %v", err)
	}
	if before.AudioPath == "" {
		t.Fatal("expected initial audio path")
	}

	if err := s.UpdateQuickNoteCapture(ctx, id, "final note", "hf", 1300, 90, []byte("new audio")); err != nil {
		t.Fatalf("UpdateQuickNoteCapture: %v", err)
	}
	after, err := s.GetQuickNote(ctx, id)
	if err != nil {
		t.Fatalf("GetQuickNote after update: %v", err)
	}
	if after.Text != "final note" || after.Provider != "hf" || after.DurationMs != 1300 || after.LatencyMs != 90 {
		t.Fatalf("updated note = %+v", after)
	}
	if after.AudioPath == "" || after.AudioPath == before.AudioPath {
		t.Fatalf("audio path after update = %q, before %q", after.AudioPath, before.AudioPath)
	}
	if _, err := os.Stat(before.AudioPath); !os.IsNotExist(err) {
		t.Fatalf("old audio path should be removed, stat err=%v", err)
	}
	if after.Audio == nil || after.Audio.SizeBytes != int64(len("new audio")) {
		t.Fatalf("audio asset = %+v", after.Audio)
	}
	var assetCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audio_assets WHERE owner_kind = ? AND owner_id = ?`, "quick_note", id).Scan(&assetCount); err != nil {
		t.Fatalf("query audio assets: %v", err)
	}
	if assetCount != 1 {
		t.Fatalf("audio asset rows = %d, want 1", assetCount)
	}
	count, err := s.QuickNoteCount(ctx)
	if err != nil {
		t.Fatalf("QuickNoteCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("QuickNoteCount = %d, want 1", count)
	}
	if s.DB() == nil {
		t.Fatal("DB should expose sqlite handle")
	}
}

func TestSQLiteQuickNoteMutationsReportMissingRows(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "test.db"), MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	for name, mutate := range map[string]func() error{
		"update text":    func() error { return s.UpdateQuickNote(ctx, 404, "missing") },
		"update capture": func() error { return s.UpdateQuickNoteCapture(ctx, 404, "missing", "hf", 1, 1, nil) },
		"pin":            func() error { return s.PinQuickNote(ctx, 404, true) },
	} {
		t.Run(name, func(t *testing.T) {
			err := mutate()
			if err == nil {
				t.Fatal("expected missing quick note error")
			}
			if !strings.Contains(err.Error(), "quick note") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSaveQuickNote(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	id, err := s.SaveQuickNote(context.Background(), "Meeting notes from standup", "en", "huggingface", 4200, 320, nil)
	if err != nil {
		t.Fatalf("SaveQuickNote: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive ID, got %d", id)
	}

	count, _ := s.QuickNoteCount(context.Background())
	if count != 1 {
		t.Errorf("expected 1 quick note, got %d", count)
	}
}

func TestRecentQuickNotesOrderAndLimit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	for i := 0; i < 5; i++ {
		_, err := s.SaveQuickNote(context.Background(), fmt.Sprintf("note-%d", i), "en", "manual", 0, 0, nil)
		if err != nil {
			t.Fatalf("SaveQuickNote %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	notes, err := s.ListQuickNotes(context.Background(), ListOpts{Limit: 3})
	if err != nil {
		t.Fatalf("RecentQuickNotes: %v", err)
	}
	if len(notes) != 3 {
		t.Fatalf("expected 3 notes, got %d", len(notes))
	}
	if notes[0].Text != "note-4" {
		t.Errorf("most recent should be note-4, got %q", notes[0].Text)
	}
}

func TestUpdateQuickNote(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	id, _ := s.SaveQuickNote(context.Background(), "original text", "en", "manual", 0, 0, nil)

	if err := s.UpdateQuickNote(context.Background(), id, "updated text"); err != nil {
		t.Fatalf("UpdateQuickNote: %v", err)
	}

	notes, _ := s.ListQuickNotes(context.Background(), ListOpts{Limit: 1})
	if len(notes) != 1 || notes[0].Text != "updated text" {
		t.Fatalf("expected updated text, got %q", notes[0].Text)
	}
}

func TestUpdateQuickNoteNotFound(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if err := s.UpdateQuickNote(context.Background(), 999, "text"); err == nil {
		t.Fatal("expected error for non-existent ID")
	}
}

func TestDeleteQuickNote(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	id, _ := s.SaveQuickNote(context.Background(), "to delete", "en", "manual", 0, 0, nil)

	if err := s.DeleteQuickNote(context.Background(), id); err != nil {
		t.Fatalf("DeleteQuickNote: %v", err)
	}

	count, _ := s.QuickNoteCount(context.Background())
	if count != 0 {
		t.Errorf("expected 0 notes after delete, got %d", count)
	}
}

func TestDeleteQuickNoteNotFound(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if err := s.DeleteQuickNote(context.Background(), 999); err == nil {
		t.Fatal("expected error for non-existent ID")
	}
}

// ---------------------------------------------------------------------------
// Factory tests
// ---------------------------------------------------------------------------
