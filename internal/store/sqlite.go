package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/sqlite/001_init.sql
var sqliteMigration001 string

//go:embed migrations/sqlite/002_quick_notes_pinned.sql
var sqliteMigration002 string

//go:embed migrations/sqlite/003_durations.sql
var sqliteMigration003 string

//go:embed migrations/sqlite/004_transcription_model.sql
var sqliteMigration004 string

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
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = "."
		}
		dbPath = filepath.Join(appData, "SpeechKit", "feedback.db")
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if _, err := db.Exec(sqliteMigration001); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate 001: %w", err)
	}
	// Migration 002: add pinned column (safe to ignore "duplicate column" error)
	if _, err := db.Exec(sqliteMigration002); err != nil {
		errMsg := err.Error()
		if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
			log.Printf("WARN: migrate 002: %v", err)
		}
	}
	if _, err := db.Exec(sqliteMigration003); err != nil {
		errMsg := err.Error()
		if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
			log.Printf("WARN: migrate 003: %v", err)
		}
	}
	if _, err := db.Exec(sqliteMigration004); err != nil {
		errMsg := err.Error()
		if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
			log.Printf("WARN: migrate 004: %v", err)
		}
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

func (s *SQLiteStore) SaveTranscription(_ context.Context, text, language, provider, model string, durationMs, latencyMs int64, audioData []byte) error {
	var audioPath string
	if strings.TrimSpace(model) == "" {
		model = s.transcriptionModelHint(provider)
	}

	if s.saveAudio && len(audioData) > 0 {
		audioDir := filepath.Join(filepath.Dir(s.path), "audio")
		if err := os.MkdirAll(audioDir, 0755); err != nil {
			return fmt.Errorf("create audio dir: %w", err)
		}
		filename := fmt.Sprintf("%d.wav", time.Now().UnixNano())
		audioPath = filepath.Join(audioDir, filename)
		if err := os.WriteFile(audioPath, audioData, 0600); err != nil {
			return fmt.Errorf("save audio: %w", err)
		}
	}

	_, err := s.db.Exec(
		`INSERT INTO transcriptions (text, language, provider, model, duration_ms, latency_ms, audio_path) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		text, language, provider, model, durationMs, latencyMs, audioPath,
	)
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}

	if s.saveAudio && s.maxStorageMB > 0 {
		go s.enforceStorageLimit()
	}
	if s.saveAudio && s.audioRetentionDays > 0 {
		go s.enforceAudioRetention()
	}

	return nil
}

func (s *SQLiteStore) ListTranscriptions(_ context.Context, opts ListOpts) ([]Transcription, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.Query(
		`SELECT id, text, language, provider, COALESCE(model, ''), COALESCE(duration_ms, 0), latency_ms, COALESCE(audio_path, ''), created_at
		 FROM transcriptions ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, opts.Offset,
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

func (s *SQLiteStore) GetTranscription(_ context.Context, id int64) (*Transcription, error) {
	row := s.db.QueryRow(
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

func (s *SQLiteStore) TranscriptionCount(_ context.Context) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM transcriptions`).Scan(&count)
	return count, err
}

func (s *SQLiteStore) SaveQuickNote(_ context.Context, text, language, provider string, durationMs, latencyMs int64, audioData []byte) (int64, error) {
	var audioPath string

	if s.saveAudio && len(audioData) > 0 {
		audioDir := filepath.Join(filepath.Dir(s.path), "audio")
		if err := os.MkdirAll(audioDir, 0755); err != nil {
			return 0, fmt.Errorf("create audio dir: %w", err)
		}
		filename := fmt.Sprintf("qn_%d.wav", time.Now().UnixNano())
		audioPath = filepath.Join(audioDir, filename)
		if err := os.WriteFile(audioPath, audioData, 0600); err != nil {
			return 0, fmt.Errorf("save audio: %w", err)
		}
	}

	result, err := s.db.Exec(
		`INSERT INTO quick_notes (text, language, provider, duration_ms, latency_ms, audio_path) VALUES (?, ?, ?, ?, ?, ?)`,
		text, language, provider, durationMs, latencyMs, audioPath,
	)
	if err != nil {
		return 0, fmt.Errorf("insert quick note: %w", err)
	}
	return result.LastInsertId()
}

func (s *SQLiteStore) GetQuickNote(_ context.Context, id int64) (*QuickNote, error) {
	row := s.db.QueryRow(
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

func (s *SQLiteStore) ListQuickNotes(_ context.Context, opts ListOpts) ([]QuickNote, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.Query(
		`SELECT id, text, language, provider, COALESCE(duration_ms, 0), latency_ms, COALESCE(audio_path, ''), COALESCE(pinned, 0), created_at, updated_at
		 FROM quick_notes ORDER BY pinned DESC, created_at DESC, id DESC LIMIT ? OFFSET ?`, limit, opts.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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

func (s *SQLiteStore) UpdateQuickNote(_ context.Context, id int64, text string) error {
	result, err := s.db.Exec(
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

func (s *SQLiteStore) UpdateQuickNoteCapture(_ context.Context, id int64, text, provider string, durationMs, latencyMs int64, audioData []byte) error {
	var (
		currentAudioPath string
		nextAudioPath    string
	)

	if err := s.db.QueryRow(`SELECT COALESCE(audio_path, '') FROM quick_notes WHERE id = ?`, id).Scan(&currentAudioPath); err != nil {
		return fmt.Errorf("lookup quick note %d: %w", id, err)
	}

	nextAudioPath = currentAudioPath
	if s.saveAudio && len(audioData) > 0 {
		audioDir := filepath.Join(filepath.Dir(s.path), "audio")
		if err := os.MkdirAll(audioDir, 0755); err != nil {
			return fmt.Errorf("create audio dir: %w", err)
		}
		filename := fmt.Sprintf("qn_%d.wav", time.Now().UnixNano())
		nextAudioPath = filepath.Join(audioDir, filename)
		if err := os.WriteFile(nextAudioPath, audioData, 0600); err != nil {
			return fmt.Errorf("save audio: %w", err)
		}
	}

	result, err := s.db.Exec(
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
		go s.enforceStorageLimit()
	}
	if s.saveAudio && s.audioRetentionDays > 0 {
		go s.enforceAudioRetention()
	}
	return nil
}

func (s *SQLiteStore) PinQuickNote(_ context.Context, id int64, pinned bool) error {
	val := 0
	if pinned {
		val = 1
	}
	result, err := s.db.Exec(`UPDATE quick_notes SET pinned = ? WHERE id = ?`, val, id)
	if err != nil {
		return fmt.Errorf("pin quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	return nil
}

func (s *SQLiteStore) DeleteQuickNote(_ context.Context, id int64) error {
	var audioPath string
	_ = s.db.QueryRow(`SELECT COALESCE(audio_path, '') FROM quick_notes WHERE id = ?`, id).Scan(&audioPath)

	result, err := s.db.Exec(`DELETE FROM quick_notes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}

	if audioPath != "" {
		os.Remove(audioPath)
	}
	return nil
}

func (s *SQLiteStore) QuickNoteCount(_ context.Context) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM quick_notes`).Scan(&count)
	return count, err
}

func (s *SQLiteStore) Stats(_ context.Context) (Stats, error) {
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
		)`,
	)
	if err != nil {
		return Stats{}, err
	}
	defer rows.Close()

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

	tx, err := s.db.Begin()
	if err != nil {
		log.Printf("store: begin cleanup tx: %v", err)
		return
	}
	defer tx.Rollback()

	rows, err := tx.Query(
		`SELECT kind, id, audio_path FROM (
			SELECT 'transcription' AS kind, id, audio_path, created_at FROM transcriptions WHERE audio_path != ''
			UNION ALL
			SELECT 'quick_note' AS kind, id, audio_path, created_at FROM quick_notes WHERE audio_path != ''
		) ORDER BY created_at ASC`,
	)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() && totalSize > limitBytes {
		var kind string
		var id int64
		var path string
		if err := rows.Scan(&kind, &id, &path); err != nil {
			log.Printf("store: scan cleanup row: %v", err)
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
			log.Printf("store: remove audio %s: %v", path, err)
			continue
		}
		totalSize -= info.Size()
		query := `UPDATE transcriptions SET audio_path = '' WHERE id = ?`
		if kind == "quick_note" {
			query = `UPDATE quick_notes SET audio_path = '' WHERE id = ?`
		}
		if _, err := tx.Exec(query, id); err != nil {
			log.Printf("store: clear %s audio_path for %d: %v", kind, id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("store: commit cleanup tx: %v", err)
	}
}

func (s *SQLiteStore) enforceAudioRetention() {
	if s.audioRetentionDays <= 0 {
		return
	}

	cutoff := time.Now().Add(-time.Duration(s.audioRetentionDays) * 24 * time.Hour).Format("2006-01-02 15:04:05")
	tx, err := s.db.Begin()
	if err != nil {
		log.Printf("store: begin retention tx: %v", err)
		return
	}
	defer tx.Rollback()

	rows, err := tx.Query(
		`SELECT kind, id, audio_path FROM (
			SELECT 'transcription' AS kind, id, audio_path, created_at FROM transcriptions WHERE audio_path != '' AND created_at < ?
			UNION ALL
			SELECT 'quick_note' AS kind, id, audio_path, created_at FROM quick_notes WHERE audio_path != '' AND created_at < ?
		)`,
		cutoff, cutoff,
	)
	if err != nil {
		log.Printf("store: query retention rows: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var kind string
		var id int64
		var path string
		if err := rows.Scan(&kind, &id, &path); err != nil {
			log.Printf("store: scan retention row: %v", err)
			continue
		}
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("store: remove retained audio %s: %v", path, err)
			continue
		}
		query := `UPDATE transcriptions SET audio_path = '' WHERE id = ?`
		if kind == "quick_note" {
			query = `UPDATE quick_notes SET audio_path = '' WHERE id = ?`
		}
		if _, err := tx.Exec(query, id); err != nil {
			log.Printf("store: clear retained %s audio_path for %d: %v", kind, id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("store: commit retention tx: %v", err)
	}
}
