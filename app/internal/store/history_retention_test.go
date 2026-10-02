package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openRetentionTestStore(t *testing.T, cfg StoreConfig) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(cfg)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func savedAudioPaths(t *testing.T, s *SQLiteStore) []string {
	t.Helper()
	paths, err := queryStrings(context.Background(), s.db, `SELECT path FROM audio_assets WHERE path <> ''`)
	if err != nil {
		t.Fatalf("list audio paths: %v", err)
	}
	return paths
}

// Turning save_audio off stops new recordings; it must not exempt the ones
// already on disk from audio retention.
func TestAudioRetentionAppliesWhenSavingIsOff(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedback.db")
	writer := openRetentionTestStore(t, StoreConfig{SQLitePath: dbPath, SaveAudio: true})
	if err := writer.SaveTranscription(context.Background(), "old clip", "en", "local", "", 500, 10, []byte("RIFFold")); err != nil {
		t.Fatalf("SaveTranscription: %v", err)
	}
	paths := savedAudioPaths(t, writer)
	if len(paths) == 0 {
		t.Fatal("no audio was saved")
	}
	if _, err := writer.db.Exec(`UPDATE audio_assets SET created_at = '2000-01-01 00:00:00'`); err != nil {
		t.Fatalf("backdate audio: %v", err)
	}
	_ = writer.Close()

	openRetentionTestStore(t, StoreConfig{SQLitePath: dbPath, SaveAudio: false, AudioRetentionDays: 7})

	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatalf("audio past retention survived with save_audio off (stat err: %v)", err)
	}
}

// An audio file no row references is deleted once it is old enough; a
// referenced file, a fresh one (its transaction may still be running) and a
// file the store did not name are kept.
func TestOrphanedAudioSweep(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "feedback.db")
	writer := openRetentionTestStore(t, StoreConfig{SQLitePath: dbPath, SaveAudio: true})
	if err := writer.SaveTranscription(context.Background(), "kept", "en", "local", "", 500, 10, []byte("RIFFkept")); err != nil {
		t.Fatalf("SaveTranscription: %v", err)
	}
	referenced := savedAudioPaths(t, writer)[0]
	_ = writer.Close()

	audioDir := filepath.Join(dir, "audio")
	old := time.Now().Add(-2 * time.Hour)
	write := func(name string, mtime time.Time) string {
		path := filepath.Join(audioDir, name)
		if err := os.WriteFile(path, []byte("RIFF"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if err := os.Chtimes(referenced, old, old); err != nil {
		t.Fatal(err)
	}
	orphan := write("1700000000000000000.wav", old)
	orphanNote := write("qn_1700000000000000001.wav", old)
	fresh := write("1700000000000000002.wav", time.Now())
	foreign := write("interview.wav", old)

	// With a Postgres DSN configured the folder may hold that database's
	// audio, which no SQLite row references: nothing is swept.
	shared := openRetentionTestStore(t, StoreConfig{SQLitePath: dbPath, SaveAudio: true, AudioRetentionDays: 30, PostgresDSN: "postgres://sk@127.0.0.1/sk"})
	_ = shared.Close()
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("audio another backend may reference was swept: %v", err)
	}

	openRetentionTestStore(t, StoreConfig{SQLitePath: dbPath, SaveAudio: true, AudioRetentionDays: 30})

	for _, gone := range []string{orphan, orphanNote} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("orphaned %s survived the sweep (stat err: %v)", filepath.Base(gone), err)
		}
	}
	for _, kept := range []string{referenced, fresh, foreign} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s was deleted by the orphan sweep: %v", filepath.Base(kept), err)
		}
	}
}

// Transcript retention is opt-in: without it nothing is deleted; with it,
// history older than the window goes and newer history stays.
func TestTranscriptRetentionOnlyWhenConfigured(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedback.db")
	ctx := context.Background()
	writer := openRetentionTestStore(t, StoreConfig{SQLitePath: dbPath})
	for _, text := range []string{"old dictation", "new dictation"} {
		if err := writer.SaveTranscription(ctx, text, "en", "local", "", 500, 10, nil); err != nil {
			t.Fatalf("SaveTranscription: %v", err)
		}
	}
	conversationID, err := writer.SaveVoiceAgentSession(ctx, VoiceAgentSession{Language: "en", Transcript: "old conversation"})
	if err != nil {
		t.Fatalf("SaveVoiceAgentSession: %v", err)
	}
	if _, err := writer.db.Exec(`UPDATE transcriptions SET created_at = '2000-01-01 00:00:00' WHERE text = 'old dictation'`); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.db.Exec(`UPDATE voice_agent_sessions SET created_at = '2000-01-01 00:00:00'`); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()

	texts := func(s *SQLiteStore) map[string]bool {
		rows, err := s.ListTranscriptions(ctx, ListOpts{Limit: 100})
		if err != nil {
			t.Fatalf("ListTranscriptions: %v", err)
		}
		out := map[string]bool{}
		for _, row := range rows {
			out[row.Text] = true
		}
		if _, err := s.GetVoiceAgentSession(ctx, conversationID); err == nil {
			out["old conversation"] = true
		}
		return out
	}

	unconfigured := openRetentionTestStore(t, StoreConfig{SQLitePath: dbPath, MeetingRetentionDays: 30})
	if got := texts(unconfigured); !got["old dictation"] || !got["old conversation"] {
		t.Fatalf("history was deleted without transcript retention: %v", got)
	}
	_ = unconfigured.Close()

	retained := openRetentionTestStore(t, StoreConfig{SQLitePath: dbPath, TranscriptRetentionDays: 30})
	got := texts(retained)
	if got["old dictation"] || got["old conversation"] {
		t.Errorf("history past the retention window survived: %v", got)
	}
	if !got["new dictation"] {
		t.Errorf("history inside the retention window was deleted: %v", got)
	}
}
