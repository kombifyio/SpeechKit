package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

func TestSQLiteStorage3CreatesScopesAndUsesLocalDefault(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "storage3.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	if !sqliteTestTableExists(t, s.db, "storage_scopes") {
		t.Fatal("storage_scopes table missing")
	}
	if !sqliteTestColumnExists(t, s.db, "transcriptions", "scope_id") {
		t.Fatal("transcriptions.scope_id column missing")
	}
	if err := s.SaveTranscription(context.Background(), "local default", "en-US", "local", "ggml", 1000, 40, nil); err != nil {
		t.Fatalf("SaveTranscription: %v", err)
	}
	var scopeKey string
	if err := s.db.QueryRow(`SELECT s.scope_key
		FROM transcriptions t
		JOIN storage_scopes s ON s.id = t.scope_id`).Scan(&scopeKey); err != nil {
		t.Fatalf("read saved scope: %v", err)
	}
	if scopeKey != "install:local" {
		t.Fatalf("scope_key = %q, want install:local", scopeKey)
	}
}

func TestSQLiteStorage3EnforcesForeignKeys(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "storage3.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	var enabled int
	if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&enabled); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if enabled != 1 {
		t.Fatalf("foreign_keys = %d, want 1", enabled)
	}
}

func TestSQLiteStorage3MigrationDoesNotRebuildDictionaryAfterApplied(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "storage3.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	var before int
	if err := s.db.QueryRow(`SELECT rootpage FROM sqlite_master WHERE type = 'table' AND name = 'user_dictionary_entries'`).Scan(&before); err != nil {
		t.Fatalf("read dictionary rootpage before rerun: %v", err)
	}
	if err := runSQLiteStorage3ModelMigration(context.Background(), s.db); err != nil {
		t.Fatalf("runSQLiteStorage3ModelMigration rerun: %v", err)
	}
	var after int
	if err := s.db.QueryRow(`SELECT rootpage FROM sqlite_master WHERE type = 'table' AND name = 'user_dictionary_entries'`).Scan(&after); err != nil {
		t.Fatalf("read dictionary rootpage after rerun: %v", err)
	}
	if after != before {
		t.Fatalf("dictionary table rootpage changed from %d to %d; migration rebuilt an already-v3 table", before, after)
	}
}

func TestSQLiteStorage3IsolatesHistoryByScope(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "storage3.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctxA := speechstorage.WithScope(context.Background(), speechstorage.Scope{UserID: "alice"})
	ctxB := speechstorage.WithScope(context.Background(), speechstorage.Scope{UserID: "bob"})
	if err := s.SaveTranscription(ctxA, "alice words", "en", "local", "", 1000, 100, nil); err != nil {
		t.Fatalf("SaveTranscription alice: %v", err)
	}
	if err := s.SaveTranscription(ctxB, "bob words", "en", "local", "", 2000, 200, nil); err != nil {
		t.Fatalf("SaveTranscription bob: %v", err)
	}
	notesA, err := s.SaveQuickNote(ctxA, "alice note", "en", "manual", 500, 50, nil)
	if err != nil {
		t.Fatalf("SaveQuickNote alice: %v", err)
	}
	if notesA == 0 {
		t.Fatal("SaveQuickNote alice returned id 0")
	}

	alice, err := s.ListTranscriptions(ctxA, ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListTranscriptions alice: %v", err)
	}
	bob, err := s.ListTranscriptions(ctxB, ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListTranscriptions bob: %v", err)
	}
	if len(alice) != 1 || alice[0].Text != "alice words" {
		t.Fatalf("alice transcriptions = %+v", alice)
	}
	if len(bob) != 1 || bob[0].Text != "bob words" {
		t.Fatalf("bob transcriptions = %+v", bob)
	}

	statsA, err := s.Stats(ctxA)
	if err != nil {
		t.Fatalf("Stats alice: %v", err)
	}
	statsB, err := s.Stats(ctxB)
	if err != nil {
		t.Fatalf("Stats bob: %v", err)
	}
	if statsA.Transcriptions != 1 || statsA.QuickNotes != 1 || statsA.TotalWords != 4 {
		t.Fatalf("alice stats = %+v, want one transcription, one quick note, four words", statsA)
	}
	if statsB.Transcriptions != 1 || statsB.QuickNotes != 0 || statsB.TotalWords != 2 {
		t.Fatalf("bob stats = %+v, want one transcription, zero quick notes, two words", statsB)
	}
}

