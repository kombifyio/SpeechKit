package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndRecent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100, TranscriptionModelHints: map[string]string{"huggingface": "openai/whisper-large-v3", "local": "ggml-small.bin"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if err := s.SaveTranscription(context.Background(), "Hallo Welt", "de", "huggingface", "openai/whisper-large-v3", 2400, 450, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.SaveTranscription(context.Background(), "Hello World", "en", "local", "ggml-small.bin", 1800, 120, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	count, _ := s.TranscriptionCount(context.Background())
	if count != 2 {
		t.Errorf("expected 2 records, got %d", count)
	}

	recent, err := s.ListTranscriptions(context.Background(), ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent, got %d", len(recent))
	}
	// Most recent first
	if recent[0].Text != "Hello World" {
		t.Errorf("expected most recent = 'Hello World', got %q", recent[0].Text)
	}
	if recent[0].Provider != "local" {
		t.Errorf("expected provider = 'local', got %q", recent[0].Provider)
	}
	if recent[0].Model != "ggml-small.bin" {
		t.Errorf("expected model = %q, got %q", "ggml-small.bin", recent[0].Model)
	}
	if recent[0].LatencyMs != 120 {
		t.Errorf("expected latency = 120, got %d", recent[0].LatencyMs)
	}
}

func TestSQLiteSaveMaintainsWordCounts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	if err := s.SaveTranscription(context.Background(), "one two three", "en", "local", "m", 1000, 100, nil); err != nil {
		t.Fatalf("SaveTranscription: %v", err)
	}
	noteID, err := s.SaveQuickNote(context.Background(), "four five", "en", "manual", 500, 50, nil)
	if err != nil {
		t.Fatalf("SaveQuickNote: %v", err)
	}
	if err := s.UpdateQuickNote(context.Background(), noteID, "four five six seven"); err != nil {
		t.Fatalf("UpdateQuickNote: %v", err)
	}

	var transcriptionWords, noteWords int
	if err := s.db.QueryRow(`SELECT word_count FROM transcriptions LIMIT 1`).Scan(&transcriptionWords); err != nil {
		t.Fatalf("select transcription word_count: %v", err)
	}
	if err := s.db.QueryRow(`SELECT word_count FROM quick_notes WHERE id = ?`, noteID).Scan(&noteWords); err != nil {
		t.Fatalf("select quick note word_count: %v", err)
	}
	if transcriptionWords != 3 {
		t.Fatalf("transcription word_count = %d, want 3", transcriptionWords)
	}
	if noteWords != 4 {
		t.Fatalf("quick note word_count = %d, want 4", noteWords)
	}

	stats, err := s.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalWords != 7 {
		t.Fatalf("Stats.TotalWords = %d, want 7", stats.TotalWords)
	}
}

func TestSaveAndRecentFallsBackToConfiguredModelHints(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{
		Backend:                 "sqlite",
		SQLitePath:              dbPath,
		MaxAudioStorageMB:       100,
		TranscriptionModelHints: map[string]string{"huggingface": "openai/whisper-large-v3", "hf": "openai/whisper-large-v3", "local": "ggml-small.bin"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if err := s.SaveTranscription(context.Background(), "Hallo Welt", "de", "huggingface", "", 2400, 450, nil); err != nil {
		t.Fatalf("Save huggingface: %v", err)
	}
	if err := s.SaveTranscription(context.Background(), "Hello World", "en", "local", "", 1800, 120, nil); err != nil {
		t.Fatalf("Save local: %v", err)
	}

	recent, err := s.ListTranscriptions(context.Background(), ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent, got %d", len(recent))
	}
	if recent[0].Model != "ggml-small.bin" {
		t.Fatalf("local model = %q, want %q", recent[0].Model, "ggml-small.bin")
	}
	if recent[1].Model != "openai/whisper-large-v3" {
		t.Fatalf("hf model = %q, want %q", recent[1].Model, "openai/whisper-large-v3")
	}
}

func TestSQLiteListOptsLanguageAndAfterFilter(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.SaveTranscription(ctx, "old german", "de", "local", "m", 1000, 100, nil); err != nil {
		t.Fatalf("SaveTranscription old german: %v", err)
	}
	first, err := s.ListTranscriptions(ctx, ListOpts{Limit: 1})
	if err != nil {
		t.Fatalf("ListTranscriptions first: %v", err)
	}
	after := first[0].CreatedAt
	// Audit 4.4: wait for the next SQLite second boundary instead of
	// a flat 1.1 s sleep. Saves up to ~1 s per call on a warm clock.
	waitForNextSecond()
	if err := s.SaveTranscription(ctx, "new german", "de-DE", "local", "m", 1000, 100, nil); err != nil {
		t.Fatalf("SaveTranscription new german: %v", err)
	}
	if err := s.SaveTranscription(ctx, "new english", "en", "local", "m", 1000, 100, nil); err != nil {
		t.Fatalf("SaveTranscription new english: %v", err)
	}

	transcriptions, err := s.ListTranscriptions(ctx, ListOpts{Limit: 10, Language: "de", After: after})
	if err != nil {
		t.Fatalf("ListTranscriptions filtered: %v", err)
	}
	if len(transcriptions) != 1 || transcriptions[0].Text != "new german" {
		t.Fatalf("filtered transcriptions = %+v, want only new german", transcriptions)
	}

	if _, err := s.SaveQuickNote(ctx, "old note", "de", "manual", 1000, 100, nil); err != nil {
		t.Fatalf("SaveQuickNote old: %v", err)
	}
	notes, err := s.ListQuickNotes(ctx, ListOpts{Limit: 1})
	if err != nil {
		t.Fatalf("ListQuickNotes first: %v", err)
	}
	noteAfter := notes[0].CreatedAt
	// Audit 4.4: wait for the next SQLite second boundary instead of
	// a flat 1.1 s sleep. Saves up to ~1 s per call on a warm clock.
	waitForNextSecond()
	if _, err := s.SaveQuickNote(ctx, "new note", "de_DE", "manual", 1000, 100, nil); err != nil {
		t.Fatalf("SaveQuickNote new de: %v", err)
	}
	if _, err := s.SaveQuickNote(ctx, "english note", "en", "manual", 1000, 100, nil); err != nil {
		t.Fatalf("SaveQuickNote new en: %v", err)
	}
	notes, err = s.ListQuickNotes(ctx, ListOpts{Limit: 10, Language: "de", After: noteAfter})
	if err != nil {
		t.Fatalf("ListQuickNotes filtered: %v", err)
	}
	if len(notes) != 1 || notes[0].Text != "new note" {
		t.Fatalf("filtered notes = %+v, want only new note", notes)
	}
}

func TestSaveWithAudio(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, SaveAudio: true, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	fakeWAV := make([]byte, 1024)
	if err := s.SaveTranscription(context.Background(), "Test", "de", "hf", "openai/whisper-large-v3", 2000, 500, fakeWAV); err != nil {
		t.Fatalf("Save with audio: %v", err)
	}

	recent, _ := s.ListTranscriptions(context.Background(), ListOpts{Limit: 1})
	if len(recent) != 1 {
		t.Fatal("expected 1 record")
	}
	if recent[0].AudioPath == "" {
		t.Error("expected audio path to be set")
	}
	if recent[0].Audio == nil {
		t.Fatal("expected audio metadata to be populated")
	}
	if recent[0].Audio.StorageKind != AudioStorageLocalFile {
		t.Fatalf("audio storage kind = %q, want %q", recent[0].Audio.StorageKind, AudioStorageLocalFile)
	}
	if recent[0].Audio.DurationMs != 2000 {
		t.Fatalf("audio duration = %d, want %d", recent[0].Audio.DurationMs, 2000)
	}
	if recent[0].Audio.SizeBytes != int64(len(fakeWAV)) {
		t.Fatalf("audio size = %d, want %d", recent[0].Audio.SizeBytes, len(fakeWAV))
	}

	sqliteStore, ok := s.(*SQLiteStore)
	if !ok {
		t.Fatalf("store type = %T, want *SQLiteStore", s)
	}
	var assetCount int
	if err := sqliteStore.db.QueryRow(`SELECT COUNT(*) FROM audio_assets WHERE owner_kind = ? AND owner_id = ? AND path = ?`, "transcription", recent[0].ID, recent[0].AudioPath).Scan(&assetCount); err != nil {
		t.Fatalf("query audio_assets: %v", err)
	}
	if assetCount != 1 {
		t.Fatalf("audio asset rows = %d, want 1", assetCount)
	}
}

func TestRecentLimitReturnsAtMostN(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	for i := 0; i < 5; i++ {
		if err := s.SaveTranscription(context.Background(), fmt.Sprintf("record-%d", i), "de", "local", "", 1000, 100, nil); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}

	recent, err := s.ListTranscriptions(context.Background(), ListOpts{Limit: 3})
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recent))
	}

	// The 3 most recent should be records 4, 3, 2 (descending).
	want := []string{"record-4", "record-3", "record-2"}
	for i, w := range want {
		if recent[i].Text != w {
			t.Errorf("recent[%d]: expected %q, got %q", i, w, recent[i].Text)
		}
	}
}

