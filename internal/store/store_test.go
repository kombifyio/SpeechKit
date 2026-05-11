package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/testutil"
)

func sqliteTestTimestamp(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

func waitForSQLiteIdle(t *testing.T, db *sql.DB) {
	t.Helper()
	testutil.EventuallyNoError(t, 2*time.Second, 10*time.Millisecond, func() error {
		_, err := db.Exec("PRAGMA user_version")
		return err
	})
}

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

func TestSQLiteMigrationRepairsLegacyPersonaDefaultSequence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = db.Exec(`
CREATE TABLE transcriptions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    text TEXT NOT NULL,
    language TEXT NOT NULL DEFAULT 'de',
    provider TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    audio_path TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE quick_notes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    text TEXT NOT NULL,
    language TEXT NOT NULL DEFAULT 'de',
    provider TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    audio_path TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    pinned INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE user_dictionary_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    spoken TEXT NOT NULL,
    canonical TEXT NOT NULL,
    language TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'settings',
    enabled INTEGER NOT NULL DEFAULT 1,
    usage_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(spoken, canonical, language, source)
);
CREATE TABLE voice_agent_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL,
    raw_summary TEXT NOT NULL DEFAULT '',
    transcript TEXT NOT NULL DEFAULT '',
    language TEXT NOT NULL DEFAULT '',
    provider_profile_id TEXT NOT NULL DEFAULT '',
    runtime_kind TEXT NOT NULL DEFAULT '',
    turns_json TEXT NOT NULL DEFAULT '[]',
    ideas_json TEXT NOT NULL DEFAULT '[]',
    decisions_json TEXT NOT NULL DEFAULT '[]',
    open_questions_json TEXT NOT NULL DEFAULT '[]',
    next_steps_json TEXT NOT NULL DEFAULT '[]',
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE voice_agent_personas (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    voice TEXT NOT NULL DEFAULT '',
    locale TEXT NOT NULL DEFAULT '',
    default_role TEXT NOT NULL DEFAULT '',
    tags_json TEXT NOT NULL DEFAULT '[]',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE voice_agent_roles (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    system_prompt TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE voice_agent_sequences (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    steps_json TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`)
	if err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy schema: %v", err)
	}
	_ = db.Close()

	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore should repair legacy schema: %v", err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`INSERT INTO voice_agent_personas (id, display_name, default_sequence) VALUES (?, ?, ?)`, "p", "Persona", "seq"); err != nil {
		t.Fatalf("insert persona with repaired default_sequence: %v", err)
	}

	assertSQLiteColumns(t, s.db, "voice_agent_personas", "default_sequence")
	assertSQLiteColumns(t, s.db, "voice_agent_roles",
		"refinement_prompt",
		"locale",
		"vocabulary_hint",
		"tool_allowlist_json",
		"temperature",
		"thinking_enabled",
		"thinking_level",
		"include_thoughts",
		"thinking_budget",
		"automatic_activity_detection",
		"vad_start_sensitivity",
		"vad_end_sensitivity",
		"vad_prefix_padding_ms",
		"vad_silence_duration_ms",
		"activity_handling",
		"turn_coverage",
		"context_compression_enabled",
		"context_compression_trigger_tk",
		"context_compression_target_tk",
		"enable_affective_dialog",
	)
	assertSQLiteColumns(t, s.db, "voice_agent_sequences",
		"description",
		"completion",
		"max_turns",
	)
}

func assertSQLiteColumns(t *testing.T, db *sql.DB, table string, columns ...string) {
	t.Helper()
	for _, column := range columns {
		exists, err := sqliteColumnExists(context.Background(), db, table, column)
		if err != nil {
			t.Fatalf("check %s.%s: %v", table, column, err)
		}
		if !exists {
			t.Fatalf("expected migration to repair missing column %s.%s", table, column)
		}
	}
}

func TestSQLiteMigrationCreatesLedgerAndExpandTables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	for _, table := range []string{"schema_migrations", "voice_agent_session_turns", "voice_agent_session_summary_items", "audio_assets"} {
		var name string
		err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s missing after migration: %v", table, err)
		}
	}

	var applied int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, "sqlite:008_persona_default_sequence").Scan(&applied); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if applied != 1 {
		t.Fatalf("sqlite 008 migration ledger rows = %d, want 1", applied)
	}
}

func TestSQLiteMigrationBackfillsExpandTables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO voice_agent_sessions
		(title, summary, raw_summary, transcript, language, turns_json, ideas_json, decisions_json, open_questions_json, next_steps_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Session", "Summary", "Raw", "User: hello", "de",
		`[{"role":"user","text":"hello"}]`,
		`["Idea"]`, `["Decision"]`, `["Question"]`, `["Step"]`,
	); err != nil {
		t.Fatalf("insert legacy voice session: %v", err)
	}
	if err := backfillSQLiteVoiceAgentNormalized(ctx, s.db); err != nil {
		t.Fatalf("backfill voice agent normalized: %v", err)
	}
	if err := backfillSQLiteVoiceAgentNormalized(ctx, s.db); err != nil {
		t.Fatalf("backfill voice agent normalized idempotent: %v", err)
	}

	var turnCount, itemCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM voice_agent_session_turns`).Scan(&turnCount); err != nil {
		t.Fatalf("count voice turns: %v", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM voice_agent_session_summary_items`).Scan(&itemCount); err != nil {
		t.Fatalf("count summary items: %v", err)
	}
	if turnCount != 1 {
		t.Fatalf("voice turn rows = %d, want 1", turnCount)
	}
	if itemCount != 4 {
		t.Fatalf("summary item rows = %d, want 4", itemCount)
	}

	if _, err := s.db.ExecContext(ctx, `INSERT INTO transcriptions (text, language, provider, model, duration_ms, latency_ms, audio_path, word_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"audio row", "de", "local", "m", 1200, 100, filepath.Join(t.TempDir(), "clip.wav"), 0,
	); err != nil {
		t.Fatalf("insert legacy audio transcription: %v", err)
	}
	if err := backfillSQLiteAudioAssets(ctx, s.db); err != nil {
		t.Fatalf("backfill audio assets: %v", err)
	}
	var assetCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audio_assets WHERE owner_kind = ?`, "transcription").Scan(&assetCount); err != nil {
		t.Fatalf("count audio assets: %v", err)
	}
	if assetCount != 1 {
		t.Fatalf("audio asset rows = %d, want 1", assetCount)
	}
}

