package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAudioAssetHelpersRecordAndDelete(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	audioPath := filepath.Join(t.TempDir(), "clip.wav")
	audioData := []byte{0, 1, 2, 3, 4}
	if err := os.WriteFile(audioPath, audioData, 0o600); err != nil {
		t.Fatalf("write audio fixture: %v", err)
	}

	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO transcriptions (id, scope_id, text, language, provider, model, duration_ms, latency_ms, word_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, 42, 1, "audio owner", "en", "local", "", 1234, 10, 2); err != nil {
		t.Fatalf("insert audio owner: %v", err)
	}
	if err := recordAudioAsset(ctx, s.db, "sqlite", "transcription", 42, audioPath, 1234); err != nil {
		t.Fatalf("recordAudioAsset: %v", err)
	}
	var sizeBytes, durationMs int64
	if err := s.db.QueryRow(`SELECT size_bytes, duration_ms FROM audio_assets WHERE owner_kind = ? AND owner_id = ?`,
		"transcription", 42,
	).Scan(&sizeBytes, &durationMs); err != nil {
		t.Fatalf("select audio asset: %v", err)
	}
	if sizeBytes != int64(len(audioData)) {
		t.Fatalf("size_bytes = %d, want %d", sizeBytes, len(audioData))
	}
	if durationMs != 1234 {
		t.Fatalf("duration_ms = %d, want 1234", durationMs)
	}

	if err := deleteAudioAsset(ctx, s.db, "sqlite", "transcription", 42, audioPath); err != nil {
		t.Fatalf("deleteAudioAsset: %v", err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audio_assets WHERE owner_kind = ? AND owner_id = ?`,
		"transcription", 42,
	).Scan(&count); err != nil {
		t.Fatalf("count audio assets: %v", err)
	}
	if count != 0 {
		t.Fatalf("audio asset rows = %d, want 0", count)
	}
}
