package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/runtimepath"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
	_ "modernc.org/sqlite" // register modernc sqlite as database/sql driver
)

//go:embed migrations/sqlite/001_init.sql
var sqliteMigration001 string

//go:embed migrations/sqlite/005_user_dictionary.sql
var sqliteMigration005 string

//go:embed migrations/sqlite/006_voice_agent_sessions.sql
var sqliteMigration006 string

//go:embed migrations/sqlite/007_personas.sql
var sqliteMigration007 string

//go:embed migrations/sqlite/010_voice_agent_normalized.sql
var sqliteMigration010 string

//go:embed migrations/sqlite/011_audio_assets.sql
var sqliteMigration011 string

//go:embed migrations/sqlite/012_indexes.sql
var sqliteMigration012 string

//go:embed migrations/sqlite/013_storage_scopes.sql
var sqliteMigration013 string

//go:embed migrations/sqlite/014_storage_v3_model.sql
var sqliteMigration014 string

// SQLiteStore implements Store using a local SQLite database.
// Uses modernc.org/sqlite (pure Go, no CGo required).
type SQLiteStore struct {
	db                      *sql.DB
	path                    string
	maxStorageMB            int
	saveAudio               bool
	audioRetentionDays      int
	transcriptionModelHints map[string]string
	defaultScope            speechstorage.Scope
	scopePolicy             speechstorage.ScopePolicy
}

// Compile-time interface check.
var _ Store = (*SQLiteStore)(nil)

// NewSQLiteStore opens or creates a SQLite feedback database.
func NewSQLiteStore(cfg StoreConfig) (*SQLiteStore, error) {
	dbPath := cfg.SQLitePath
	if dbPath == "" {
		dbPath = filepath.Join(runtimepath.DataDir(), "feedback.db")
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}

	if err := runSQLiteMigrations(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}

	store := &SQLiteStore{
		db:                      db,
		path:                    dbPath,
		maxStorageMB:            cfg.MaxAudioStorageMB,
		saveAudio:               cfg.SaveAudio,
		audioRetentionDays:      cfg.AudioRetentionDays,
		transcriptionModelHints: normalizeTranscriptionModelHints(cfg.TranscriptionModelHints),
		defaultScope:            speechstorage.NormalizeScope(cfg.DefaultScope),
		scopePolicy:             normalizedScopePolicy(cfg.ScopePolicy),
	}
	if store.saveAudio && store.audioRetentionDays > 0 {
		store.enforceAudioRetention()
	}
	if store.saveAudio && store.maxStorageMB > 0 {
		store.enforceStorageLimit()
	}
	return store, nil
}

// DB exposes the underlying *sql.DB so adjacent packages can build their
// own table-scoped persisters (e.g. the persona catalog in
// internal/server/persona) without the base Store interface needing to
// enumerate every optional capability. Callers must treat the returned
// handle as read-mostly: it is owned by the Store and must not be closed.
func (s *SQLiteStore) DB() *sql.DB { return s.db }

func (s *SQLiteStore) SaveTranscription(ctx context.Context, text, language, provider, model string, durationMs, latencyMs int64, audioData []byte) error {
	return s.SaveTranscriptionWithAudio(ctx, text, language, provider, model, durationMs, latencyMs, audioAssetInputFromBytes(audioData))
}

