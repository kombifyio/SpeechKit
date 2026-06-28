package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/testutil"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

// waitForNextSecond busy-waits until time.Now() crosses the next
// whole-second boundary. Replaces flat time.Sleep(1100*time.Millisecond)
// sites where the only requirement is "the NEXT save gets a different
// SQLite CURRENT_TIMESTAMP" (audit 4.4). Worst case 1 s wait; best case
// near-zero when called right before a second tick. Use only when the
// schema actually has 1-second-resolution timestamps.
func waitForNextSecond() {
	start := time.Now().Unix()
	for time.Now().Unix() == start {
		time.Sleep(5 * time.Millisecond)
	}
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

func TestNormalizedLanguageFilterHelpers(t *testing.T) {
	clauses, args := appendNormalizedLanguageFilter(nil, nil, "de-DE")
	if len(clauses) != 1 || len(args) != 1 {
		t.Fatalf("filter clauses=%v args=%v, want one clause and one arg", clauses, args)
	}
	if !strings.Contains(clauses[0], "?") {
		t.Fatalf("filter clause must use a `?` placeholder (rebound per dialect): %s", clauses[0])
	}
	clauses, args = appendNormalizedLanguageFilter(clauses, args, " ")
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

func TestRecordingSessionStoreLifecycle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recording_sessions.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	sessionID, err := s.SaveRecordingSession(ctx, RecordingSession{
		ExternalID:     "meeting-1",
		Kind:           RecordingSessionKindMeeting,
		Title:          "Planning meeting",
		Language:       "de-DE",
		Provider:       "deepgram",
		Model:          "nova-3",
		InputSource:    "system_loopback",
		ProcessingMode: "segment_batch",
	})
	if err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}
	segmentID, err := s.AppendRecordingSessionSegment(ctx, sessionID, RecordingSessionSegment{
		SegmentIndex:   0,
		ProviderItemID: "deepgram:1",
		Text:           "Hallo zusammen",
		IsFinal:        true,
		StartedMs:      0,
		EndedMs:        1200,
	})
	if err != nil {
		t.Fatalf("AppendRecordingSessionSegment: %v", err)
	}
	updatedSegmentID, err := s.AppendRecordingSessionSegment(ctx, sessionID, RecordingSessionSegment{
		SegmentIndex:   0,
		ProviderItemID: "deepgram:retry-1",
		Text:           "Hallo zusammen korrigiert",
		IsFinal:        true,
		StartedMs:      0,
		EndedMs:        1300,
	})
	if err != nil {
		t.Fatalf("AppendRecordingSessionSegment retry: %v", err)
	}
	if updatedSegmentID != segmentID {
		t.Fatalf("retry segment id = %d, want original %d", updatedSegmentID, segmentID)
	}
	if err := s.UpdateRecordingSessionCaptureStatus(ctx, sessionID, RecordingSessionCaptureRecording, time.Now()); err != nil {
		t.Fatalf("UpdateRecordingSessionCaptureStatus recording: %v", err)
	}
	if err := s.UpdateRecordingSessionCaptureStatus(ctx, sessionID, RecordingSessionCapturePaused, time.Now()); err != nil {
		t.Fatalf("UpdateRecordingSessionCaptureStatus paused: %v", err)
	}
	if err := s.UpdateRecordingSessionSummaryStatus(ctx, sessionID, RecordingSessionSummaryRunning, "", time.Now()); err != nil {
		t.Fatalf("UpdateRecordingSessionSummaryStatus running: %v", err)
	}
	if err := s.UpdateRecordingSessionSummary(ctx, sessionID, "Vorab Zusammenfassung"); err != nil {
		t.Fatalf("UpdateRecordingSessionSummary: %v", err)
	}
	if err := s.FinishRecordingSession(ctx, sessionID, "Kurze Zusammenfassung", time.Now()); err != nil {
		t.Fatalf("FinishRecordingSession: %v", err)
	}
	listed, err := s.ListRecordingSessions(ctx, ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListRecordingSessions: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != sessionID {
		t.Fatalf("listed recording sessions = %+v", listed)
	}

	got, err := s.GetRecordingSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetRecordingSession: %v", err)
	}
	if got.Kind != RecordingSessionKindMeeting || got.Status != RecordingSessionStatusFinished {
		t.Fatalf("recording session status = %s/%s", got.Kind, got.Status)
	}
	if got.CaptureStatus != RecordingSessionCaptureStopped || got.CaptureStartedAt.IsZero() || got.CapturePausedAt.IsZero() || got.CaptureStoppedAt.IsZero() {
		t.Fatalf("recording session capture fields = %+v", got)
	}
	if got.SummaryStatus != RecordingSessionSummaryReady || got.SummaryUpdatedAt.IsZero() || got.SummaryError != "" {
		t.Fatalf("recording session summary fields = %+v", got)
	}
	if got.Language != "de-DE" || got.InputSource != "system_loopback" || got.ProcessingMode != "segment_batch" {
		t.Fatalf("recording session metadata = %+v", got)
	}
	if len(got.Segments) != 1 || got.Segments[0].Text != "Hallo zusammen korrigiert" || got.Segments[0].ProviderItemID != "deepgram:retry-1" || !got.Segments[0].IsFinal {
		t.Fatalf("recording session segments = %+v", got.Segments)
	}
	if got.Summary != "Kurze Zusammenfassung" || got.EndedAt.IsZero() {
		t.Fatalf("recording session finish fields = %+v", got)
	}
	if err := s.DeleteRecordingSession(ctx, sessionID); err != nil {
		t.Fatalf("DeleteRecordingSession: %v", err)
	}
	if _, err := s.GetRecordingSession(ctx, sessionID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetRecordingSession after delete err = %v, want sql.ErrNoRows", err)
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

func TestCustomizationStoreWordsReplacementsAndDictionaryProjection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	customizationStore, ok := s.(CustomizationStore)
	if !ok {
		t.Fatal("sqlite store does not implement CustomizationStore")
	}

	ctx := context.Background()
	if err := customizationStore.ReplaceWords(ctx, "de", []speechcustomize.Word{
		{Term: "Kombify", Language: "de-DE", Source: "settings", Enabled: true},
		{Term: "AcmeOS", Language: "de", Source: "settings", Enabled: true},
	}); err != nil {
		t.Fatalf("ReplaceWords: %v", err)
	}
	if err := customizationStore.ReplaceReplacements(ctx, "de", []speechcustomize.Replacement{
		{
			Kind:     speechcustomize.KindSubstitution,
			Language: "de",
			Modes:    []speechcustomize.Mode{speechcustomize.ModeDictation, speechcustomize.ModeAssist},
			Stage:    speechcustomize.StagePostSTT,
			Match:    speechcustomize.Match{Type: speechcustomize.MatchSpokenAlias, Pattern: "kombi fire", WordBoundary: true},
			Output:   speechcustomize.ReplacementOutput{Text: "Kombify"},
			Enabled:  true,
			Source:   "settings",
		},
	}); err != nil {
		t.Fatalf("ReplaceReplacements: %v", err)
	}

	words, err := customizationStore.ListWords(ctx, CustomizationListOpts{Language: "de-DE"})
	if err != nil {
		t.Fatalf("ListWords: %v", err)
	}
	if len(words) != 2 {
		t.Fatalf("ListWords len = %d, want 2", len(words))
	}
	replacements, err := customizationStore.ListReplacements(ctx, CustomizationListOpts{Language: "de", Mode: speechcustomize.ModeAssist, Stage: speechcustomize.StagePostSTT})
	if err != nil {
		t.Fatalf("ListReplacements: %v", err)
	}
	if len(replacements) != 1 || replacements[0].Output.Text != "Kombify" {
		t.Fatalf("ListReplacements = %+v", replacements)
	}

	dictionaryStore := s.(UserDictionaryStore)
	entries, err := dictionaryStore.ListUserDictionaryEntries(ctx, "de")
	if err != nil {
		t.Fatalf("ListUserDictionaryEntries: %v", err)
	}
	if len(entries) != 2 || entries[0].Spoken != "kombi fire" || entries[0].Canonical != "Kombify" {
		t.Fatalf("dictionary projection = %+v", entries)
	}
}

func TestCustomizationStoreReplaceWordsWithOptionsIsSourceAware(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	customizationStore := s.(CustomizationStore)
	sourceStore := s.(CustomizationSourceStore)
	ctx := context.Background()
	if err := customizationStore.ReplaceWords(ctx, "de", []speechcustomize.Word{
		{Term: "UserTerm", Language: "de", Source: "settings", Enabled: true},
	}); err != nil {
		t.Fatalf("ReplaceWords settings: %v", err)
	}
	if err := sourceStore.ReplaceWordsWithOptions(ctx, CustomizationReplaceOpts{Language: "de", Source: "pack:demo"}, []speechcustomize.Word{
		{Term: "PackTerm", Language: "de", Source: "pack:demo", Enabled: true},
	}); err != nil {
		t.Fatalf("ReplaceWords pack: %v", err)
	}
	if err := sourceStore.ReplaceWordsWithOptions(ctx, CustomizationReplaceOpts{Language: "de", Source: "pack:demo"}, []speechcustomize.Word{
		{Term: "PackTerm2", Language: "de", Source: "pack:demo", Enabled: true},
	}); err != nil {
		t.Fatalf("ReplaceWords pack second import: %v", err)
	}
	words, err := customizationStore.ListWords(ctx, CustomizationListOpts{Language: "de", IncludeDisabled: true})
	if err != nil {
		t.Fatalf("ListWords: %v", err)
	}
	got := map[string]bool{}
	for _, word := range words {
		got[word.Term] = true
	}
	if !got["UserTerm"] || !got["PackTerm2"] || got["PackTerm"] {
		t.Fatalf("source-aware words = %+v", words)
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
	if got.Transcript != "" || len(got.Turns) != 0 || got.Summary.RawText != "" {
		t.Fatalf("list response should be light, got %#v", got)
	}
	detail, err := sessionStore.GetVoiceAgentSession(context.Background(), id)
	if err != nil {
		t.Fatalf("GetVoiceAgentSession: %v", err)
	}
	if len(detail.Turns) != 2 || detail.Turns[0].Role != "user" {
		t.Fatalf("detail turns = %#v", detail.Turns)
	}
	if len(detail.Summary.NextSteps) != 1 || detail.Summary.NextSteps[0] != "Build pruefen" {
		t.Fatalf("detail next steps = %#v", detail.Summary.NextSteps)
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
		PostgresDSN: "postgres://127.0.0.1:1/speechkit?sslmode=disable&connect_timeout=1",
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
