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

//go:embed migrations/sqlite/002_quick_notes_pinned.sql
var sqliteMigration002 string

//go:embed migrations/sqlite/003_durations.sql
var sqliteMigration003 string

//go:embed migrations/sqlite/004_transcription_model.sql
var sqliteMigration004 string

//go:embed migrations/sqlite/005_user_dictionary.sql
var sqliteMigration005 string

//go:embed migrations/sqlite/006_voice_agent_sessions.sql
var sqliteMigration006 string

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

	if _, err := db.ExecContext(context.Background(), sqliteMigration001); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate 001: %w", err)
	}
	// Migration 002: add pinned column (safe to ignore "duplicate column" error)
	if _, err := db.ExecContext(context.Background(), sqliteMigration002); err != nil {
		errMsg := err.Error()
		if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
			slog.Warn("migrate 002", "err", err)
		}
	}
	if _, err := db.ExecContext(context.Background(), sqliteMigration003); err != nil {
		errMsg := err.Error()
		if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
			slog.Warn("migrate 003", "err", err)
		}
	}
	if _, err := db.ExecContext(context.Background(), sqliteMigration004); err != nil {
		errMsg := err.Error()
		if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
			slog.Warn("migrate 004", "err", err)
		}
	}
	if _, err := db.ExecContext(context.Background(), sqliteMigration005); err != nil {
		return nil, fmt.Errorf("migrate 005: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), sqliteMigration006); err != nil {
		return nil, fmt.Errorf("migrate 006: %w", err)
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

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO transcriptions (text, language, provider, model, duration_ms, latency_ms, audio_path) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		text, language, provider, model, durationMs, latencyMs, audioPath,
	)
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}

	if s.saveAudio && s.maxStorageMB > 0 {
		go s.enforceStorageLimit() //nolint:contextcheck,gosec // G118: maintenance goroutines must not be bound to request context
	}
	if s.saveAudio && s.audioRetentionDays > 0 {
		go s.enforceAudioRetention() //nolint:contextcheck,gosec // G118: maintenance goroutines must not be bound to request context
	}

	return nil
}

func (s *SQLiteStore) ListTranscriptions(ctx context.Context, opts ListOpts) ([]Transcription, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, text, language, provider, COALESCE(model, ''), COALESCE(duration_ms, 0), latency_ms, COALESCE(audio_path, ''), created_at
		 FROM transcriptions ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, opts.Offset,
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

func normalizeTranscriptionModelHints(hints map[string]string) map[string]string {
	if len(hints) == 0 {
		return nil
	}
	normalized := make(map[string]string, len(hints))
	for provider, model := range hints {
		provider = strings.TrimSpace(strings.ToLower(provider))
		model = strings.TrimSpace(model)
		if provider == "" || model == "" {
			continue
		}
		normalized[provider] = model
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
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
		`INSERT INTO quick_notes (text, language, provider, duration_ms, latency_ms, audio_path) VALUES (?, ?, ?, ?, ?, ?)`,
		text, language, provider, durationMs, latencyMs, audioPath,
	)
	if err != nil {
		return 0, fmt.Errorf("insert quick note: %w", err)
	}
	return result.LastInsertId()
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
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, text, language, provider, COALESCE(duration_ms, 0), latency_ms, COALESCE(audio_path, ''), COALESCE(pinned, 0), created_at, updated_at
		 FROM quick_notes ORDER BY pinned DESC, created_at DESC, id DESC LIMIT ? OFFSET ?`, limit, opts.Offset,
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
		`UPDATE quick_notes SET text = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		text, id,
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
		 SET text = ?, provider = ?, duration_ms = ?, latency_ms = ?, audio_path = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		text, provider, durationMs, latencyMs, nextAudioPath, id,
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
	}
	if s.saveAudio && s.maxStorageMB > 0 {
		go s.enforceStorageLimit() //nolint:contextcheck,gosec // G118: maintenance goroutines must not be bound to request context
	}
	if s.saveAudio && s.audioRetentionDays > 0 {
		go s.enforceAudioRetention() //nolint:contextcheck,gosec // G118: maintenance goroutines must not be bound to request context
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
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transcriptions`).Scan(&stats.Transcriptions); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quick_notes`).Scan(&stats.QuickNotes); err != nil {
		return Stats{}, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT text, COALESCE(duration_ms, 0), COALESCE(latency_ms, 0) FROM (
			SELECT text, duration_ms, latency_ms FROM transcriptions
			UNION ALL
			SELECT text, duration_ms, latency_ms FROM quick_notes
		)`,
	)
	if err != nil {
		return Stats{}, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	var (
		totalLatency int64
		latencyCount int64
	)
	for rows.Next() {
		var (
			text       string
			durationMs int64
			latencyMs  int64
		)
		if err := rows.Scan(&text, &durationMs, &latencyMs); err != nil {
			return Stats{}, err
		}
		stats.TotalWords += len(strings.Fields(text))
		stats.TotalAudioDurationMs += durationMs
		if latencyMs > 0 {
			totalLatency += latencyMs
			latencyCount++
		}
	}
	if err := rows.Err(); err != nil {
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

func buildLocalAudioAsset(path string, durationMs int64) *AudioAsset {
	if path == "" {
		return nil
	}

	asset := &AudioAsset{
		StorageKind: AudioStorageLocalFile,
		Path:        path,
		MimeType:    "audio/wav",
		DurationMs:  durationMs,
	}
	if info, err := os.Stat(path); err == nil {
		asset.SizeBytes = info.Size()
	}
	return asset
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
		`SELECT kind, id, audio_path FROM (
			SELECT 'transcription' AS kind, id, audio_path, created_at FROM transcriptions WHERE audio_path != ''
			UNION ALL
			SELECT 'quick_note' AS kind, id, audio_path, created_at FROM quick_notes WHERE audio_path != ''
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
		`SELECT kind, id, audio_path FROM (
			SELECT 'transcription' AS kind, id, audio_path, created_at FROM transcriptions WHERE audio_path != '' AND created_at < ?
			UNION ALL
			SELECT 'quick_note' AS kind, id, audio_path, created_at FROM quick_notes WHERE audio_path != '' AND created_at < ?
		)`,
		cutoff, cutoff,
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
	}
	if err := rows.Err(); err != nil {
		slog.Warn("store: retention rows error", "err", err)
	}

	if err := tx.Commit(); err != nil {
		slog.Warn("store: commit retention tx", "err", err)
	}
}