func (s *SQLiteStore) SaveTranscriptionWithAudio(ctx context.Context, text, language, provider, model string, durationMs, latencyMs int64, audio AudioAssetInput) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	var audioPath string
	audio = normalizeAudioAssetInput(audio)
	if audio.DurationMs > 0 {
		durationMs = audio.DurationMs
	}
	if strings.TrimSpace(model) == "" {
		model = s.transcriptionModelHint(provider)
	}
	owner, _ := RecordOwnerFromContext(ctx)

	if s.saveAudio && len(audio.Data) > 0 {
		audioDir := filepath.Join(filepath.Dir(s.path), "audio")
		if err := os.MkdirAll(audioDir, 0o700); err != nil {
			return fmt.Errorf("create audio dir: %w", err)
		}
		filename := fmt.Sprintf("%d%s", time.Now().UnixNano(), audio.Extension)
		audioPath = filepath.Join(audioDir, filename)
		if err := os.WriteFile(audioPath, audio.Data, 0o600); err != nil {
			return fmt.Errorf("save audio: %w", err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	result, err := tx.ExecContext(ctx,
		`INSERT INTO transcriptions (scope_id, text, language, language_base, provider, model, duration_ms, latency_ms, word_count, owner_user_id, owner_org_id, owner_source)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		scopeID, text, language, normalizeDictionaryLanguage(language), provider, model, durationMs, latencyMs, countWords(text), owner.UserID, owner.OrgID, owner.Source,
	)
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if audioPath != "" {
		if err := recordScopedAudioAsset(ctx, tx, "sqlite", scopeID, "transcription", id, audioPath, durationMs); err != nil {
			return fmt.Errorf("record audio asset: %w", err)
		}
	}
	if err := refreshSQLiteStoreStats(ctx, tx, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transcription: %w", err)
	}

	if s.saveAudio && s.maxStorageMB > 0 {
		//nolint:contextcheck // maintenance cleanup must not be cancelled with the request.
		go s.enforceStorageLimit() // #nosec G118 -- bounded retention cleanup, not request-scoped work.
	}
	if s.saveAudio && s.audioRetentionDays > 0 {
		//nolint:contextcheck // maintenance cleanup must not be cancelled with the request.
		go s.enforceAudioRetention() // #nosec G118 -- bounded retention cleanup, not request-scoped work.
	}

	return nil
}

func (s *SQLiteStore) ListTranscriptions(ctx context.Context, opts ListOpts) ([]Transcription, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	limit, offset := normalizedListPagination(opts)

	query := `SELECT t.id, t.text, t.language, t.provider, COALESCE(t.model, ''), COALESCE(t.duration_ms, 0), t.latency_ms,
			COALESCE(a.path, ''), COALESCE(a.storage_kind, ''), COALESCE(a.mime_type, ''), COALESCE(a.size_bytes, 0), COALESCE(a.duration_ms, 0),
			t.created_at, COALESCE(t.owner_user_id, ''), COALESCE(t.owner_org_id, ''), COALESCE(t.owner_source, '')
		 FROM transcriptions t
		 LEFT JOIN audio_assets a ON a.id = (
			SELECT link.audio_asset_id
			FROM transcription_audio_assets link
			JOIN audio_assets asset ON asset.id = link.audio_asset_id
			WHERE link.transcription_id = t.id AND link.role = 'source'
			ORDER BY link.created_at DESC, link.audio_asset_id DESC
			LIMIT 1
		 )`
	args := []any{scopeID}
	clauses := []string{"t.scope_id = ?"}
	clauses, args = appendSQLiteNormalizedLanguageFilter(clauses, args, opts.Language)
	clauses, args = appendSQLiteOwnerFilter(clauses, args, opts)
	if !opts.After.IsZero() {
		clauses = append(clauses, "t.created_at > ?")
		args = append(args, sqliteTime(opts.After))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ") // #nosec G202 -- clauses are fixed internal snippets; values are parameterized.
	}
	query += " ORDER BY t.created_at DESC, t.id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx,
		query, args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	results := make([]Transcription, 0)
	for rows.Next() {
		var t Transcription
		var audioStorageKind, audioMimeType string
		var audioSizeBytes, audioDurationMs int64
		if err := rows.Scan(&t.ID, &t.Text, &t.Language, &t.Provider, &t.Model, &t.DurationMs, &t.LatencyMs,
			&t.AudioPath, &audioStorageKind, &audioMimeType, &audioSizeBytes, &audioDurationMs, &t.CreatedAt,
			&t.OwnerUserID, &t.OwnerOrgID, &t.OwnerSource); err != nil {
			return nil, err
		}
		if strings.TrimSpace(t.Model) == "" {
			t.Model = s.transcriptionModelHint(t.Provider)
		}
		t.Audio = buildAudioAsset(audioStorageKind, t.AudioPath, audioMimeType, audioSizeBytes, audioDurationMs)
		results = append(results, t)
	}
	return results, rows.Err()
}

func (s *SQLiteStore) GetTranscription(ctx context.Context, id int64) (*Transcription, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT t.id, t.text, t.language, t.provider, COALESCE(t.model, ''), COALESCE(t.duration_ms, 0), t.latency_ms,
			COALESCE(a.path, ''), COALESCE(a.storage_kind, ''), COALESCE(a.mime_type, ''), COALESCE(a.size_bytes, 0), COALESCE(a.duration_ms, 0),
			t.created_at, COALESCE(t.owner_user_id, ''), COALESCE(t.owner_org_id, ''), COALESCE(t.owner_source, '')
		 FROM transcriptions t
		 LEFT JOIN audio_assets a ON a.id = (
			SELECT link.audio_asset_id
			FROM transcription_audio_assets link
			JOIN audio_assets asset ON asset.id = link.audio_asset_id
			WHERE link.transcription_id = t.id AND link.role = 'source'
			ORDER BY link.created_at DESC, link.audio_asset_id DESC
			LIMIT 1
		 )
		 WHERE t.id = ? AND t.scope_id = ?`, id, scopeID,
	)

	var t Transcription
	var audioStorageKind, audioMimeType string
	var audioSizeBytes, audioDurationMs int64
	if err := row.Scan(&t.ID, &t.Text, &t.Language, &t.Provider, &t.Model, &t.DurationMs, &t.LatencyMs,
		&t.AudioPath, &audioStorageKind, &audioMimeType, &audioSizeBytes, &audioDurationMs, &t.CreatedAt,
		&t.OwnerUserID, &t.OwnerOrgID, &t.OwnerSource); err != nil {
		return nil, err
	}
	if strings.TrimSpace(t.Model) == "" {
		t.Model = s.transcriptionModelHint(t.Provider)
	}
	t.Audio = buildAudioAsset(audioStorageKind, t.AudioPath, audioMimeType, audioSizeBytes, audioDurationMs)
	return &t, nil
}

func (s *SQLiteStore) transcriptionModelHint(provider string) string {
	if len(s.transcriptionModelHints) == 0 {
		return ""
	}
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		return ""
	}
	if model := s.transcriptionModelHints[provider]; model != "" {
		return model
	}
	switch provider {
	case "hf":
		return s.transcriptionModelHints["huggingface"]
	case "huggingface":
		return s.transcriptionModelHints["hf"]
	default:
		return ""
	}
}

func (s *SQLiteStore) ReplaceUserDictionaryEntries(ctx context.Context, language string, entries []UserDictionaryEntry) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	language = normalizeDictionaryLanguage(language)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx,
		`DELETE FROM user_dictionary_entries WHERE scope_id = ? AND language = ? AND source = ?`,
		scopeID, language, userDictionarySettingsSource,
	); err != nil {
		return fmt.Errorf("clear user dictionary entries: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO user_dictionary_entries (scope_id, spoken, canonical, language, source, enabled)
		 VALUES (?, ?, ?, ?, ?, 1)
		 ON CONFLICT(scope_id, spoken, canonical, language, source)
		 DO UPDATE SET enabled = 1, updated_at = CURRENT_TIMESTAMP`,
	)
	if err != nil {
		return fmt.Errorf("prepare user dictionary insert: %w", err)
	}
	defer stmt.Close() //nolint:errcheck // statement close during transaction cleanup

	for _, entry := range entries {
		entry, ok := normalizeUserDictionaryEntry(entry, language)
		if !ok {
			continue
		}
		if _, err = stmt.ExecContext(ctx, scopeID, entry.Spoken, entry.Canonical, entry.Language, entry.Source); err != nil {
			return fmt.Errorf("insert user dictionary entry: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit user dictionary entries: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListUserDictionaryEntries(ctx context.Context, language string) ([]UserDictionaryEntry, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	language = normalizeDictionaryLanguage(language)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, spoken, canonical, language, source, enabled, usage_count, created_at, updated_at
		 FROM user_dictionary_entries
		 WHERE scope_id = ? AND enabled = 1 AND (? = '' OR language = ? OR language = '')
		 ORDER BY id ASC`,
		scopeID, language, language,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	entries := make([]UserDictionaryEntry, 0)
	for rows.Next() {
		var entry UserDictionaryEntry
		var enabledInt int
		if err := rows.Scan(
			&entry.ID,
			&entry.Spoken,
			&entry.Canonical,
			&entry.Language,
			&entry.Source,
			&enabledInt,
			&entry.UsageCount,
			&entry.CreatedAt,
			&entry.UpdatedAt,
		); err != nil {
			return nil, err
		}
		entry.Enabled = enabledInt != 0
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *SQLiteStore) RecordUserDictionaryUsage(ctx context.Context, canonical, language string) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	canonical = strings.TrimSpace(canonical)
	language = normalizeDictionaryLanguage(language)
	if canonical == "" {
		return nil
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE user_dictionary_entries
		 SET usage_count = usage_count + 1, updated_at = CURRENT_TIMESTAMP
		 WHERE scope_id = ? AND enabled = 1 AND lower(canonical) = lower(?) AND (? = '' OR language = ? OR language = '')`,
		scopeID, canonical, language, language,
	)
	if err != nil {
		return fmt.Errorf("record user dictionary usage: %w", err)
	}
	return nil
}

func (s *SQLiteStore) TranscriptionCount(ctx context.Context) (int, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	var count int
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transcriptions WHERE scope_id = ?`, scopeID).Scan(&count)
	return count, err
}

func (s *SQLiteStore) SaveQuickNote(ctx context.Context, text, language, provider string, durationMs, latencyMs int64, audioData []byte) (int64, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	var audioPath string

	if s.saveAudio && len(audioData) > 0 {
		audioDir := filepath.Join(filepath.Dir(s.path), "audio")
		if err := os.MkdirAll(audioDir, 0o700); err != nil {
			return 0, fmt.Errorf("create audio dir: %w", err)
		}
		filename := fmt.Sprintf("qn_%d.wav", time.Now().UnixNano())
		audioPath = filepath.Join(audioDir, filename)
		if err := os.WriteFile(audioPath, audioData, 0o600); err != nil {
			return 0, fmt.Errorf("save audio: %w", err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	result, err := tx.ExecContext(ctx,
		`INSERT INTO quick_notes (scope_id, text, language, language_base, provider, duration_ms, latency_ms, word_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		scopeID, text, language, normalizeDictionaryLanguage(language), provider, durationMs, latencyMs, countWords(text),
	)
	if err != nil {
		return 0, fmt.Errorf("insert quick note: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if audioPath != "" {
		if err := recordScopedAudioAsset(ctx, tx, "sqlite", scopeID, "quick_note", id, audioPath, durationMs); err != nil {
			return 0, fmt.Errorf("record audio asset: %w", err)
		}
	}
	if err := refreshSQLiteStoreStats(ctx, tx, scopeID); err != nil {
		return 0, fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit quick note: %w", err)
	}
	return id, nil
}

func (s *SQLiteStore) GetQuickNote(ctx context.Context, id int64) (*QuickNote, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT q.id, q.text, q.language, q.provider, COALESCE(q.duration_ms, 0), q.latency_ms,
			COALESCE(a.path, ''), COALESCE(a.storage_kind, ''), COALESCE(a.mime_type, ''), COALESCE(a.size_bytes, 0), COALESCE(a.duration_ms, 0),
			q.pinned, q.created_at, q.updated_at
		 FROM quick_notes q
		 LEFT JOIN audio_assets a ON a.id = (
			SELECT link.audio_asset_id
			FROM quick_note_audio_assets link
			JOIN audio_assets asset ON asset.id = link.audio_asset_id
			WHERE link.quick_note_id = q.id AND link.role = 'source'
			ORDER BY link.created_at DESC, link.audio_asset_id DESC
			LIMIT 1
		 )
		 WHERE q.id = ? AND q.scope_id = ?`, id, scopeID)
	var n QuickNote
	var pinInt int
	var audioStorageKind, audioMimeType string
	var audioSizeBytes, audioDurationMs int64
	if err := row.Scan(&n.ID, &n.Text, &n.Language, &n.Provider, &n.DurationMs, &n.LatencyMs,
		&n.AudioPath, &audioStorageKind, &audioMimeType, &audioSizeBytes, &audioDurationMs, &pinInt, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, err
	}
	n.Pinned = pinInt != 0
	n.Audio = buildAudioAsset(audioStorageKind, n.AudioPath, audioMimeType, audioSizeBytes, audioDurationMs)
	return &n, nil
}

func (s *SQLiteStore) ListQuickNotes(ctx context.Context, opts ListOpts) ([]QuickNote, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	limit, offset := normalizedListPagination(opts)

	query := `SELECT q.id, q.text, q.language, q.provider, COALESCE(q.duration_ms, 0), q.latency_ms,
			COALESCE(a.path, ''), COALESCE(a.storage_kind, ''), COALESCE(a.mime_type, ''), COALESCE(a.size_bytes, 0), COALESCE(a.duration_ms, 0),
			COALESCE(q.pinned, 0), q.created_at, q.updated_at
		 FROM quick_notes q
		 LEFT JOIN audio_assets a ON a.id = (
			SELECT link.audio_asset_id
			FROM quick_note_audio_assets link
			JOIN audio_assets asset ON asset.id = link.audio_asset_id
			WHERE link.quick_note_id = q.id AND link.role = 'source'
			ORDER BY link.created_at DESC, link.audio_asset_id DESC
			LIMIT 1
		 )`
	args := []any{scopeID}
	clauses := []string{"q.scope_id = ?"}
	clauses, args = appendSQLiteNormalizedLanguageFilter(clauses, args, opts.Language)
	if !opts.After.IsZero() {
		clauses = append(clauses, "q.created_at > ?")
		args = append(args, sqliteTime(opts.After))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ") // #nosec G202 -- clauses are fixed internal snippets; values are parameterized.
	}
	query += " ORDER BY q.pinned DESC, q.created_at DESC, q.id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx,
		query, args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	results := make([]QuickNote, 0)
	for rows.Next() {
		var n QuickNote
		var pinned int
		var audioStorageKind, audioMimeType string
		var audioSizeBytes, audioDurationMs int64
		if err := rows.Scan(&n.ID, &n.Text, &n.Language, &n.Provider, &n.DurationMs, &n.LatencyMs,
			&n.AudioPath, &audioStorageKind, &audioMimeType, &audioSizeBytes, &audioDurationMs, &pinned, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		n.Pinned = pinned != 0
		n.Audio = buildAudioAsset(audioStorageKind, n.AudioPath, audioMimeType, audioSizeBytes, audioDurationMs)
		results = append(results, n)
	}
	return results, rows.Err()
}

func (s *SQLiteStore) UpdateQuickNote(ctx context.Context, id int64, text string) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	result, err := tx.ExecContext(ctx,
		`UPDATE quick_notes SET text = ?, word_count = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND scope_id = ?`,
		text, countWords(text), id, scopeID,
	)
	if err != nil {
		return fmt.Errorf("update quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	if err := refreshSQLiteStoreStats(ctx, tx, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit quick note update: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateQuickNoteCapture(ctx context.Context, id int64, text, provider string, durationMs, latencyMs int64, audioData []byte) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	var (
		currentAudioPath string
		nextAudioPath    string
	)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(a.path, '')
		FROM quick_notes q
		LEFT JOIN audio_assets a ON a.id = (
			SELECT link.audio_asset_id
			FROM quick_note_audio_assets link
			JOIN audio_assets asset ON asset.id = link.audio_asset_id
			WHERE link.quick_note_id = q.id AND link.role = 'source'
			ORDER BY link.created_at DESC, link.audio_asset_id DESC
			LIMIT 1
		)
		WHERE q.id = ? AND q.scope_id = ?
		LIMIT 1`, id, scopeID).Scan(&currentAudioPath); err != nil {
		return fmt.Errorf("lookup quick note %d: %w", id, err)
	}

	nextAudioPath = currentAudioPath
	if s.saveAudio && len(audioData) > 0 {
		audioDir := filepath.Join(filepath.Dir(s.path), "audio")
		if err := os.MkdirAll(audioDir, 0o700); err != nil {
			return fmt.Errorf("create audio dir: %w", err)
		}
		filename := fmt.Sprintf("qn_%d.wav", time.Now().UnixNano())
		nextAudioPath = filepath.Join(audioDir, filename)
		if err := os.WriteFile(nextAudioPath, audioData, 0o600); err != nil {
			return fmt.Errorf("save audio: %w", err)
		}
	}
	createdNewAudio := nextAudioPath != "" && currentAudioPath != nextAudioPath
	defer func() {
		if !committed && createdNewAudio {
			_ = os.Remove(nextAudioPath)
		}
	}()

	result, err := tx.ExecContext(ctx,
		`UPDATE quick_notes
		 SET text = ?, provider = ?, duration_ms = ?, latency_ms = ?, word_count = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND scope_id = ?`,
		text, provider, durationMs, latencyMs, countWords(text), id, scopeID,
	)
	if err != nil {
		return fmt.Errorf("update quick note capture: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	if currentAudioPath != "" && currentAudioPath != nextAudioPath {
		if err := deleteAudioAsset(ctx, tx, "sqlite", "quick_note", id, currentAudioPath); err != nil {
			return fmt.Errorf("delete previous audio asset: %w", err)
		}
	}
	if nextAudioPath != "" && currentAudioPath != nextAudioPath {
		if err := recordScopedAudioAsset(ctx, tx, "sqlite", scopeID, "quick_note", id, nextAudioPath, durationMs); err != nil {
			return fmt.Errorf("record audio asset: %w", err)
		}
	}
	if err := refreshSQLiteStoreStats(ctx, tx, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit quick note capture: %w", err)
	}
	committed = true
	if currentAudioPath != "" && currentAudioPath != nextAudioPath {
		_ = os.Remove(currentAudioPath)
	}
	if s.saveAudio && s.maxStorageMB > 0 {
		//nolint:contextcheck // maintenance cleanup must not be cancelled with the request.
		go s.enforceStorageLimit() // #nosec G118 -- bounded retention cleanup, not request-scoped work.
	}
	if s.saveAudio && s.audioRetentionDays > 0 {
		//nolint:contextcheck // maintenance cleanup must not be cancelled with the request.
		go s.enforceAudioRetention() // #nosec G118 -- bounded retention cleanup, not request-scoped work.
	}
	return nil
}

func (s *SQLiteStore) PinQuickNote(ctx context.Context, id int64, pinned bool) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	val := 0
	if pinned {
		val = 1
	}
	result, err := s.db.ExecContext(ctx, `UPDATE quick_notes SET pinned = ? WHERE id = ? AND scope_id = ?`, val, id, scopeID)
	if err != nil {
		return fmt.Errorf("pin quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	return nil
}

func (s *SQLiteStore) DeleteQuickNote(ctx context.Context, id int64) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	var audioPath string
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(a.path, '')
		FROM quick_notes q
		LEFT JOIN audio_assets a ON a.id = (
			SELECT link.audio_asset_id
			FROM quick_note_audio_assets link
			JOIN audio_assets asset ON asset.id = link.audio_asset_id
			WHERE link.quick_note_id = q.id AND link.role = 'source'
			ORDER BY link.created_at DESC, link.audio_asset_id DESC
			LIMIT 1
		)
		WHERE q.id = ? AND q.scope_id = ?
		LIMIT 1`, id, scopeID).Scan(&audioPath)

	if audioPath != "" {
		if err := deleteAudioAssetsForOwner(ctx, tx, "sqlite", "quick_note", id); err != nil {
			return fmt.Errorf("delete quick note audio assets: %w", err)
		}
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM quick_notes WHERE id = ? AND scope_id = ?`, id, scopeID)
	if err != nil {
		return fmt.Errorf("delete quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}

	if err := refreshSQLiteStoreStats(ctx, tx, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit quick note delete: %w", err)
	}
	committed = true
	if audioPath != "" {
		_ = os.Remove(audioPath)
	}
	return nil
}

func (s *SQLiteStore) QuickNoteCount(ctx context.Context) (int, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	var count int
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quick_notes WHERE scope_id = ?`, scopeID).Scan(&count)
	return count, err
}

func (s *SQLiteStore) Stats(ctx context.Context) (Stats, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return Stats{}, err
	}
	var stats Stats
	var (
		totalLatency int64
		latencyCount int64
	)
	err = s.db.QueryRowContext(ctx, `SELECT transcriptions_count, quick_notes_count, total_words,
		total_audio_duration_ms, total_latency_ms, latency_count
		FROM store_stats WHERE scope_id = ?`, scopeID).
		Scan(&stats.Transcriptions, &stats.QuickNotes, &stats.TotalWords, &stats.TotalAudioDurationMs, &totalLatency, &latencyCount)
	if err != nil {
		return Stats{}, err
	}
	if stats.TotalAudioDurationMs > 0 {
		stats.AverageWordsPerMinute = float64(stats.TotalWords) / (float64(stats.TotalAudioDurationMs) / float64(time.Minute/time.Millisecond))
	}
	if latencyCount > 0 {
		stats.AverageLatencyMs = totalLatency / latencyCount
	}
	return stats, nil
}

func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *SQLiteStore) SemanticCapabilities(context.Context) SemanticCapabilities {
	return SemanticCapabilities{
		Provider:     SemanticProviderNone,
		FullText:     false,
		Embeddings:   false,
		VectorSearch: false,
	}
}

func (s *SQLiteStore) enforceStorageLimit() {
	s.enforceAudioRetention()

	audioDir := filepath.Join(filepath.Dir(s.path), "audio")
	entries, err := os.ReadDir(audioDir)
	if err != nil {
		return
	}

	var totalSize int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		totalSize += info.Size()
	}

	limitBytes := int64(s.maxStorageMB) * 1024 * 1024
	if totalSize <= limitBytes {
		return
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		slog.Warn("store: begin cleanup tx", "err", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback, error not actionable

	rows, err := tx.QueryContext(context.Background(),
		`SELECT owner_kind AS kind, owner_id AS id, path
		 FROM audio_assets
		 WHERE path != ''
		 ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	for rows.Next() && totalSize > limitBytes {
		var kind string
		var id int64
		var path string
		if err := rows.Scan(&kind, &id, &path); err != nil {
			slog.Warn("store: scan cleanup row", "err", err)
			continue
		}
		if path == "" {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		if err := os.Remove(path); err != nil {
			slog.Warn("store: remove audio", "path", path, "err", err)
			continue
		}
		totalSize -= info.Size()
		if err := deleteAudioAsset(context.Background(), tx, "sqlite", kind, id, path); err != nil {
			slog.Warn("store: delete audio asset", "kind", kind, "id", id, "err", err)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Warn("store: cleanup rows error", "err", err)
	}

	if err := tx.Commit(); err != nil {
		slog.Warn("store: commit cleanup tx", "err", err)
	}
}

func (s *SQLiteStore) enforceAudioRetention() {
	if s.audioRetentionDays <= 0 {
		return
	}

	cutoff := time.Now().Add(-time.Duration(s.audioRetentionDays) * 24 * time.Hour).Format("2006-01-02 15:04:05")
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		slog.Warn("store: begin retention tx", "err", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback, error not actionable

	rows, err := tx.QueryContext(context.Background(),
		`SELECT owner_kind AS kind, owner_id AS id, path
		 FROM audio_assets
		 WHERE path != '' AND created_at < ?
		 ORDER BY created_at ASC, id ASC`,
		cutoff,
	)
	if err != nil {
		slog.Warn("store: query retention rows", "err", err)
		return
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	for rows.Next() {
		var kind string
		var id int64
		var path string
		if err := rows.Scan(&kind, &id, &path); err != nil {
			slog.Warn("store: scan retention row", "err", err)
			continue
		}
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Warn("store: remove retained audio", "path", path, "err", err)
			continue
		}
		if err := deleteAudioAsset(context.Background(), tx, "sqlite", kind, id, path); err != nil {
			slog.Warn("store: delete retained audio asset", "kind", kind, "id", id, "err", err)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Warn("store: retention rows error", "err", err)
	}

	if err := tx.Commit(); err != nil {
		slog.Warn("store: commit retention tx", "err", err)
	}
}