func TestSQLiteStorage3StatsUpdateRollsBackWhenRefreshFails(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "storage3.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	noteID, err := s.SaveQuickNote(ctx, "before", "en-US", "manual", 100, 10, nil)
	if err != nil {
		t.Fatalf("SaveQuickNote: %v", err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_store_stats_update
		BEFORE UPDATE ON store_stats
		BEGIN
			SELECT RAISE(FAIL, 'stats refresh failed');
		END`); err != nil {
		t.Fatalf("create stats failure trigger: %v", err)
	}
	if err := s.UpdateQuickNote(ctx, noteID, "after"); err == nil {
		t.Fatal("UpdateQuickNote succeeded, want stats refresh error")
	}
	var text string
	if err := s.db.QueryRow(`SELECT text FROM quick_notes WHERE id = ?`, noteID).Scan(&text); err != nil {
		t.Fatalf("read quick note text: %v", err)
	}
	if text != "before" {
		t.Fatalf("quick note text = %q, want rollback to before", text)
	}
}

func TestSQLiteStorage3ScopePolicyCanRequireUser(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{
		SQLitePath:  filepath.Join(t.TempDir(), "storage3.db"),
		ScopePolicy: speechstorage.ScopeUserRequired,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	if err := s.SaveTranscription(context.Background(), "missing user", "en", "local", "", 0, 0, nil); err == nil {
		t.Fatal("SaveTranscription without user succeeded, want scope policy error")
	}
	ctx := speechstorage.WithScope(context.Background(), speechstorage.Scope{UserID: "alice"})
	if err := s.SaveTranscription(ctx, "has user", "en", "local", "", 0, 0, nil); err != nil {
		t.Fatalf("SaveTranscription with user: %v", err)
	}
}

func TestSQLiteStorage3ListTranscriptionsDoesNotDuplicateMultipleSourceLinks(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(StoreConfig{
		SQLitePath:        filepath.Join(dir, "storage3.db"),
		SaveAudio:         true,
		MaxAudioStorageMB: 100,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.SaveTranscription(ctx, "with duplicate links", "en-US", "local", "", 1200, 20, []byte{1, 2, 3}); err != nil {
		t.Fatalf("SaveTranscription: %v", err)
	}
	records, err := s.ListTranscriptions(ctx, ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListTranscriptions: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1 before duplicate setup", len(records))
	}

	extraPath := filepath.Join(dir, "audio", "extra.wav")
	if err := os.WriteFile(extraPath, []byte{4, 5, 6, 7}, 0o600); err != nil {
		t.Fatalf("write extra audio: %v", err)
	}
	var scopeID int64
	if err := s.db.QueryRow(`SELECT scope_id FROM transcriptions WHERE id = ?`, records[0].ID).Scan(&scopeID); err != nil {
		t.Fatalf("read scope id: %v", err)
	}
	result, err := s.db.Exec(`INSERT INTO audio_assets
		(scope_id, owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms)
		VALUES (?, 'transcription', ?, 'local-file', ?, 'audio/wav', 4, 900)`,
		scopeID, records[0].ID, extraPath,
	)
	if err != nil {
		t.Fatalf("insert duplicate source asset: %v", err)
	}
	assetID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("duplicate source asset id: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO transcription_audio_assets (transcription_id, audio_asset_id, role)
		VALUES (?, ?, 'source')`, records[0].ID, assetID); err != nil {
		t.Fatalf("insert duplicate source link: %v", err)
	}

	records, err = s.ListTranscriptions(ctx, ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListTranscriptions after duplicate source link: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want one owner row despite duplicate source links", len(records))
	}
}

func TestSQLiteStorage3StoresLanguageBaseForHistoryFilters(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "storage3.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.SaveTranscription(ctx, "deutsch", "de-DE", "local", "", 1000, 10, nil); err != nil {
		t.Fatalf("SaveTranscription: %v", err)
	}
	if _, err := s.SaveQuickNote(ctx, "english", "en_US", "manual", 500, 5, nil); err != nil {
		t.Fatalf("SaveQuickNote: %v", err)
	}

	if !sqliteTestColumnExists(t, s.db, "transcriptions", "language_base") {
		t.Fatal("transcriptions.language_base column missing")
	}
	if !sqliteTestColumnExists(t, s.db, "quick_notes", "language_base") {
		t.Fatal("quick_notes.language_base column missing")
	}
	var transcriptionBase, noteBase string
	if err := s.db.QueryRow(`SELECT language_base FROM transcriptions LIMIT 1`).Scan(&transcriptionBase); err != nil {
		t.Fatalf("read transcription language_base: %v", err)
	}
	if err := s.db.QueryRow(`SELECT language_base FROM quick_notes LIMIT 1`).Scan(&noteBase); err != nil {
		t.Fatalf("read quick note language_base: %v", err)
	}
	if transcriptionBase != "de" {
		t.Fatalf("transcription language_base = %q, want de", transcriptionBase)
	}
	if noteBase != "en" {
		t.Fatalf("quick note language_base = %q, want en", noteBase)
	}

	transcriptions, err := s.ListTranscriptions(ctx, ListOpts{Language: "de", Limit: 10})
	if err != nil {
		t.Fatalf("ListTranscriptions language filter: %v", err)
	}
	if len(transcriptions) != 1 || transcriptions[0].Language != "de-DE" {
		t.Fatalf("filtered transcriptions = %+v", transcriptions)
	}
	notes, err := s.ListQuickNotes(ctx, ListOpts{Language: "en", Limit: 10})
	if err != nil {
		t.Fatalf("ListQuickNotes language filter: %v", err)
	}
	if len(notes) != 1 || notes[0].Language != "en_US" {
		t.Fatalf("filtered quick notes = %+v", notes)
	}
}

