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
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/postgres/001_init.sql
var postgresMigration001 string

// PostgresStore implements Store using PostgreSQL for metadata and the local
// filesystem for optional raw WAV persistence.
type PostgresStore struct {
	db                      *sql.DB
	audioDir                string
	maxStorageMB            int
	saveAudio               bool
	audioRetentionDays      int
	transcriptionModelHints map[string]string
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore creates a PostgreSQL-backed store.
func NewPostgresStore(cfg StoreConfig) (*PostgresStore, error) {
	if strings.TrimSpace(cfg.PostgresDSN) == "" {
		return nil, fmt.Errorf("postgres backend requires a DSN (set store.postgres_dsn in config.toml)")
	}

	db, err := sql.Open("pgx", cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if _, err := db.Exec(postgresMigration001); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate postgres: %w", err)
	}

	store := &PostgresStore{
		db:                      db,
		audioDir:                defaultAudioDir(),
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

func (s *PostgresStore) SaveTranscription(_ context.Context, text, language, provider, model string, durationMs, latencyMs int64, audioData []byte) error {
	audioPath, err := s.persistAudio(audioData, "", durationMs)
	if err != nil {
		return err
	}
	if strings.TrimSpace(model) == "" {
		model = s.transcriptionModelHint(provider)
	}

	_, err = s.db.Exec(
		`INSERT INTO transcriptions (text, language, provider, model, duration_ms, latency_ms, audio_path)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		text, language, provider, model, durationMs, latencyMs, audioPath,
	)
	if err != nil {
		return fmt.Errorf("insert transcription: %w", err)
	}

	s.scheduleMaintenance()
	return nil
}

func (s *PostgresStore) GetTranscription(_ context.Context, id int64) (*Transcription, error) {
	row := s.db.QueryRow(
		`SELECT id, text, language, provider, COALESCE(model, ''), COALESCE(duration_ms, 0), COALESCE(latency_ms, 0), COALESCE(audio_path, ''), created_at
		 FROM transcriptions WHERE id = $1`,
		id,
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

func (s *PostgresStore) ListTranscriptions(_ context.Context, opts ListOpts) ([]Transcription, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.Query(
		`SELECT id, text, language, provider, COALESCE(model, ''), COALESCE(duration_ms, 0), COALESCE(latency_ms, 0), COALESCE(audio_path, ''), created_at
		 FROM transcriptions
		 ORDER BY created_at DESC, id DESC
		 LIMIT $1 OFFSET $2`,
		limit, opts.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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

func (s *PostgresStore) TranscriptionCount(_ context.Context) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM transcriptions`).Scan(&count)
	return count, err
}

func (s *PostgresStore) SaveQuickNote(_ context.Context, text, language, provider string, durationMs, latencyMs int64, audioData []byte) (int64, error) {
	audioPath, err := s.persistAudio(audioData, "qn_", durationMs)
	if err != nil {
		return 0, err
	}

	var id int64
	err = s.db.QueryRow(
		`INSERT INTO quick_notes (text, language, provider, duration_ms, latency_ms, audio_path)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id`,
		text, language, provider, durationMs, latencyMs, audioPath,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert quick note: %w", err)
	}

	s.scheduleMaintenance()
	return id, nil
}

func (s *PostgresStore) GetQuickNote(_ context.Context, id int64) (*QuickNote, error) {
	row := s.db.QueryRow(
		`SELECT id, text, language, provider, COALESCE(duration_ms, 0), COALESCE(latency_ms, 0), COALESCE(audio_path, ''), pinned, created_at, updated_at
		 FROM quick_notes WHERE id = $1`,
		id,
	)

	var n QuickNote
	if err := row.Scan(&n.ID, &n.Text, &n.Language, &n.Provider, &n.DurationMs, &n.LatencyMs, &n.AudioPath, &n.Pinned, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, err
	}
	n.Audio = buildLocalAudioAsset(n.AudioPath, n.DurationMs)
	return &n, nil
}

func (s *PostgresStore) ListQuickNotes(_ context.Context, opts ListOpts) ([]QuickNote, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.Query(
		`SELECT id, text, language, provider, COALESCE(duration_ms, 0), COALESCE(latency_ms, 0), COALESCE(audio_path, ''), pinned, created_at, updated_at
		 FROM quick_notes
		 ORDER BY pinned DESC, created_at DESC, id DESC
		 LIMIT $1 OFFSET $2`,
		limit, opts.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]QuickNote, 0)
	for rows.Next() {
		var n QuickNote
		if err := rows.Scan(&n.ID, &n.Text, &n.Language, &n.Provider, &n.DurationMs, &n.LatencyMs, &n.AudioPath, &n.Pinned, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		n.Audio = buildLocalAudioAsset(n.AudioPath, n.DurationMs)
		results = append(results, n)
	}
	return results, rows.Err()
}

func (s *PostgresStore) UpdateQuickNote(_ context.Context, id int64, text string) error {
	result, err := s.db.Exec(`UPDATE quick_notes SET text = $1, updated_at = NOW() WHERE id = $2`, text, id)
	if err != nil {
		return fmt.Errorf("update quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	return nil
}

func (s *PostgresStore) UpdateQuickNoteCapture(_ context.Context, id int64, text, provider string, durationMs, latencyMs int64, audioData []byte) error {
	var currentAudioPath string
	if err := s.db.QueryRow(`SELECT COALESCE(audio_path, '') FROM quick_notes WHERE id = $1`, id).Scan(&currentAudioPath); err != nil {
		return fmt.Errorf("lookup quick note %d: %w", id, err)
	}

	nextAudioPath, err := s.persistAudio(audioData, "qn_", durationMs)
	if err != nil {
		return err
	}
	if nextAudioPath == "" {
		nextAudioPath = currentAudioPath
	}

	result, err := s.db.Exec(
		`UPDATE quick_notes
		 SET text = $1, provider = $2, duration_ms = $3, latency_ms = $4, audio_path = $5, updated_at = NOW()
		 WHERE id = $6`,
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

	s.scheduleMaintenance()
	return nil
}

func (s *PostgresStore) PinQuickNote(_ context.Context, id int64, pinned bool) error {
	result, err := s.db.Exec(`UPDATE quick_notes SET pinned = $1 WHERE id = $2`, pinned, id)
	if err != nil {
		return fmt.Errorf("pin quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	return nil
}

func (s *PostgresStore) DeleteQuickNote(_ context.Context, id int64) error {
	var audioPath string
	_ = s.db.QueryRow(`SELECT COALESCE(audio_path, '') FROM quick_notes WHERE id = $1`, id).Scan(&audioPath)

	result, err := s.db.Exec(`DELETE FROM quick_notes WHERE id = $1`, id)
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

func (s *PostgresStore) QuickNoteCount(_ context.Context) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM quick_notes`).Scan(&count)
	return count, err
}

func (s *PostgresStore) Stats(_ context.Context) (Stats, error) {
	var stats Stats
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM transcriptions`).Scan(&stats.Transcriptions); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM quick_notes`).Scan(&stats.QuickNotes); err != nil {
		return Stats{}, err
	}

	rows, err := s.db.Query(
		`SELECT text, COALESCE(duration_ms, 0), COALESCE(latency_ms, 0) FROM (
			SELECT text, duration_ms, latency_ms FROM transcriptions
			UNION ALL
			SELECT text, duration_ms, latency_ms FROM quick_notes
		) AS combined`,
	)
	if err != nil {
		return Stats{}, err
	}
	defer rows.Close()

	var totalLatency int64
	var latencyCount int64
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

func (s *PostgresStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *PostgresStore) SemanticCapabilities(context.Context) SemanticCapabilities {
	return SemanticCapabilities{
		Provider:     SemanticProviderNone,
		FullText:     false,
		Embeddings:   false,
		VectorSearch: false,
	}
}

func (s *PostgresStore) scheduleMaintenance() {
	if s.saveAudio && s.maxStorageMB > 0 {
		go s.enforceStorageLimit()
	}
	if s.saveAudio && s.audioRetentionDays > 0 {
		go s.enforceAudioRetention()
	}
}

func (s *PostgresStore) persistAudio(audioData []byte, prefix string, _ int64) (string, error) {
	if !s.saveAudio || len(audioData) == 0 {
		return "", nil
	}
	if err := os.MkdirAll(s.audioDir, 0755); err != nil {
		return "", fmt.Errorf("create audio dir: %w", err)
	}
	filename := fmt.Sprintf("%s%d.wav", prefix, time.Now().UnixNano())
	audioPath := filepath.Join(s.audioDir, filename)
	if err := os.WriteFile(audioPath, audioData, 0600); err != nil {
		return "", fmt.Errorf("save audio: %w", err)
	}
	return audioPath, nil
}

func (s *PostgresStore) transcriptionModelHint(provider string) string {
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

func (s *PostgresStore) enforceStorageLimit() {
	s.enforceAudioRetention()

	entries, err := os.ReadDir(s.audioDir)
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

	tx, err := s.db.Begin()
	if err != nil {
		slog.Warn("store: begin postgres cleanup tx", "err", err)
		return
	}
	defer tx.Rollback()

	rows, err := tx.Query(
		`SELECT kind, id, audio_path FROM (
			SELECT 'transcription' AS kind, id, audio_path, created_at FROM transcriptions WHERE audio_path <> ''
			UNION ALL
			SELECT 'quick_note' AS kind, id, audio_path, created_at FROM quick_notes WHERE audio_path <> ''
		) AS assets
		ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() && totalSize > limitBytes {
		var (
			kind string
			id   int64
			path string
		)
		if err := rows.Scan(&kind, &id, &path); err != nil {
			slog.Warn("store: scan postgres cleanup row", "err", err)
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
			slog.Warn("store: remove postgres audio", "path", path, "err", err)
			continue
		}
		totalSize -= info.Size()
		query := `UPDATE transcriptions SET audio_path = '' WHERE id = $1`
		if kind == "quick_note" {
			query = `UPDATE quick_notes SET audio_path = '' WHERE id = $1`
		}
		if _, err := tx.Exec(query, id); err != nil {
			slog.Warn("store: clear postgres audio_path", "kind", kind, "id", id, "err", err)
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Warn("store: commit postgres cleanup tx", "err", err)
	}
}

func (s *PostgresStore) enforceAudioRetention() {
	if s.audioRetentionDays <= 0 {
		return
	}

	cutoff := time.Now().Add(-time.Duration(s.audioRetentionDays) * 24 * time.Hour)
	tx, err := s.db.Begin()
	if err != nil {
		slog.Warn("store: begin postgres retention tx", "err", err)
		return
	}
	defer tx.Rollback()

	rows, err := tx.Query(
		`SELECT kind, id, audio_path FROM (
			SELECT 'transcription' AS kind, id, audio_path, created_at FROM transcriptions WHERE audio_path <> '' AND created_at < $1
			UNION ALL
			SELECT 'quick_note' AS kind, id, audio_path, created_at FROM quick_notes WHERE audio_path <> '' AND created_at < $1
		) AS assets`,
		cutoff,
	)
	if err != nil {
		slog.Warn("store: query postgres retention rows", "err", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var (
			kind string
			id   int64
			path string
		)
		if err := rows.Scan(&kind, &id, &path); err != nil {
			slog.Warn("store: scan postgres retention row", "err", err)
			continue
		}
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Warn("store: remove retained postgres audio", "path", path, "err", err)
			continue
		}
		query := `UPDATE transcriptions SET audio_path = '' WHERE id = $1`
		if kind == "quick_note" {
			query = `UPDATE quick_notes SET audio_path = '' WHERE id = $1`
		}
		if _, err := tx.Exec(query, id); err != nil {
			slog.Warn("store: clear retained postgres audio_path", "kind", kind, "id", id, "err", err)
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Warn("store: commit postgres retention tx", "err", err)
	}
}

func defaultAudioDir() string {
	return filepath.Join(runtimepath.DataDir(), "audio")
}