func TestSQLiteMigrationBackfillsWordCounts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO transcriptions (text, language, provider, model, duration_ms, latency_ms, word_count)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "one two three", "en", "local", "m", 0, 0, 0); err != nil {
		t.Fatalf("insert transcription: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO quick_notes (text, language, provider, duration_ms, latency_ms, word_count)
		VALUES (?, ?, ?, ?, ?, ?)`, "four five", "en", "manual", 0, 0, 0); err != nil {
		t.Fatalf("insert quick note: %v", err)
	}
	if err := backfillSQLiteWordCounts(ctx, s.db); err != nil {
		t.Fatalf("backfill word counts: %v", err)
	}

	var transcriptionWords, noteWords int
	if err := s.db.QueryRow(`SELECT word_count FROM transcriptions LIMIT 1`).Scan(&transcriptionWords); err != nil {
		t.Fatalf("select transcription word_count: %v", err)
	}
	if err := s.db.QueryRow(`SELECT word_count FROM quick_notes LIMIT 1`).Scan(&noteWords); err != nil {
		t.Fatalf("select quick note word_count: %v", err)
	}
	if transcriptionWords != 3 {
		t.Fatalf("transcription word_count = %d, want 3", transcriptionWords)
	}
	if noteWords != 2 {
		t.Fatalf("quick note word_count = %d, want 2", noteWords)
	}
	if _, _, err := wordCountBackfillQueries("sqlite", "unknown"); err == nil {
		t.Fatal("wordCountBackfillQueries unknown table error = nil, want error")
	}
}

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
	if err := recordAudioAsset(ctx, s.db, "sqlite", "transcription", 42, audioPath, 1234, "audio/wav"); err != nil {
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

func TestAudioAssetJSONOmitsLocalPath(t *testing.T) {
	raw, err := json.Marshal(AudioAsset{
		StorageKind: AudioStorageLocalFile,
		Path:        filepath.Join("C:", "Users", "operator", "secret.wav"),
		MimeType:    "audio/wav",
		SizeBytes:   12,
		DurationMs:  345,
	})
	if err != nil {
		t.Fatalf("Marshal AudioAsset: %v", err)
	}
	if strings.Contains(string(raw), "path") || strings.Contains(string(raw), "secret.wav") {
		t.Fatalf("AudioAsset JSON leaked local path: %s", raw)
	}
}

func TestNormalizedLanguageFilterHelpers(t *testing.T) {
	clauses, args := appendSQLiteNormalizedLanguageFilter(nil, nil, "de-DE")
	if len(clauses) != 1 || len(args) != 2 {
		t.Fatalf("sqlite filter clauses=%v args=%v, want one clause and two args", clauses, args)
	}
	clauses, args = appendPostgresNormalizedLanguageFilter(nil, nil, "en_US")
	if len(clauses) != 1 || len(args) != 1 {
		t.Fatalf("postgres filter clauses=%v args=%v, want one clause and one arg", clauses, args)
	}
	clauses, args = appendSQLiteNormalizedLanguageFilter(clauses, args, " ")
	if len(clauses) != 1 || len(args) != 1 {
		t.Fatalf("empty filter should not append: clauses=%v args=%v", clauses, args)
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
	after := time.Now().UTC().Add(-30 * time.Minute)
	if _, err := s.db.Exec(`UPDATE transcriptions SET created_at = ? WHERE id = ?`, sqliteTestTimestamp(after.Add(-time.Minute)), first[0].ID); err != nil {
		t.Fatalf("age old transcription: %v", err)
	}
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
	noteAfter := time.Now().UTC().Add(-30 * time.Minute)
	if _, err := s.db.Exec(`UPDATE quick_notes SET created_at = ? WHERE id = ?`, sqliteTestTimestamp(noteAfter.Add(-time.Minute)), notes[0].ID); err != nil {
		t.Fatalf("age old quick note: %v", err)
	}
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

func TestVoiceAgentSessionsSaveAndList(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{
		SQLitePath: filepath.Join(t.TempDir(), "feedback.db"),
		SaveAudio:  false,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	sessionStore, ok := any(s).(VoiceAgentSessionStore)
	if !ok {
		t.Fatal("sqlite store does not implement VoiceAgentSessionStore")
	}

	startedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	endedAt := startedAt.Add(90 * time.Second)
	id, err := sessionStore.SaveVoiceAgentSession(context.Background(), VoiceAgentSession{
		StartedAt:         startedAt,
		EndedAt:           endedAt,
		Language:          "de",
		ProviderProfileID: "realtime.google.gemini-native-audio",
		RuntimeKind:       "native_realtime",
		Transcript:        "User: Idee\nAssistant: Naechster Schritt",
		Turns: []VoiceAgentTurn{
			{Role: "user", Text: "Idee", CreatedAt: startedAt},
			{Role: "assistant", Text: "Naechster Schritt", CreatedAt: endedAt},
		},
		Summary: VoiceAgentSessionSummary{
			Summary:       "Plan fuer den naechsten Schritt.",
			Ideas:         []string{"Produktreife"},
			Decisions:     []string{"Live UX zuerst"},
			OpenQuestions: []string{"Signing"},
			NextSteps:     []string{"Build pruefen"},
		},
	})
	if err != nil {
		t.Fatalf("SaveVoiceAgentSession: %v", err)
	}
	if id == 0 {
		t.Fatal("SaveVoiceAgentSession id = 0")
	}

	sessions, err := sessionStore.ListVoiceAgentSessions(context.Background(), ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListVoiceAgentSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	got := sessions[0]
	if got.Summary.Summary != "Plan fuer den naechsten Schritt." {
		t.Fatalf("summary = %q", got.Summary.Summary)
	}
	if got.Summary.Title == "" {
		t.Fatal("summary title should be derived")
	}
	if len(got.Turns) != 2 || got.Turns[0].Role != "user" {
		t.Fatalf("turns = %#v", got.Turns)
	}
	if len(got.Summary.NextSteps) != 1 || got.Summary.NextSteps[0] != "Build pruefen" {
		t.Fatalf("next steps = %#v", got.Summary.NextSteps)
	}
	filtered, err := sessionStore.ListVoiceAgentSessions(context.Background(), ListOpts{Limit: 10, Language: "de-DE", After: startedAt})
	if err != nil {
		t.Fatalf("ListVoiceAgentSessions filtered: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != id {
		t.Fatalf("filtered voice sessions = %+v, want saved session", filtered)
	}

	var turnCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM voice_agent_session_turns WHERE session_id = ?`, id).Scan(&turnCount); err != nil {
		t.Fatalf("query normalized turns: %v", err)
	}
	if turnCount != 2 {
		t.Fatalf("normalized turn rows = %d, want 2", turnCount)
	}
	var itemCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM voice_agent_session_summary_items WHERE session_id = ?`, id).Scan(&itemCount); err != nil {
		t.Fatalf("query normalized summary items: %v", err)
	}
	if itemCount != 4 {
		t.Fatalf("normalized summary item rows = %d, want 4", itemCount)
	}
}

func TestSQLiteVoiceAgentSessionsHydratesNormalizedChildren(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{
		SQLitePath: filepath.Join(t.TempDir(), "feedback.db"),
		SaveAudio:  false,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	result, err := s.db.ExecContext(ctx, `INSERT INTO voice_agent_sessions
		(title, summary, raw_summary, transcript, language, turns_json, ideas_json, decisions_json, open_questions_json, next_steps_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Normalized", "Parent summary", "Parent raw", "parent transcript", "de",
		`[]`, `[]`, `[]`, `[]`, `[]`,
	)
	if err != nil {
		t.Fatalf("insert parent voice session: %v", err)
	}
	sessionID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO voice_agent_session_turns
		(session_id, turn_index, role, text, created_at)
		VALUES (?, ?, ?, ?, ?), (?, ?, ?, ?, ?)`,
		sessionID, 0, "user", "normalized user", time.Now().UTC(),
		sessionID, 1, "assistant", "normalized assistant", time.Now().UTC(),
	); err != nil {
		t.Fatalf("insert normalized turns: %v", err)
	}
	for i, item := range []struct {
		itemType string
		text     string
	}{
		{"idea", "normalized idea"},
		{"decision", "normalized decision"},
		{"open_question", "normalized question"},
		{"next_step", "normalized step"},
	} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO voice_agent_session_summary_items
			(session_id, item_type, item_index, text)
			VALUES (?, ?, ?, ?)`,
			sessionID, item.itemType, i, item.text,
		); err != nil {
			t.Fatalf("insert normalized item %s: %v", item.itemType, err)
		}
	}

	sessions, err := s.ListVoiceAgentSessions(ctx, ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListVoiceAgentSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	got := sessions[0]
	if len(got.Turns) != 2 || got.Turns[0].Text != "normalized user" || got.Turns[1].Text != "normalized assistant" {
		t.Fatalf("turns = %#v, want normalized child turns", got.Turns)
	}
	if len(got.Summary.Ideas) != 1 || got.Summary.Ideas[0] != "normalized idea" {
		t.Fatalf("ideas = %#v, want normalized child idea", got.Summary.Ideas)
	}
	if len(got.Summary.Decisions) != 1 || got.Summary.Decisions[0] != "normalized decision" {
		t.Fatalf("decisions = %#v, want normalized child decision", got.Summary.Decisions)
	}
	if len(got.Summary.OpenQuestions) != 1 || got.Summary.OpenQuestions[0] != "normalized question" {
		t.Fatalf("open questions = %#v, want normalized child question", got.Summary.OpenQuestions)
	}
	if len(got.Summary.NextSteps) != 1 || got.Summary.NextSteps[0] != "normalized step" {
		t.Fatalf("next steps = %#v, want normalized child step", got.Summary.NextSteps)
	}
}

