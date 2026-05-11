package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/store"
)

func TestResolveDashboardAudioUsesAudioAssetWhenLegacyPathEmpty(t *testing.T) {
	feedbackStore, err := store.NewSQLiteStore(store.StoreConfig{
		SQLitePath: filepath.Join(t.TempDir(), "feedback.db"),
		SaveAudio:  false,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer feedbackStore.Close()

	audioPath := filepath.Join(t.TempDir(), "asset.wav")
	if err := os.WriteFile(audioPath, []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatalf("write audio fixture: %v", err)
	}

	ctx := context.Background()
	result, err := feedbackStore.DB().ExecContext(ctx, `INSERT INTO transcriptions
		(text, language, provider, model, duration_ms, latency_ms, audio_path, word_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"asset-backed audio", "de", "local", "model", 1200, 100, "", 2,
	)
	if err != nil {
		t.Fatalf("insert transcription: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	if _, err := feedbackStore.DB().ExecContext(ctx, `INSERT INTO audio_assets
		(owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"transcription", id, string(store.AudioStorageLocalFile), audioPath, "audio/wav", int64(3), int64(1200),
	); err != nil {
		t.Fatalf("insert audio asset: %v", err)
	}

	path, filename, err := resolveDashboardAudio(ctx, feedbackStore, "transcription", strconv.FormatInt(id, 10))
	if err != nil {
		t.Fatalf("resolveDashboardAudio: %v", err)
	}
	if path != audioPath {
		t.Fatalf("resolved path = %q, want %q", path, audioPath)
	}
	if filename != "transcription-1.wav" {
		t.Fatalf("filename = %q, want transcription-1.wav", filename)
	}
}
