package store

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/testutil"
)

func TestPostgresStoreParity(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("SPEECHKIT_POSTGRES_TEST_DSN"))
	if dsn == "" {
		testutil.SkipOrFailExplicitMissingConfig(t, "SPEECHKIT_POSTGRES_TEST_DSN", "Set it to run postgres parity tests.")
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
	// CASCADE is required because audio_assets link tables FK-reference
	// transcriptions and quick_notes; a plain TRUNCATE is rejected with
	// SQLSTATE 0A000 once those links exist.
	if _, err := pg.db.Exec(`TRUNCATE TABLE quick_notes, transcriptions RESTART IDENTITY CASCADE`); err != nil {
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