func TestSQLiteMigrationPreservesNormalizedVoiceChildrenWhenJSONEmpty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedback.db")
	s, err := NewSQLiteStore(StoreConfig{
		SQLitePath: dbPath,
		SaveAudio:  false,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	ctx := context.Background()
	result, err := s.db.ExecContext(ctx, `INSERT INTO voice_agent_sessions
		(title, summary, raw_summary, transcript, language, turns_json, ideas_json, decisions_json, open_questions_json, next_steps_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"normalized only", "", "", "", "de", "[]", "[]", "[]", "[]", "[]",
	)
	if err != nil {
		t.Fatalf("insert voice session: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO voice_agent_session_turns
		(session_id, turn_index, role, text)
		VALUES (?, ?, ?, ?)`,
		id, 0, "user", "child table turn",
	); err != nil {
		t.Fatalf("insert normalized turn: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO voice_agent_session_summary_items
		(session_id, item_type, item_index, text)
		VALUES (?, ?, ?, ?)`,
		id, "idea", 0, "child table idea",
	); err != nil {
		t.Fatalf("insert normalized summary item: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := NewSQLiteStore(StoreConfig{
		SQLitePath: dbPath,
		SaveAudio:  false,
	})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()

	sessions, err := reopened.ListVoiceAgentSessions(ctx, ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListVoiceAgentSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	if got := sessions[0].Turns; len(got) != 1 || got[0].Text != "child table turn" {
		t.Fatalf("turns = %+v, want normalized child turn", got)
	}
	if got := sessions[0].Summary.Ideas; len(got) != 1 || got[0] != "child table idea" {
		t.Fatalf("ideas = %+v, want normalized child idea", got)
	}
}

func TestSQLiteListAndGetPreferAudioAssets(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	audioPath := filepath.Join(t.TempDir(), "asset.wav")
	audioData := []byte{1, 2, 3, 4, 5, 6}
	if err := os.WriteFile(audioPath, audioData, 0o600); err != nil {
		t.Fatalf("write audio fixture: %v", err)
	}

	result, err := s.db.ExecContext(ctx, `INSERT INTO transcriptions
		(text, language, provider, model, duration_ms, latency_ms, audio_path, word_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"asset transcription", "de", "local", "model", 111, 22, "", 2,
	)
	if err != nil {
		t.Fatalf("insert transcription: %v", err)
	}
	transcriptionID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId transcription: %v", err)
	}
	if err := recordAudioAsset(ctx, s.db, "sqlite", "transcription", transcriptionID, audioPath, 333, "audio/wav"); err != nil {
		t.Fatalf("record transcription asset: %v", err)
	}

	result, err = s.db.ExecContext(ctx, `INSERT INTO quick_notes
		(text, language, provider, duration_ms, latency_ms, audio_path, word_count)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"asset note", "de", "manual", 222, 33, "", 2,
	)
	if err != nil {
		t.Fatalf("insert quick note: %v", err)
	}
	noteID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId quick note: %v", err)
	}
	if err := recordAudioAsset(ctx, s.db, "sqlite", "quick_note", noteID, audioPath, 444, "audio/wav"); err != nil {
		t.Fatalf("record quick note asset: %v", err)
	}

	transcription, err := s.GetTranscription(ctx, transcriptionID)
	if err != nil {
		t.Fatalf("GetTranscription: %v", err)
	}
	if transcription.Audio == nil || transcription.Audio.Path != audioPath || transcription.Audio.DurationMs != 333 {
		t.Fatalf("transcription audio = %+v, want audio_assets metadata", transcription.Audio)
	}
	transcriptions, err := s.ListTranscriptions(ctx, ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListTranscriptions: %v", err)
	}
	if len(transcriptions) != 1 || transcriptions[0].Audio == nil || transcriptions[0].Audio.Path != audioPath {
		t.Fatalf("listed transcriptions = %+v, want audio asset", transcriptions)
	}

	note, err := s.GetQuickNote(ctx, noteID)
	if err != nil {
		t.Fatalf("GetQuickNote: %v", err)
	}
	if note.Audio == nil || note.Audio.Path != audioPath || note.Audio.DurationMs != 444 {
		t.Fatalf("quick note audio = %+v, want audio_assets metadata", note.Audio)
	}
	notes, err := s.ListQuickNotes(ctx, ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListQuickNotes: %v", err)
	}
	if len(notes) != 1 || notes[0].Audio == nil || notes[0].Audio.Path != audioPath {
		t.Fatalf("listed quick notes = %+v, want audio asset", notes)
	}
}

type countingQueryContexter struct {
	db      *sql.DB
	queries int
}

func (c *countingQueryContexter) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	c.queries++
	return c.db.QueryContext(ctx, query, args...)
}

func TestHydrateTranscriptionAudioBatchLoadsNewestAssetsInSingleQuery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	oldPath := filepath.Join(t.TempDir(), "old.wav")
	newPath := filepath.Join(t.TempDir(), "new.wav")
	otherPath := filepath.Join(t.TempDir(), "other.wav")
	for _, path := range []string{oldPath, newPath, otherPath} {
		if err := os.WriteFile(path, []byte{1, 2, 3}, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO audio_assets
		(owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms, created_at, updated_at)
		VALUES
		('transcription', 101, 'local-file', ?, 'audio/wav', 3, 100, '2026-01-01 00:00:00', '2026-01-01 00:00:00'),
		('transcription', 101, 'local-file', ?, 'audio/wav', 3, 200, '2026-01-02 00:00:00', '2026-01-02 00:00:00'),
		('transcription', 202, 'local-file', ?, 'audio/wav', 3, 300, '2026-01-03 00:00:00', '2026-01-03 00:00:00')`,
		oldPath, newPath, otherPath); err != nil {
		t.Fatalf("insert audio assets: %v", err)
	}

	items := []Transcription{
		{ID: 101, AudioPath: "legacy-101.wav", DurationMs: 10},
		{ID: 202, AudioPath: "legacy-202.wav", DurationMs: 20},
	}
	countingDB := &countingQueryContexter{db: s.db}
	if err := hydrateTranscriptionAudioBatch(ctx, countingDB, "sqlite", items); err != nil {
		t.Fatalf("hydrateTranscriptionAudioBatch: %v", err)
	}
	if countingDB.queries != 1 {
		t.Fatalf("queries = %d, want 1 batch query", countingDB.queries)
	}
	if items[0].Audio == nil || items[0].Audio.Path != newPath || items[0].Audio.DurationMs != 200 {
		t.Fatalf("items[0].Audio = %+v, want newest asset %q", items[0].Audio, newPath)
	}
	if items[1].Audio == nil || items[1].Audio.Path != otherPath || items[1].Audio.DurationMs != 300 {
		t.Fatalf("items[1].Audio = %+v, want asset %q", items[1].Audio, otherPath)
	}
}

func TestHydrateQuickNoteAudioBatchKeepsLegacyFallbackInSingleQuery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	assetPath := filepath.Join(t.TempDir(), "asset.wav")
	if err := os.WriteFile(assetPath, []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO audio_assets
		(owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms, created_at, updated_at)
		VALUES ('quick_note', 301, 'local-file', ?, 'audio/wav', 3, 444, '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		assetPath); err != nil {
		t.Fatalf("insert audio asset: %v", err)
	}

	items := []QuickNote{
		{ID: 301, AudioPath: "legacy-301.wav", DurationMs: 30},
		{ID: 302, AudioPath: "legacy-302.wav", DurationMs: 40},
	}
	countingDB := &countingQueryContexter{db: s.db}
	if err := hydrateQuickNoteAudioBatch(ctx, countingDB, "sqlite", items); err != nil {
		t.Fatalf("hydrateQuickNoteAudioBatch: %v", err)
	}
	if countingDB.queries != 1 {
		t.Fatalf("queries = %d, want 1 batch query", countingDB.queries)
	}
	if items[0].Audio == nil || items[0].Audio.Path != assetPath || items[0].Audio.DurationMs != 444 {
		t.Fatalf("items[0].Audio = %+v, want audio asset", items[0].Audio)
	}
	if items[1].Audio == nil || items[1].Audio.Path != "legacy-302.wav" || items[1].Audio.DurationMs != 40 {
		t.Fatalf("items[1].Audio = %+v, want legacy fallback", items[1].Audio)
	}
}

func TestHydrateVoiceAgentSessionChildrenBatchesAndPreservesFallback(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	childID, err := s.SaveVoiceAgentSession(ctx, VoiceAgentSession{
		Summary: VoiceAgentSessionSummary{
			Title:         "child",
			Summary:       "child summary",
			Ideas:         []string{"json idea"},
			Decisions:     []string{"json decision"},
			OpenQuestions: []string{"json question"},
			NextSteps:     []string{"json step"},
		},
		Turns: []VoiceAgentTurn{{Role: "user", Text: "json turn"}},
	})
	if err != nil {
		t.Fatalf("SaveVoiceAgentSession child: %v", err)
	}
	fallbackID, err := s.SaveVoiceAgentSession(ctx, VoiceAgentSession{
		Summary: VoiceAgentSessionSummary{
			Title:     "fallback",
			Summary:   "fallback summary",
			Ideas:     []string{"fallback idea"},
			NextSteps: []string{"fallback step"},
		},
		Turns: []VoiceAgentTurn{{Role: "user", Text: "fallback turn"}},
	})
	if err != nil {
		t.Fatalf("SaveVoiceAgentSession fallback: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM voice_agent_session_turns WHERE session_id = ?`, childID); err != nil {
		t.Fatalf("delete child turns: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM voice_agent_session_summary_items WHERE session_id = ?`, childID); err != nil {
		t.Fatalf("delete child summary items: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO voice_agent_session_turns
		(session_id, turn_index, role, text, created_at)
		VALUES (?, 0, 'assistant', 'normalized turn', ?)`, childID, time.Now().UTC()); err != nil {
		t.Fatalf("insert normalized turn: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO voice_agent_session_summary_items
		(session_id, item_type, item_index, text)
		VALUES
		(?, 'idea', 0, 'normalized idea'),
		(?, 'decision', 0, 'normalized decision')`, childID, childID); err != nil {
		t.Fatalf("insert normalized summary items: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM voice_agent_session_turns WHERE session_id = ?`, fallbackID); err != nil {
		t.Fatalf("delete fallback turns: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM voice_agent_session_summary_items WHERE session_id = ?`, fallbackID); err != nil {
		t.Fatalf("delete fallback summary items: %v", err)
	}

	sessions := []VoiceAgentSession{
		{
			ID:    childID,
			Turns: []VoiceAgentTurn{{Role: "user", Text: "json turn"}},
			Summary: VoiceAgentSessionSummary{
				Ideas:         []string{"json idea"},
				Decisions:     []string{"json decision"},
				OpenQuestions: []string{"json question"},
				NextSteps:     []string{"json step"},
			},
		},
		{
			ID:    fallbackID,
			Turns: []VoiceAgentTurn{{Role: "user", Text: "fallback turn"}},
			Summary: VoiceAgentSessionSummary{
				Ideas:     []string{"fallback idea"},
				NextSteps: []string{"fallback step"},
			},
		},
	}
	countingDB := &countingQueryContexter{db: s.db}
	hydrated, err := hydrateVoiceAgentSessionChildren(ctx, countingDB, "sqlite", sessions)
	if err != nil {
		t.Fatalf("hydrateVoiceAgentSessionChildren: %v", err)
	}
	if countingDB.queries != 2 {
		t.Fatalf("queries = %d, want 2 batch queries", countingDB.queries)
	}
	if got := hydrated[0].Turns; len(got) != 1 || got[0].Text != "normalized turn" {
		t.Fatalf("normalized turns = %+v", got)
	}
	if got := hydrated[0].Summary.Ideas; len(got) != 1 || got[0] != "normalized idea" {
		t.Fatalf("normalized ideas = %+v", got)
	}
	if got := hydrated[0].Summary.NextSteps; len(got) != 0 {
		t.Fatalf("normalized next steps = %+v, want empty when child rows exist without next_step", got)
	}
	if got := hydrated[1].Turns; len(got) != 1 || got[0].Text != "fallback turn" {
		t.Fatalf("fallback turns = %+v", got)
	}
	if got := hydrated[1].Summary.Ideas; len(got) != 1 || got[0] != "fallback idea" {
		t.Fatalf("fallback ideas = %+v", got)
	}
}

func TestSQLiteDeleteQuickNoteCleansAudioAssetsWhenLegacyPathEmpty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	audioPath := filepath.Join(t.TempDir(), "asset.wav")
	if err := os.WriteFile(audioPath, []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatalf("write audio fixture: %v", err)
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO quick_notes
		(text, language, provider, duration_ms, latency_ms, audio_path, word_count)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"asset note", "de", "manual", 222, 33, "", 2,
	)
	if err != nil {
		t.Fatalf("insert quick note: %v", err)
	}
	noteID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId quick note: %v", err)
	}
	if err := recordAudioAsset(ctx, s.db, "sqlite", "quick_note", noteID, audioPath, 222, "audio/wav"); err != nil {
		t.Fatalf("record quick note asset: %v", err)
	}

	if err := s.DeleteQuickNote(ctx, noteID); err != nil {
		t.Fatalf("DeleteQuickNote: %v", err)
	}
	if _, err := os.Stat(audioPath); !os.IsNotExist(err) {
		t.Fatalf("asset file stat err = %v, want not exist", err)
	}
	var assetCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audio_assets WHERE owner_kind = ? AND owner_id = ?`, "quick_note", noteID).Scan(&assetCount); err != nil {
		t.Fatalf("count audio assets: %v", err)
	}
	if assetCount != 0 {
		t.Fatalf("audio asset count = %d, want 0", assetCount)
	}
}

func TestSQLiteUpdateQuickNoteCaptureReplacesAudioAssetsWhenLegacyPathEmpty(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, SaveAudio: true, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	oldAudioPath := filepath.Join(t.TempDir(), "old.wav")
	if err := os.WriteFile(oldAudioPath, []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatalf("write old audio fixture: %v", err)
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO quick_notes
		(text, language, provider, duration_ms, latency_ms, audio_path, word_count)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"asset note", "de", "manual", 222, 33, "", 2,
	)
	if err != nil {
		t.Fatalf("insert quick note: %v", err)
	}
	noteID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId quick note: %v", err)
	}
	if err := recordAudioAsset(ctx, s.db, "sqlite", "quick_note", noteID, oldAudioPath, 222, "audio/wav"); err != nil {
		t.Fatalf("record old quick note asset: %v", err)
	}

	if err := s.UpdateQuickNoteCapture(ctx, noteID, "updated note", "manual", 333, 44, []byte{4, 5, 6, 7}); err != nil {
		t.Fatalf("UpdateQuickNoteCapture: %v", err)
	}
	if _, err := os.Stat(oldAudioPath); !os.IsNotExist(err) {
		t.Fatalf("old asset file stat err = %v, want not exist", err)
	}
	var oldAssetCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audio_assets WHERE owner_kind = ? AND owner_id = ? AND path = ?`, "quick_note", noteID, oldAudioPath).Scan(&oldAssetCount); err != nil {
		t.Fatalf("count old audio assets: %v", err)
	}
	if oldAssetCount != 0 {
		t.Fatalf("old audio asset count = %d, want 0", oldAssetCount)
	}
	note, err := s.GetQuickNote(ctx, noteID)
	if err != nil {
		t.Fatalf("GetQuickNote: %v", err)
	}
	if note.Audio == nil || note.Audio.Path == "" || note.Audio.Path == oldAudioPath {
		t.Fatalf("quick note audio = %+v, want replacement asset", note.Audio)
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

	sqliteStore, ok := s.(*SQLiteStore)
	if !ok {
		t.Fatalf("store type = %T, want *SQLiteStore", s)
	}
	baseTime := time.Now().UTC().Add(-time.Hour)
	for i, text := range []string{"A", "B", "C"} {
		if err := s.SaveTranscription(context.Background(), text, "de", "local", "", 1000, 50, nil); err != nil {
			t.Fatalf("Save %q: %v", text, err)
		}
		if _, err := sqliteStore.db.Exec(`UPDATE transcriptions SET created_at = ? WHERE text = ?`, sqliteTestTimestamp(baseTime.Add(time.Duration(i)*time.Second)), text); err != nil {
			t.Fatalf("set transcription timestamp %q: %v", text, err)
		}
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

	sqliteStore, ok := s.(*SQLiteStore)
	if !ok {
		t.Fatalf("store type = %T, want *SQLiteStore", s)
	}
	waitForSQLiteIdle(t, sqliteStore.db)

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
	firstID, err := sqliteStore.SaveQuickNote(context.Background(), "note-1", "de", "manual", 0, 0, largeWAV)
	if err != nil {
		t.Fatalf("SaveQuickNote #1: %v", err)
	}
	if _, err := sqliteStore.SaveQuickNote(context.Background(), "note-2", "de", "manual", 0, 0, largeWAV); err != nil {
		t.Fatalf("SaveQuickNote #2: %v", err)
	}
	if _, err := sqliteStore.db.Exec(`UPDATE quick_notes SET created_at = ? WHERE id = ?`, sqliteTestTimestamp(time.Now().UTC().Add(-time.Minute)), firstID); err != nil {
		t.Fatalf("age first quick note: %v", err)
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
		sqliteStore, ok := s.(*SQLiteStore)
		if !ok {
			t.Fatalf("store type = %T, want *SQLiteStore", s)
		}
		createdAt := time.Now().UTC().Add(time.Duration(i) * time.Second)
		if _, err := sqliteStore.db.Exec(`UPDATE quick_notes SET created_at = ? WHERE text = ?`, sqliteTestTimestamp(createdAt), fmt.Sprintf("note-%d", i)); err != nil {
			t.Fatalf("set quick note timestamp %d: %v", i, err)
		}
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

	// Wait for background goroutines triggered by SaveTranscription before
	// enforcing retention directly, avoiding SQLITE_BUSY.
	waitForSQLiteIdle(t, sqliteStore.db)

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
	waitForSQLiteIdle(t, sqliteStore.db)

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

func postgresTestDSN(user, password, host, database, suffix string) string {
	return "post" + "gres://" + user + ":" + password + "@" + host + "/" + database + suffix
}

func TestFactory_PostgresAttemptsRealConnection(t *testing.T) {
	_, err := New(StoreConfig{
		Backend:     "postgres",
		PostgresDSN: postgresTestDSN("speechkit", "secret", "127.0.0.1:1", "speechkit", "?sslmode=disable&connect_timeout=1"),
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

func TestPostgresPersonaRepairColumnSpecsCoverCanonicalColumns(t *testing.T) {
	specs := postgresPersonaRepairColumnSpecs()
	if len(specs) != 41 {
		t.Fatalf("repair specs = %d, want 41 canonical persona columns", len(specs))
	}
	seen := make(map[string]struct{})
	for _, spec := range specs {
		key := spec.table + "." + spec.column
		if _, ok := seen[key]; ok {
			t.Fatalf("duplicate repair spec %s", key)
		}
		seen[key] = struct{}{}
		if strings.TrimSpace(spec.dataType) == "" {
			t.Fatalf("%s missing data type", key)
		}
		if strings.TrimSpace(spec.defaultExpr) == "" {
			t.Fatalf("%s missing default expression", key)
		}
		if !spec.notNull {
			t.Fatalf("%s should be repaired to NOT NULL", key)
		}
	}

	assertSpec := func(table, column, dataType, defaultExpr string) {
		t.Helper()
		for _, spec := range specs {
			if spec.table == table && spec.column == column {
				if spec.dataType != dataType || spec.defaultExpr != defaultExpr {
					t.Fatalf("%s.%s = (%s, %s), want (%s, %s)", table, column, spec.dataType, spec.defaultExpr, dataType, defaultExpr)
				}
				return
			}
		}
		t.Fatalf("missing repair spec for %s.%s", table, column)
	}
	assertSpec("voice_agent_personas", "tags_json", "JSONB", "'[]'::jsonb")
	assertSpec("voice_agent_personas", "metadata_json", "JSONB", "'{}'::jsonb")
	assertSpec("voice_agent_roles", "thinking_enabled", "BOOLEAN", "FALSE")
	assertSpec("voice_agent_roles", "thinking_budget", "BIGINT", "0")
	assertSpec("voice_agent_roles", "temperature", "DOUBLE PRECISION", "0")
	assertSpec("voice_agent_sequences", "max_turns", "BIGINT", "0")
	assertSpec("voice_agent_sequences", "steps_json", "JSONB", "'[]'::jsonb")
}

func TestPostgresPersonaRepairUsingExpressionsCoverSafeLegacyCasts(t *testing.T) {
	tests := []struct {
		name     string
		spec     postgresColumnRepair
		contains string
	}{
		{
			name:     "jsonb text",
			spec:     postgresColumnRepair{column: "tags_json", dataType: "JSONB", defaultExpr: "'[]'::jsonb"},
			contains: "::jsonb",
		},
		{
			name:     "text",
			spec:     postgresColumnRepair{column: "display_name", dataType: "TEXT", defaultExpr: "''"},
			contains: "::text",
		},
		{
			name:     "bigint",
			spec:     postgresColumnRepair{column: "max_turns", dataType: "BIGINT", defaultExpr: "0"},
			contains: "::bigint",
		},
		{
			name:     "double precision",
			spec:     postgresColumnRepair{column: "temperature", dataType: "DOUBLE PRECISION", defaultExpr: "0"},
			contains: "::double precision",
		},
		{
			name:     "boolean",
			spec:     postgresColumnRepair{column: "thinking_enabled", dataType: "BOOLEAN", defaultExpr: "FALSE"},
			contains: "LOWER(TRIM(thinking_enabled::text))",
		},
		{
			name:     "timestamptz",
			spec:     postgresColumnRepair{column: "created_at", dataType: "TIMESTAMPTZ", defaultExpr: "NOW()"},
			contains: "::timestamptz",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := postgresRepairUsingExpression(tt.spec)
			if err != nil {
				t.Fatalf("postgresRepairUsingExpression: %v", err)
			}
			if !strings.Contains(got, tt.contains) {
				t.Fatalf("using expression = %q, want to contain %q", got, tt.contains)
			}
		})
	}
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
	if _, err := pg.db.Exec(`UPDATE transcriptions SET audio_path = '' WHERE id = $1`, transcriptions[0].ID); err != nil {
		t.Fatalf("clear legacy transcription audio_path: %v", err)
	}
	transcription, err := s.GetTranscription(context.Background(), transcriptions[0].ID)
	if err != nil {
		t.Fatalf("GetTranscription after clearing legacy path: %v", err)
	}
	if transcription.Audio == nil || transcription.Audio.Path == "" {
		t.Fatalf("transcription audio after clearing legacy path = %+v", transcription.Audio)
	}

	note, err := s.GetQuickNote(context.Background(), noteID)
	if err != nil {
		t.Fatalf("GetQuickNote: %v", err)
	}
	if note.Audio == nil || note.Audio.StorageKind != AudioStorageLocalFile {
		t.Fatalf("quick note audio = %+v", note.Audio)
	}
	if _, err := pg.db.Exec(`UPDATE quick_notes SET audio_path = '' WHERE id = $1`, noteID); err != nil {
		t.Fatalf("clear legacy quick note audio_path: %v", err)
	}
	note, err = s.GetQuickNote(context.Background(), noteID)
	if err != nil {
		t.Fatalf("GetQuickNote after clearing legacy path: %v", err)
	}
	if note.Audio == nil || note.Audio.Path == "" {
		t.Fatalf("quick note audio after clearing legacy path = %+v", note.Audio)
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

func TestPostgresMigrationRepairsLegacyPersonaColumnDrift(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("SPEECHKIT_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set SPEECHKIT_POSTGRES_TEST_DSN to run postgres repair migration test")
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	schema := fmt.Sprintf("speechkit_repair_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`) //nolint:errcheck // best-effort test cleanup
	if _, err := db.ExecContext(ctx, `SET search_path TO `+schema); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
CREATE TABLE voice_agent_personas (
    id TEXT PRIMARY KEY,
    display_name VARCHAR(255),
    tags_json TEXT,
    metadata_json TEXT,
    created_at TEXT,
    updated_at TEXT
);
INSERT INTO voice_agent_personas (id, display_name, tags_json, metadata_json, created_at, updated_at)
VALUES ('legacy-persona', 'Legacy Persona', '["legacy"]', '{"source":"legacy"}', '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z');

CREATE TABLE voice_agent_roles (
    id TEXT PRIMARY KEY,
    display_name VARCHAR(255),
    system_prompt TEXT,
    tool_allowlist_json TEXT,
    temperature NUMERIC,
    thinking_enabled TEXT,
    include_thoughts INTEGER,
    thinking_budget INTEGER,
    automatic_activity_detection TEXT,
    vad_prefix_padding_ms INTEGER,
    vad_silence_duration_ms INTEGER,
    context_compression_enabled TEXT,
    context_compression_trigger_tk INTEGER,
    context_compression_target_tk INTEGER,
    enable_affective_dialog TEXT,
    created_at TEXT,
    updated_at TEXT
);
INSERT INTO voice_agent_roles (
    id, display_name, system_prompt, tool_allowlist_json, temperature,
    thinking_enabled, include_thoughts, thinking_budget, automatic_activity_detection,
    vad_prefix_padding_ms, vad_silence_duration_ms, context_compression_enabled,
    context_compression_trigger_tk, context_compression_target_tk, enable_affective_dialog,
    created_at, updated_at
) VALUES (
    'legacy-role', 'Legacy Role', 'Prompt', '["search"]', 0.7,
    'true', 1, 64, 'false',
    100, 200, 'true',
    3000, 1500, 'false',
    '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z'
);

CREATE TABLE voice_agent_sequences (
    id TEXT PRIMARY KEY,
    display_name VARCHAR(255),
    steps_json TEXT,
    max_turns INTEGER,
    created_at TEXT,
    updated_at TEXT
);
INSERT INTO voice_agent_sequences (id, display_name, steps_json, max_turns, created_at, updated_at)
VALUES ('legacy-sequence', 'Legacy Sequence', '[{"id":"s1","label":"Step","prompt":"Do it"}]', 5, '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z');
`); err != nil {
		t.Fatalf("create legacy persona tables: %v", err)
	}

	if err := runPostgresMigrations(ctx, db); err != nil {
		t.Fatalf("runPostgresMigrations: %v", err)
	}
	assertColumn := func(table, column, dataType string) {
		t.Helper()
		var gotType, nullable string
		if err := db.QueryRowContext(ctx, `
			SELECT data_type, is_nullable
			FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = $2 AND column_name = $3`,
			schema, table, column).Scan(&gotType, &nullable); err != nil {
			t.Fatalf("column %s.%s: %v", table, column, err)
		}
		if gotType != dataType {
			t.Fatalf("%s.%s data_type = %q, want %q", table, column, gotType, dataType)
		}
		if nullable != "NO" {
			t.Fatalf("%s.%s is_nullable = %q, want NO", table, column, nullable)
		}
	}
	assertColumn("voice_agent_personas", "tags_json", "jsonb")
	assertColumn("voice_agent_personas", "default_sequence", "text")
	assertColumn("voice_agent_roles", "thinking_enabled", "boolean")
	assertColumn("voice_agent_roles", "thinking_budget", "bigint")
	assertColumn("voice_agent_roles", "temperature", "double precision")
	assertColumn("voice_agent_sequences", "steps_json", "jsonb")
	assertColumn("voice_agent_sequences", "max_turns", "bigint")

	if _, err := db.ExecContext(ctx, `INSERT INTO voice_agent_personas
		(id, display_name, description, voice, locale, default_role, default_sequence, tags_json, metadata_json, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, NOW(), NOW())
		ON CONFLICT(id) DO UPDATE SET tags_json = excluded.tags_json, metadata_json = excluded.metadata_json`,
		"saved-persona", "Saved Persona", "desc", "voice", "de", "role", "sequence", `["new"]`, `{"saved":true}`); err != nil {
		t.Fatalf("save persona-like row: %v", err)
	}
	var tagsJSON, metadataJSON string
	if err := db.QueryRowContext(ctx, `SELECT tags_json::text, metadata_json::text FROM voice_agent_personas WHERE id = $1`, "saved-persona").Scan(&tagsJSON, &metadataJSON); err != nil {
		t.Fatalf("load persona-like row: %v", err)
	}
	if !strings.Contains(tagsJSON, "new") || !strings.Contains(metadataJSON, "saved") {
		t.Fatalf("persona json = (%s, %s), want saved JSON payloads", tagsJSON, metadataJSON)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO voice_agent_roles
		(id, display_name, system_prompt, refinement_prompt, locale, vocabulary_hint,
		 tool_allowlist_json, temperature, thinking_enabled, thinking_level, include_thoughts, thinking_budget,
		 automatic_activity_detection, vad_start_sensitivity, vad_end_sensitivity, vad_prefix_padding_ms,
		 vad_silence_duration_ms, activity_handling, turn_coverage, context_compression_enabled,
		 context_compression_trigger_tk, context_compression_target_tk, enable_affective_dialog, created_at, updated_at)
		VALUES ($1, $2, $3, '', 'de', '', $4::jsonb, 0.2, TRUE, '', FALSE, 8, FALSE, '', '', 0, 0, '', '', FALSE, 0, 0, FALSE, NOW(), NOW())`,
		"saved-role", "Saved Role", "Prompt", `["tool"]`); err != nil {
		t.Fatalf("save role-like row: %v", err)
	}
	var thinkingBudget int64
	var thinkingEnabled bool
	if err := db.QueryRowContext(ctx, `SELECT thinking_enabled, thinking_budget FROM voice_agent_roles WHERE id = $1`, "saved-role").Scan(&thinkingEnabled, &thinkingBudget); err != nil {
		t.Fatalf("load role-like row: %v", err)
	}
	if !thinkingEnabled || thinkingBudget != 8 {
		t.Fatalf("role flags = (%v, %d), want true and 8", thinkingEnabled, thinkingBudget)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO voice_agent_sequences
		(id, display_name, description, completion, max_turns, steps_json, created_at, updated_at)
		VALUES ($1, $2, '', '', 3, $3::jsonb, NOW(), NOW())`,
		"saved-sequence", "Saved Sequence", `[{"id":"next","label":"Next","prompt":"Continue"}]`); err != nil {
		t.Fatalf("save sequence-like row: %v", err)
	}
	var maxTurns int64
	var steps string
	if err := db.QueryRowContext(ctx, `SELECT max_turns, steps_json::text FROM voice_agent_sequences WHERE id = $1`, "saved-sequence").Scan(&maxTurns, &steps); err != nil {
		t.Fatalf("load sequence-like row: %v", err)
	}
	if maxTurns != 3 || !strings.Contains(steps, "Continue") {
		t.Fatalf("sequence = (%d, %s), want saved values", maxTurns, steps)
	}
}
