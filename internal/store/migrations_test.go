package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
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

func TestSQLiteRecordingSessionStateMigrationIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	first, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("first NewSQLiteStore: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}

	second, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("second NewSQLiteStore should rerun migration 019 idempotently: %v", err)
	}
	defer second.Close()

	for _, column := range []string{
		"capture_status",
		"capture_started_at",
		"capture_paused_at",
		"capture_stopped_at",
		"summary_status",
		"summary_error",
		"summary_updated_at",
	} {
		exists, err := sqliteColumnExists(context.Background(), second.db, "recording_sessions", column)
		if err != nil {
			t.Fatalf("check column %s: %v", column, err)
		}
		if !exists {
			t.Fatalf("recording_sessions.%s missing after idempotent migration", column)
		}
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
	if _, err := s.db.Exec(`INSERT INTO voice_agent_roles
		(id, display_name, system_prompt, refinement_prompt, tool_allowlist_json)
		VALUES (?, ?, ?, ?, ?)`,
		"r", "Role", "help", "tighten", `["clipboard.read"]`,
	); err != nil {
		t.Fatalf("insert role with repaired persona-role columns: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO voice_agent_sequences
		(id, display_name, description, completion, max_turns, steps_json)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"seq", "Sequence", "legacy repair", "explicit_close", 3, `[{"id":"one","instruction":"start"}]`,
	); err != nil {
		t.Fatalf("insert sequence with repaired persona-sequence columns: %v", err)
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
