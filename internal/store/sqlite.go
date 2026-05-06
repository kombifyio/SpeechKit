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

// SQLiteStore implements Store using a local SQLite database.
// Uses modernc.org/sqlite (pure Go, no CGo required).
type SQLiteStore struct {
	db                      *sql.DB
	path                    string
	maxStorageMB            int
	saveAudio               bool
	audioRetentionDays      int
	transcriptionModelHints map[string]string
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
	var audioPath string
	if strings.TrimSpace(model) == "" {
		model = s.transcriptionModelHint(provider)
	}

	if s.saveAudio && len(audioData) > 0 {
		audioDir := filepath.Join(filepath.Dir(s.path), "audio")
		if err := os.MkdirAll(audioDir, 0o700); err != nil {
			return fmt.Errorf("create audio dir: %w", err)
		}
		filename := fmt.Sprintf("%d.wav", time.Now().UnixNano())
		audioPath = filepath.Join(audioDir, filename)
		if err := os.WriteFile(audioPath, audioData, 0o600); err != nil {
			return fmt.Errorf("save audio: %w", err)
		}
	}

	result, err := s.db.ExecContext(ctx,
		`INSERT INTO transcriptions (text, language, provider, model, duration_ms, latency_ms, audio_path, word_count) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		text, language, provider, model, durationMs, latencyMs, audioPath, countWords(text),
	)
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if audioPath != "" {
		if err := recordAudioAsset(ctx, s.db, "sqlite", "transcription", id, audioPath, durationMs); err != nil {
			return fmt.Errorf("record audio asset: %w", err)
		}
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
	limit, offset := normalizedListPagination(opts)

	query := `SELECT id, text, language, provider, COALESCE(model, ''), COALESCE(duration_ms, 0), latency_ms, COALESCE(audio_path, ''), created_at
		 FROM transcriptions`
	args := make([]any, 0, 4)
	clauses := make([]string, 0, 2)
	clauses, args = appendSQLiteNormalizedLanguageFilter(clauses, args, opts.Language)
	if !opts.After.IsZero() {
		clauses = append(clauses, "created_at > ?")
		args = append(args, sqliteTime(opts.After))
	}
	query += buildWhereClause(clauses) // #nosec G202 -- audited choke-point; see SECURITY INVARIANT in query_filters.go.
	query += " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
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
		if err := rows.Scan(&t.ID, &t.Text, &t.Language, &t.Provider, &t.Model, &t.DurationMs, &t.LatencyMs, &t.AudioPath, &t.CreatedAt); err != nil {
			return nil, err
		}
		if strings.TrimSpace(t.Model) == "" {
			t.Model = s.transcriptionModelHint(t.Provider)
		}
		t.Audio = buildLocalAudioAsset(t.AudioPath, t.DurationMs)
		results = append(results, t)
	}
	return results, rows.Err()
}

func (s *SQLiteStore) GetTranscription(ctx context.Context, id int64) (*Transcription, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, text, language, provider, COALESCE(model, ''), COALESCE(duration_ms, 0), latency_ms, COALESCE(audio_path, ''), created_at
		 FROM transcriptions WHERE id = ?`, id,
	)

	var t Transcription
	if err := row.Scan(&t.ID, &t.Text, &t.Language, &t.Provider, &t.Model, &t.DurationMs, &t.LatencyMs, &t.AudioPath, &t.CreatedAt); err != nil {
		return nil, err
	}
	if strings.TrimSpace(t.Model) == "" {
		t.Model = s.transcriptionModelHint(t.Provider)
	}
	t.Audio = buildLocalAudioAsset(t.AudioPath, t.DurationMs)
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
		`DELETE FROM user_dictionary_entries WHERE language = ? AND source = ?`,
		language, userDictionarySettingsSource,
	); err != nil {
		return fmt.Errorf("clear user dictionary entries: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO user_dictionary_entries (spoken, canonical, language, source, enabled)
		 VALUES (?, ?, ?, ?, 1)
		 ON CONFLICT(spoken, canonical, language, source)
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
		if _, err = stmt.ExecContext(ctx, entry.Spoken, entry.Canonical, entry.Language, entry.Source); err != nil {
			return fmt.Errorf("insert user dictionary entry: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit user dictionary entries: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListUserDictionaryEntries(ctx context.Context, language string) ([]UserDictionaryEntry, error) {
	language = normalizeDictionaryLanguage(language)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, spoken, canonical, language, source, enabled, usage_count, created_at, updated_at
		 FROM user_dictionary_entries
		 WHERE enabled = 1 AND (? = '' OR language = ? OR language = '')
		 ORDER BY id ASC`,
		language, language,
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
	canonical = strings.TrimSpace(canonical)
	language = normalizeDictionaryLanguage(language)
	if canonical == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE user_dictionary_entries
		 SET usage_count = usage_count + 1, updated_at = CURRENT_TIMESTAMP
		 WHERE enabled = 1 AND lower(canonical) = lower(?) AND (? = '' OR language = ? OR language = '')`,
		canonical, language, language,
	)
	if err != nil {
		return fmt.Errorf("record user dictionary usage: %w", err)
	}
	return nil
}

func (s *SQLiteStore) TranscriptionCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transcriptions`).Scan(&count)
	return count, err
}

func (s *SQLiteStore) SaveQuickNote(ctx context.Context, text, language, provider string, durationMs, latencyMs int64, audioData []byte) (int64, error) {
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

	result, err := s.db.ExecContext(ctx,
		`INSERT INTO quick_notes (text, language, provider, duration_ms, latency_ms, audio_path, word_count) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		text, language, provider, durationMs, latencyMs, audioPath, countWords(text),
	)
	if err != nil {
		return 0, fmt.Errorf("insert quick note: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if audioPath != "" {
		if err := recordAudioAsset(ctx, s.db, "sqlite", "quick_note", id, audioPath, durationMs); err != nil {
			return 0, fmt.Errorf("record audio asset: %w", err)
		}
	}
	return id, nil
}

func (s *SQLiteStore) GetQuickNote(ctx context.Context, id int64) (*QuickNote, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, text, language, provider, COALESCE(duration_ms, 0), latency_ms, COALESCE(audio_path,''), pinned, created_at, updated_at
		 FROM quick_notes WHERE id = ?`, id)
	var n QuickNote
	var pinInt int
	if err := row.Scan(&n.ID, &n.Text, &n.Language, &n.Provider, &n.DurationMs, &n.LatencyMs, &n.AudioPath, &pinInt, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, err
	}
	n.Pinned = pinInt != 0
	n.Audio = buildLocalAudioAsset(n.AudioPath, n.DurationMs)
	return &n, nil
}

func (s *SQLiteStore) ListQuickNotes(ctx context.Context, opts ListOpts) ([]QuickNote, error) {
	limit, offset := normalizedListPagination(opts)

	query := `SELECT id, text, language, provider, COALESCE(duration_ms, 0), latency_ms, COALESCE(audio_path, ''), COALESCE(pinned, 0), created_at, updated_at
		 FROM quick_notes`
	args := make([]any, 0, 4)
	clauses := make([]string, 0, 2)
	clauses, args = appendSQLiteNormalizedLanguageFilter(clauses, args, opts.Language)
	if !opts.After.IsZero() {
		clauses = append(clauses, "created_at > ?")
		args = append(args, sqliteTime(opts.After))
	}
	query += buildWhereClause(clauses) // #nosec G202 -- audited choke-point; see SECURITY INVARIANT in query_filters.go.
	query += " ORDER BY pinned DESC, created_at DESC, id DESC LIMIT ? OFFSET ?"
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
		if err := rows.Scan(&n.ID, &n.Text, &n.Language, &n.Provider, &n.DurationMs, &n.LatencyMs, &n.AudioPath, &pinned, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		n.Pinned = pinned != 0
		n.Audio = buildLocalAudioAsset(n.AudioPath, n.DurationMs)
		results = append(results, n)
	}
	return results, rows.Err()
}

func (s *SQLiteStore) UpdateQuickNote(ctx context.Context, id int64, text string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE quick_notes SET text = ?, word_count = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		text, countWords(text), id,
	)
	if err != nil {
		return fmt.Errorf("update quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	return nil
}

func (s *SQLiteStore) UpdateQuickNoteCapture(ctx context.Context, id int64, text, provider string, durationMs, latencyMs int64, audioData []byte) error {
	var (
		currentAudioPath string
		nextAudioPath    string
	)

	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(audio_path, '') FROM quick_notes WHERE id = ?`, id).Scan(&currentAudioPath); err != nil {
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

	result, err := s.db.ExecContext(ctx,
		`UPDATE quick_notes
		 SET text = ?, provider = ?, duration_ms = ?, latency_ms = ?, audio_path = ?, word_count = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		text, provider, durationMs, latencyMs, nextAudioPath, countWords(text), id,
	)
	if err != nil {
		return fmt.Errorf("update quick note capture: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	if currentAudioPath != "" && currentAudioPath != nextAudioPath {
		_ = os.Remove(currentAudioPath)
		_ = deleteAudioAsset(ctx, s.db, "sqlite", "quick_note", id, currentAudioPath)
	}
	if nextAudioPath != "" && currentAudioPath != nextAudioPath {
		if err := recordAudioAsset(ctx, s.db, "sqlite", "quick_note", id, nextAudioPath, durationMs); err != nil {
			return fmt.Errorf("record audio asset: %w", err)
		}
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
	val := 0
	if pinned {
		val = 1
	}
	result, err := s.db.ExecContext(ctx, `UPDATE quick_notes SET pinned = ? WHERE id = ?`, val, id)
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
	var audioPath string
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(audio_path, '') FROM quick_notes WHERE id = ?`, id).Scan(&audioPath)

	result, err := s.db.ExecContext(ctx, `DELETE FROM quick_notes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}

	if audioPath != "" {
		_ = os.Remove(audioPath)
		_ = deleteAudioAssetsForOwner(ctx, s.db, "sqlite", "quick_note", id)
	}
	return nil
}

func (s *SQLiteStore) QuickNoteCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quick_notes`).Scan(&count)
	return count, err
}

func (s *SQLiteStore) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	var (
		totalLatency int64
		latencyCount int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM transcriptions),
			(SELECT COUNT(*) FROM quick_notes),
			COALESCE(SUM(word_count), 0),
			COALESCE(SUM(duration_ms), 0),
			COALESCE(SUM(CASE WHEN latency_ms > 0 THEN latency_ms ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN latency_ms > 0 THEN 1 ELSE 0 END), 0)
		FROM (
			SELECT word_count, duration_ms, latency_ms FROM transcriptions
			UNION ALL
			SELECT word_count, duration_ms, latency_ms FROM quick_notes
		)`,
	).Scan(&stats.Transcriptions, &stats.QuickNotes, &stats.TotalWords, &stats.TotalAudioDurationMs, &totalLatency, &latencyCount)
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
		`SELECT kind, id, path FROM (
			SELECT owner_kind AS kind, owner_id AS id, path, created_at FROM audio_assets WHERE path != ''
			UNION ALL
			SELECT 'transcription' AS kind, t.id, t.audio_path AS path, t.created_at FROM transcriptions t
			 WHERE t.audio_path != ''
			   AND NOT EXISTS (
			       SELECT 1 FROM audio_assets a
			       WHERE a.owner_kind = 'transcription' AND a.owner_id = t.id AND a.path = t.audio_path
			   )
			UNION ALL
			SELECT 'quick_note' AS kind, q.id, q.audio_path AS path, q.created_at FROM quick_notes q
			 WHERE q.audio_path != ''
			   AND NOT EXISTS (
			       SELECT 1 FROM audio_assets a
			       WHERE a.owner_kind = 'quick_note' AND a.owner_id = q.id AND a.path = q.audio_path
			   )
		) ORDER BY created_at ASC`,
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
		query := `UPDATE transcriptions SET audio_path = '' WHERE id = ?`
		if kind == "quick_note" {
			query = `UPDATE quick_notes SET audio_path = '' WHERE id = ?`
		}
		if _, err := tx.ExecContext(context.Background(), query, id); err != nil {
			slog.Warn("store: clear audio_path", "kind", kind, "id", id, "err", err)
		}
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
		`SELECT kind, id, path FROM (
			SELECT owner_kind AS kind, owner_id AS id, path, created_at FROM audio_assets WHERE path != '' AND created_at < ?
			UNION ALL
			SELECT 'transcription' AS kind, t.id, t.audio_path AS path, t.created_at FROM transcriptions t
			 WHERE t.audio_path != '' AND t.created_at < ?
			   AND NOT EXISTS (
			       SELECT 1 FROM audio_assets a
			       WHERE a.owner_kind = 'transcription' AND a.owner_id = t.id AND a.path = t.audio_path
			   )
			UNION ALL
			SELECT 'quick_note' AS kind, q.id, q.audio_path AS path, q.created_at FROM quick_notes q
			 WHERE q.audio_path != '' AND q.created_at < ?
			   AND NOT EXISTS (
			       SELECT 1 FROM audio_assets a
			       WHERE a.owner_kind = 'quick_note' AND a.owner_id = q.id AND a.path = q.audio_path
			   )
		)`,
		cutoff, cutoff, cutoff,
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
		query := `UPDATE transcriptions SET audio_path = '' WHERE id = ?`
		if kind == "quick_note" {
			query = `UPDATE quick_notes SET audio_path = '' WHERE id = ?`
		}
		if _, err := tx.ExecContext(context.Background(), query, id); err != nil {
			slog.Warn("store: clear retained audio_path", "kind", kind, "id", id, "err", err)
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