func TestSQLiteStorage3DerivesAudioFromAssetLinks(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(StoreConfig{
		SQLitePath:        filepath.Join(dir, "storage3.db"),
		SaveAudio:         true,
		MaxAudioStorageMB: 100,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	audio := []byte{0, 1, 2, 3, 4, 5}
	if err := s.SaveTranscription(ctx, "with audio", "en-US", "local", "", 1200, 20, audio); err != nil {
		t.Fatalf("SaveTranscription: %v", err)
	}
	noteID, err := s.SaveQuickNote(ctx, "note audio", "en-US", "manual", 800, 12, audio)
	if err != nil {
		t.Fatalf("SaveQuickNote: %v", err)
	}

	transcriptions, err := s.ListTranscriptions(ctx, ListOpts{Limit: 1})
	if err != nil {
		t.Fatalf("ListTranscriptions: %v", err)
	}
	if len(transcriptions) != 1 {
		t.Fatalf("len(transcriptions) = %d, want 1", len(transcriptions))
	}
	got := transcriptions[0]
	if got.AudioPath == "" || got.Audio == nil {
		t.Fatalf("derived transcription audio missing: %+v", got)
	}
	if got.Audio.SizeBytes != int64(len(audio)) {
		t.Fatalf("transcription audio size = %d, want %d", got.Audio.SizeBytes, len(audio))
	}
	if _, err := os.Stat(got.AudioPath); err != nil {
		t.Fatalf("stat derived transcription audio path: %v", err)
	}
	var ownerPath string
	if err := s.db.QueryRow(`SELECT COALESCE(audio_path, '') FROM transcriptions WHERE id = ?`, got.ID).Scan(&ownerPath); err != nil {
		t.Fatalf("read transcription owner audio_path: %v", err)
	}
	if ownerPath != "" {
		t.Fatalf("transcriptions.audio_path = %q, want empty derived compatibility field", ownerPath)
	}
	var linkCount int
	if err := s.db.QueryRow(`SELECT COUNT(*)
		FROM transcription_audio_assets link
		JOIN audio_assets asset ON asset.id = link.audio_asset_id
		WHERE link.transcription_id = ? AND asset.path = ?`, got.ID, got.AudioPath).Scan(&linkCount); err != nil {
		t.Fatalf("count transcription audio links: %v", err)
	}
	if linkCount != 1 {
		t.Fatalf("transcription audio links = %d, want 1", linkCount)
	}

	note, err := s.GetQuickNote(ctx, noteID)
	if err != nil {
		t.Fatalf("GetQuickNote: %v", err)
	}
	if note.AudioPath == "" || note.Audio == nil {
		t.Fatalf("derived quick note audio missing: %+v", note)
	}
	if err := s.db.QueryRow(`SELECT COALESCE(audio_path, '') FROM quick_notes WHERE id = ?`, note.ID).Scan(&ownerPath); err != nil {
		t.Fatalf("read quick note owner audio_path: %v", err)
	}
	if ownerPath != "" {
		t.Fatalf("quick_notes.audio_path = %q, want empty derived compatibility field", ownerPath)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*)
		FROM quick_note_audio_assets link
		JOIN audio_assets asset ON asset.id = link.audio_asset_id
		WHERE link.quick_note_id = ? AND asset.path = ?`, note.ID, note.AudioPath).Scan(&linkCount); err != nil {
		t.Fatalf("count quick note audio links: %v", err)
	}
	if linkCount != 1 {
		t.Fatalf("quick note audio links = %d, want 1", linkCount)
	}
}

func TestSQLiteStorage3RefreshesPerScopeStoreStats(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "storage3.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := speechstorage.WithScope(context.Background(), speechstorage.Scope{UserID: "alice"})
	if err := s.SaveTranscription(ctx, "one two", "en-US", "local", "", 60000, 100, nil); err != nil {
		t.Fatalf("SaveTranscription: %v", err)
	}
	noteID, err := s.SaveQuickNote(ctx, "three four five", "en-US", "manual", 30000, 50, nil)
	if err != nil {
		t.Fatalf("SaveQuickNote: %v", err)
	}
	if err := s.UpdateQuickNote(ctx, noteID, "three four"); err != nil {
		t.Fatalf("UpdateQuickNote: %v", err)
	}

	var transcriptions, quickNotes, totalWords int
	var totalDuration, totalLatency, latencyCount int64
	if err := s.db.QueryRow(`SELECT stats.transcriptions_count, stats.quick_notes_count, stats.total_words,
			stats.total_audio_duration_ms, stats.total_latency_ms, stats.latency_count
		FROM store_stats stats
		JOIN storage_scopes scopes ON scopes.id = stats.scope_id
		WHERE scopes.scope_key = ?`, "user:alice").Scan(&transcriptions, &quickNotes, &totalWords, &totalDuration, &totalLatency, &latencyCount); err != nil {
		t.Fatalf("read store_stats: %v", err)
	}
	if transcriptions != 1 || quickNotes != 1 || totalWords != 4 || totalDuration != 90000 || totalLatency != 150 || latencyCount != 2 {
		t.Fatalf("store_stats = transcriptions:%d quickNotes:%d words:%d duration:%d latency:%d latencyCount:%d",
			transcriptions, quickNotes, totalWords, totalDuration, totalLatency, latencyCount)
	}
	stats, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Transcriptions != 1 || stats.QuickNotes != 1 || stats.TotalWords != 4 || stats.TotalAudioDurationMs != 90000 || stats.AverageLatencyMs != 75 {
		t.Fatalf("Stats() = %+v", stats)
	}
}

func TestSQLiteStorage3IsolatesDictionaryUniquenessByScope(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "storage3.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctxA := speechstorage.WithScope(context.Background(), speechstorage.Scope{UserID: "alice"})
	ctxB := speechstorage.WithScope(context.Background(), speechstorage.Scope{UserID: "bob"})
	entry := []UserDictionaryEntry{{Spoken: "kombify", Canonical: "kombify"}}
	if err := s.ReplaceUserDictionaryEntries(ctxA, "de-DE", entry); err != nil {
		t.Fatalf("ReplaceUserDictionaryEntries alice: %v", err)
	}
	if err := s.ReplaceUserDictionaryEntries(ctxB, "de-DE", entry); err != nil {
		t.Fatalf("ReplaceUserDictionaryEntries bob: %v", err)
	}

	alice, err := s.ListUserDictionaryEntries(ctxA, "de")
	if err != nil {
		t.Fatalf("ListUserDictionaryEntries alice: %v", err)
	}
	bob, err := s.ListUserDictionaryEntries(ctxB, "de")
	if err != nil {
		t.Fatalf("ListUserDictionaryEntries bob: %v", err)
	}
	if len(alice) != 1 || len(bob) != 1 {
		t.Fatalf("dictionary entries alice=%+v bob=%+v, want one each", alice, bob)
	}

	var stored int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM user_dictionary_entries WHERE canonical = ?`, "kombify").Scan(&stored); err != nil {
		t.Fatalf("count dictionary rows: %v", err)
	}
	if stored != 2 {
		t.Fatalf("stored dictionary rows = %d, want 2 scoped rows", stored)
	}
}

func sqliteTestTableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		t.Fatalf("lookup table %s: %v", table, err)
	}
	return count > 0
}

func sqliteTestColumnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	exists, err := sqliteColumnExists(context.Background(), db, table, column)
	if err != nil {
		t.Fatalf("lookup column %s.%s: %v", table, column, err)
	}
	return exists
}