func TestRecentOrderDescending(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	for _, text := range []string{"A", "B", "C"} {
		if err := s.SaveTranscription(context.Background(), text, "de", "local", "", 1000, 50, nil); err != nil {
			t.Fatalf("Save %q: %v", text, err)
		}
		// Tiny sleep so created_at timestamps differ (SQLite CURRENT_TIMESTAMP
		// has second resolution).
		time.Sleep(10 * time.Millisecond)
	}

	recent, err := s.ListTranscriptions(context.Background(), ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recent))
	}

	want := []string{"C", "B", "A"}
	for i, w := range want {
		if recent[i].Text != w {
			t.Errorf("recent[%d]: expected %q, got %q", i, w, recent[i].Text)
		}
	}
}

func TestSaveWithAudioDisabled(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	fakeWAV := make([]byte, 1024)
	if err := s.SaveTranscription(context.Background(), "no audio", "de", "hf", "", 1500, 200, fakeWAV); err != nil {
		t.Fatalf("Save: %v", err)
	}

	recent, err := s.ListTranscriptions(context.Background(), ListOpts{Limit: 1})
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 1 {
		t.Fatal("expected 1 record")
	}
	if recent[0].AudioPath != "" {
		t.Errorf("expected empty audio path when saveAudio=false, got %q", recent[0].AudioPath)
	}
}

func TestCountAfterMultipleSaves(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	for i := 0; i < 10; i++ {
		if err := s.SaveTranscription(context.Background(), fmt.Sprintf("entry-%d", i), "en", "local", "", 1200, 80, nil); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}

	count, err := s.TranscriptionCount(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 10 {
		t.Errorf("expected 10, got %d", count)
	}
}

func TestRecentEmptyStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	recent, err := s.ListTranscriptions(context.Background(), ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 0 {
		t.Errorf("expected 0 records, got %d", len(recent))
	}
}
