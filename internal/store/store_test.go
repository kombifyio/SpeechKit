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

func TestNewAndMigrate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	count, err := s.TranscriptionCount(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 records, got %d", count)
	}
}

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

func TestEnforceStorageLimit(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	// saveAudio=true with a 1 MB limit.
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, SaveAudio: true, MaxAudioStorageMB: 1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	fakeWAV := make([]byte, 1024)
	for i := 0; i < 5; i++ {
		if err := s.SaveTranscription(context.Background(), fmt.Sprintf("clip-%d", i), "de", "local", "", 800, 100, fakeWAV); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}

	// Enforcement runs in a goroutine; give it a moment.
	time.Sleep(100 * time.Millisecond)

	count, err := s.TranscriptionCount(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 records regardless of cleanup, got %d", count)
	}

	// Verify the audio directory exists. Total is 5 KB which is well under
	// 1 MB, so no cleanup should have occurred. However enforceStorageLimit
	// runs async and may still be in-flight, so just verify records persisted.
	audioDir := filepath.Join(dir, "audio")
	entries, err := os.ReadDir(audioDir)
	if err != nil {
		t.Fatalf("read audio dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least some audio files to remain")
	}
}

func TestEnforceStorageLimitCleansQuickNoteAudio(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	sqliteStore, err := NewSQLiteStore(StoreConfig{
		Backend:           "sqlite",
		SQLitePath:        dbPath,
		SaveAudio:         true,
		MaxAudioStorageMB: 1,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer sqliteStore.Close()

	largeWAV := make([]byte, 700*1024)
	if _, err := sqliteStore.SaveQuickNote(context.Background(), "note-1", "de", "manual", 0, 0, largeWAV); err != nil {
		t.Fatalf("SaveQuickNote #1: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := sqliteStore.SaveQuickNote(context.Background(), "note-2", "de", "manual", 0, 0, largeWAV); err != nil {
		t.Fatalf("SaveQuickNote #2: %v", err)
	}

	sqliteStore.enforceStorageLimit()

	notes, err := sqliteStore.ListQuickNotes(context.Background(), ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListQuickNotes: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}

	cleared := 0
	for _, note := range notes {
		if note.AudioPath == "" {
			cleared++
		}
	}
	if cleared == 0 {
		t.Fatal("expected storage cleanup to clear at least one quick note audio path")
	}
}

func TestCloseIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// First close should succeed.
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Second close must not panic. An error is acceptable (sql: database is
	// closed) but a panic is not.
	_ = s.Close()
}

func TestNewWithEmptyPath(t *testing.T) {
	// Point APPDATA to a temp dir so the default path lands somewhere safe.
	tmpDir := t.TempDir()
	original := os.Getenv("APPDATA")
	t.Setenv("APPDATA", tmpDir)
	defer os.Setenv("APPDATA", original)

	s, err := New(StoreConfig{Backend: "sqlite", MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New with empty path: %v", err)
	}
	defer s.Close()

	// Verify the database was created under the temp APPDATA.
	expectedDir := filepath.Join(tmpDir, "SpeechKit")
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("expected directory %s to exist", expectedDir)
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

func TestStatsIncludesAverageWordsPerMinute(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, SaveAudio: true, AudioRetentionDays: 7, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if err := s.SaveTranscription(context.Background(), "one two three four", "en", "local", "", 2000, 180, nil); err != nil {
		t.Fatalf("SaveTranscription #1: %v", err)
	}
	if err := s.SaveTranscription(context.Background(), "five six seven eight", "en", "huggingface", "", 2000, 220, nil); err != nil {
		t.Fatalf("SaveTranscription #2: %v", err)
	}
	if _, err := s.SaveQuickNote(context.Background(), "quick capture text", "en", "capture", 3000, 160, nil); err != nil {
		t.Fatalf("SaveQuickNote: %v", err)
	}

	stats, err := s.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if stats.Transcriptions != 2 {
		t.Fatalf("stats.Transcriptions = %d, want 2", stats.Transcriptions)
	}
	if stats.QuickNotes != 1 {
		t.Fatalf("stats.QuickNotes = %d, want 1", stats.QuickNotes)
	}
	if stats.TotalWords != 11 {
		t.Fatalf("stats.TotalWords = %d, want 11", stats.TotalWords)
	}
	if stats.TotalAudioDurationMs != 7000 {
		t.Fatalf("stats.TotalAudioDurationMs = %d, want 7000", stats.TotalAudioDurationMs)
	}
	if stats.AverageWordsPerMinute < 90 || stats.AverageWordsPerMinute > 100 {
		t.Fatalf("stats.AverageWordsPerMinute = %.2f, want approx 94.29", stats.AverageWordsPerMinute)
	}
}

func TestAudioRetentionRemovesExpiredAudio(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	sqliteStore, err := NewSQLiteStore(StoreConfig{
		Backend:            "sqlite",
		SQLitePath:         dbPath,
		SaveAudio:          true,
		AudioRetentionDays: 7,
		MaxAudioStorageMB:  0,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer sqliteStore.Close()

	audio := make([]byte, 1024)
	if err := sqliteStore.SaveTranscription(context.Background(), "expired clip", "de", "local", "", 1000, 100, audio); err != nil {
		t.Fatalf("SaveTranscription: %v", err)
	}

	// Allow background goroutines triggered by SaveTranscription to finish
	// before we call enforceAudioRetention directly, avoiding SQLITE_BUSY.
	time.Sleep(300 * time.Millisecond)

	records, err := sqliteStore.ListTranscriptions(context.Background(), ListOpts{Limit: 1})
	if err != nil {
		t.Fatalf("ListTranscriptions: %v", err)
	}
	if len(records) != 1 || records[0].AudioPath == "" {
		t.Fatalf("expected saved audio path, got %+v", records)
	}

	expiredAt := time.Now().Add(-8 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err := sqliteStore.db.Exec(`UPDATE transcriptions SET created_at = ? WHERE id = ?`, expiredAt, records[0].ID); err != nil {
		t.Fatalf("age transcription: %v", err)
	}

	sqliteStore.enforceAudioRetention()

	updated, err := sqliteStore.ListTranscriptions(context.Background(), ListOpts{Limit: 1})
	if err != nil {
		t.Fatalf("ListTranscriptions after retention: %v", err)
	}
	if updated[0].AudioPath != "" {
		t.Fatalf("AudioPath = %q, want cleared after retention", updated[0].AudioPath)
	}
	if updated[0].Audio != nil {
		t.Fatalf("Audio metadata = %+v, want nil after retention", updated[0].Audio)
	}
}

func TestSQLiteStoreProvidesSemanticCapabilities(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	provider, ok := s.(SemanticCapabilityProvider)
	if !ok {
		t.Fatal("sqlite store should expose semantic capabilities")
	}

	caps := provider.SemanticCapabilities(context.Background())
	if caps.Embeddings {
		t.Fatal("sqlite local mode should not advertise embeddings by default")
	}
	if caps.VectorSearch {
		t.Fatal("sqlite local mode should not advertise vector search by default")
	}
	if caps.Provider != SemanticProviderNone {
		t.Fatalf("semantic provider = %q, want %q", caps.Provider, SemanticProviderNone)
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

func TestFactory_SQLiteDefault(t *testing.T) {
	tmpDir := t.TempDir()
	original := os.Getenv("APPDATA")
	t.Setenv("APPDATA", tmpDir)
	defer os.Setenv("APPDATA", original)

	s, err := New(StoreConfig{})
	if err != nil {
		t.Fatalf("New with empty config: %v", err)
	}
	defer s.Close()

	// Should default to sqlite and succeed.
	count, err := s.TranscriptionCount(context.Background())
	if err != nil {
		t.Fatalf("TranscriptionCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestFactory_ExplicitSQLite(t *testing.T) {
	tmpPath := filepath.Join(t.TempDir(), "explicit.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: tmpPath})
	if err != nil {
		t.Fatalf("New with explicit sqlite: %v", err)
	}
	defer s.Close()

	if _, statErr := os.Stat(tmpPath); os.IsNotExist(statErr) {
		t.Fatalf("expected database file at %s", tmpPath)
	}
}

func TestFactory_PostgresRequiresDSN(t *testing.T) {
	_, err := New(StoreConfig{Backend: "postgres"})
	if err == nil {
		t.Fatal("expected error for postgres backend without DSN")
	}
	if !strings.Contains(err.Error(), "requires a DSN") {
		t.Fatalf("error = %q, want message about missing DSN", err.Error())
	}
}

func TestFactory_PostgresAttemptsRealConnection(t *testing.T) {
	_, err := New(StoreConfig{
		Backend:     "postgres",
		PostgresDSN: "postgres://speechkit:secret@127.0.0.1:1/speechkit?sslmode=disable&connect_timeout=1",
	})
	if err == nil {
		t.Fatal("expected connection error for unreachable postgres endpoint")
	}
	if strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("error = %q, want real connection failure instead of stub message", err.Error())
	}
}

func TestFactory_UnknownBackend(t *testing.T) {
	_, err := New(StoreConfig{Backend: "foobar"})
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error = %q, want message containing 'unknown'", err.Error())
	}
}

// mockStore is a minimal Store implementation for testing RegisterBackend.
type mockStore struct{}

func (m *mockStore) SaveTranscription(_ context.Context, _, _, _, _ string, _, _ int64, _ []byte) error {
	return nil
}
func (m *mockStore) GetTranscription(_ context.Context, _ int64) (*Transcription, error) {
	return nil, nil
}
func (m *mockStore) ListTranscriptions(_ context.Context, _ ListOpts) ([]Transcription, error) {
	return nil, nil
}
func (m *mockStore) TranscriptionCount(_ context.Context) (int, error) { return 0, nil }
func (m *mockStore) SaveQuickNote(_ context.Context, _, _, _ string, _, _ int64, _ []byte) (int64, error) {
	return 0, nil
}
func (m *mockStore) GetQuickNote(_ context.Context, _ int64) (*QuickNote, error) {
	return nil, nil
}
func (m *mockStore) ListQuickNotes(_ context.Context, _ ListOpts) ([]QuickNote, error) {
	return nil, nil
}
func (m *mockStore) UpdateQuickNote(_ context.Context, _ int64, _ string) error { return nil }
func (m *mockStore) UpdateQuickNoteCapture(_ context.Context, _ int64, _, _ string, _, _ int64, _ []byte) error {
	return nil
}
func (m *mockStore) PinQuickNote(_ context.Context, _ int64, _ bool) error { return nil }
func (m *mockStore) DeleteQuickNote(_ context.Context, _ int64) error      { return nil }
func (m *mockStore) QuickNoteCount(_ context.Context) (int, error)         { return 0, nil }
func (m *mockStore) Stats(_ context.Context) (Stats, error)                { return Stats{}, nil }
func (m *mockStore) Close() error                                          { return nil }

func TestRegisterBackend(t *testing.T) {
	RegisterBackend("test", func(cfg StoreConfig) (Store, error) {
		return &mockStore{}, nil
	})
	defer delete(registeredBackends, "test")

	s, err := New(StoreConfig{Backend: "test"})
	if err != nil {
		t.Fatalf("New with registered backend: %v", err)
	}
	defer s.Close()

	if _, ok := s.(*mockStore); !ok {
		t.Fatalf("expected *mockStore, got %T", s)
	}
}

func TestStoreInterface_CompileCheck(t *testing.T) {
	// Compile-time check: SQLiteStore must implement Store.
	var _ Store = (*SQLiteStore)(nil)
}

func TestPostgresStoreParity(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("SPEECHKIT_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set SPEECHKIT_POSTGRES_TEST_DSN to run postgres parity tests")
	}

	t.Setenv("APPDATA", t.TempDir())
	s, err := New(StoreConfig{
		Backend:           "postgres",
		PostgresDSN:       dsn,
		SaveAudio:         true,
		MaxAudioStorageMB: 100,
	})
	if err != nil {
		t.Fatalf("New postgres store: %v", err)
	}
	defer s.Close()

	pg, ok := s.(*PostgresStore)
	if !ok {
		t.Fatalf("store type = %T, want *PostgresStore", s)
	}
	if _, err := pg.db.Exec(`TRUNCATE TABLE quick_notes, transcriptions RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate tables: %v", err)
	}

	audio := []byte("fake wav payload")
	if err := s.SaveTranscription(context.Background(), "Hallo Postgres", "de", "hf", "openai/whisper-large-v3", 2100, 300, audio); err != nil {
		t.Fatalf("SaveTranscription: %v", err)
	}
	noteID, err := s.SaveQuickNote(context.Background(), "postgres note", "de", "manual", 900, 120, audio)
	if err != nil {
		t.Fatalf("SaveQuickNote: %v", err)
	}

	count, err := s.TranscriptionCount(context.Background())
	if err != nil {
		t.Fatalf("TranscriptionCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("TranscriptionCount = %d, want 1", count)
	}

	transcriptions, err := s.ListTranscriptions(context.Background(), ListOpts{Limit: 5})
	if err != nil {
		t.Fatalf("ListTranscriptions: %v", err)
	}
	if len(transcriptions) != 1 {
		t.Fatalf("len(ListTranscriptions) = %d, want 1", len(transcriptions))
	}
	if transcriptions[0].Audio == nil || transcriptions[0].Audio.StorageKind != AudioStorageLocalFile {
		t.Fatalf("transcription audio = %+v", transcriptions[0].Audio)
	}

	note, err := s.GetQuickNote(context.Background(), noteID)
	if err != nil {
		t.Fatalf("GetQuickNote: %v", err)
	}
	if note.Audio == nil || note.Audio.StorageKind != AudioStorageLocalFile {
		t.Fatalf("quick note audio = %+v", note.Audio)
	}

	if err := s.PinQuickNote(context.Background(), noteID, true); err != nil {
		t.Fatalf("PinQuickNote: %v", err)
	}
	if err := s.UpdateQuickNote(context.Background(), noteID, "postgres note updated"); err != nil {
		t.Fatalf("UpdateQuickNote: %v", err)
	}

	stats, err := s.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Transcriptions != 1 || stats.QuickNotes != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}
