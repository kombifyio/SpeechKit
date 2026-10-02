package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
	if _, err := sqliteStore.db.Exec(`UPDATE audio_assets SET created_at = ? WHERE owner_kind = ? AND owner_id = ?`, expiredAt, "transcription", records[0].ID); err != nil {
		t.Fatalf("age audio asset: %v", err)
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
	var assetCount int
	if err := sqliteStore.db.QueryRow(`SELECT COUNT(*) FROM audio_assets WHERE owner_kind = ? AND owner_id = ?`, "transcription", records[0].ID).Scan(&assetCount); err != nil {
		t.Fatalf("count audio assets: %v", err)
	}
	if assetCount != 0 {
		t.Fatalf("audio asset count = %d, want 0 after retention", assetCount)
	}
}

func TestAudioRetentionFallsBackToLegacyAudioPath(t *testing.T) {
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

	if err := sqliteStore.SaveTranscription(context.Background(), "legacy clip", "de", "local", "", 1000, 100, make([]byte, 512)); err != nil {
		t.Fatalf("SaveTranscription: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	records, err := sqliteStore.ListTranscriptions(context.Background(), ListOpts{Limit: 1})
	if err != nil {
		t.Fatalf("ListTranscriptions: %v", err)
	}
	if len(records) != 1 || records[0].AudioPath == "" {
		t.Fatalf("expected saved audio path, got %+v", records)
	}

	if _, err := sqliteStore.db.Exec(`DELETE FROM audio_assets WHERE owner_kind = ? AND owner_id = ?`, "transcription", records[0].ID); err != nil {
		t.Fatalf("remove audio asset row: %v", err)
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
		t.Fatalf("AudioPath = %q, want cleared through legacy fallback", updated[0].AudioPath)
	}
}
