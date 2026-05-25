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

	_ "github.com/jackc/pgx/v5/stdlib" // register pgx as database/sql driver
	"github.com/kombifyio/SpeechKit/internal/runtimepath"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

//go:embed migrations/postgres/001_init.sql
var postgresMigration001 string

//go:embed migrations/postgres/002_voice_agent_sessions.sql
var postgresMigration002 string

//go:embed migrations/postgres/003_personas.sql
var postgresMigration003 string

//go:embed migrations/postgres/004_word_counts.sql
var postgresMigration004 string

//go:embed migrations/postgres/005_voice_agent_normalized.sql
var postgresMigration005 string

//go:embed migrations/postgres/006_audio_assets.sql
var postgresMigration006 string

//go:embed migrations/postgres/007_indexes.sql
var postgresMigration007 string

//go:embed migrations/postgres/008_storage_scopes.sql
var postgresMigration008 string

//go:embed migrations/postgres/009_storage_v3_model.sql
var postgresMigration009 string

//go:embed migrations/postgres/010_wakeword_activations.sql
var postgresMigration010 string

// PostgresStore implements Store using PostgreSQL for metadata and the local
// filesystem for optional raw WAV persistence.
type PostgresStore struct {
	db                      *sql.DB
	audioDir                string
	maxStorageMB            int
	saveAudio               bool
	audioRetentionDays      int
	transcriptionModelHints map[string]string
	defaultScope            speechstorage.Scope
	scopePolicy             speechstorage.ScopePolicy
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
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := runPostgresMigrations(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}

	store := &PostgresStore{
		db:                      db,
		audioDir:                defaultAudioDir(),
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

// DB exposes the underlying *sql.DB for adjacent table-scoped persisters.
// The caller must not close the returned handle.
func (s *PostgresStore) DB() *sql.DB { return s.db }

func (s *PostgresStore) SaveTranscription(ctx context.Context, text, language, provider, model string, durationMs, latencyMs int64, audioData []byte) error {
	return s.SaveTranscriptionWithAudio(ctx, text, language, provider, model, durationMs, latencyMs, audioAssetInputFromBytes(audioData))
}

func (s *PostgresStore) SaveTranscriptionWithAudio(ctx context.Context, text, language, provider, model string, durationMs, latencyMs int64, audio AudioAssetInput) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("postgres: resolve scope: %w", err)
	}
	audio = normalizeAudioAssetInput(audio)
	if audio.DurationMs > 0 {
		durationMs = audio.DurationMs
	}
	audioPath, err := s.persistAudio(audio, "")
	if err != nil {
		return fmt.Errorf("postgres: persist transcription audio: %w", err)
	}
	if strings.TrimSpace(model) == "" {
		model = s.transcriptionModelHint(provider)
	}
	owner, _ := RecordOwnerFromContext(ctx)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO transcriptions (scope_id, text, language, language_base, provider, model, duration_ms, latency_ms, word_count, owner_user_id, owner_org_id, owner_source)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 RETURNING id`,
		scopeID, text, language, normalizeDictionaryLanguage(language), provider, model, durationMs, latencyMs,
		countWords(text), owner.UserID, owner.OrgID, owner.Source,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("insert transcription: %w", err)
	}
	if audioPath != "" {
		if err := recordScopedAudioAsset(ctx, tx, "postgres", scopeID, "transcription", id, audioPath, durationMs); err != nil {
			return fmt.Errorf("record audio asset: %w", err)
		}
	}
	if err := refreshPostgresStoreStats(ctx, tx, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transcription: %w", err)
	}

	s.scheduleMaintenance() //nolint:contextcheck // maintenance goroutines must not be bound to request context
	return nil
}

func (s *PostgresStore) GetTranscription(ctx context.Context, id int64) (*Transcription, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT t.id, t.text, t.language, t.provider, COALESCE(t.model, ''), COALESCE(t.duration_ms, 0), COALESCE(t.latency_ms, 0),
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
		 WHERE t.id = $1 AND t.scope_id = $2`,
		id, scopeID,
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

func (s *PostgresStore) ListTranscriptions(ctx context.Context, opts ListOpts) ([]Transcription, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	limit, offset := normalizedListPagination(opts)

	query := `SELECT t.id, t.text, t.language, t.provider, COALESCE(t.model, ''), COALESCE(t.duration_ms, 0), COALESCE(t.latency_ms, 0),
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
	clauses := []string{"t.scope_id = $1"}
	clauses, args = appendPostgresNormalizedLanguageFilter(clauses, args, opts.Language)
	clauses, args = appendPostgresOwnerFilter(clauses, args, opts)
	if !opts.After.IsZero() {
		args = append(args, opts.After.UTC())
		clauses = append(clauses, fmt.Sprintf("t.created_at > $%d", len(args)))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ") // #nosec G202 -- clauses are fixed internal snippets; values are parameterized.
	}
	args = append(args, limit)
	limitPos := len(args)
	args = append(args, offset)
	offsetPos := len(args)
	query += fmt.Sprintf(" ORDER BY t.created_at DESC, t.id DESC LIMIT $%d OFFSET $%d", limitPos, offsetPos) // #nosec G202 -- limit/offset positions are derived from argument indexes.

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

func (s *PostgresStore) ReplaceUserDictionaryEntries(ctx context.Context, language string, entries []UserDictionaryEntry) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("postgres: resolve scope: %w", err)
	}
	language = normalizeDictionaryLanguage(language)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx,
		`DELETE FROM user_dictionary_entries WHERE scope_id = $1 AND language = $2 AND source = $3`,
		scopeID, language, userDictionarySettingsSource,
	); err != nil {
		return fmt.Errorf("clear user dictionary entries: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO user_dictionary_entries (scope_id, spoken, canonical, language, source, enabled)
		 VALUES ($1, $2, $3, $4, $5, TRUE)
		 ON CONFLICT(scope_id, spoken, canonical, language, source)
		 DO UPDATE SET enabled = TRUE, updated_at = NOW()`,
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

func (s *PostgresStore) ListUserDictionaryEntries(ctx context.Context, language string) ([]UserDictionaryEntry, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	language = normalizeDictionaryLanguage(language)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, spoken, canonical, language, source, enabled, usage_count, created_at, updated_at
		 FROM user_dictionary_entries
		 WHERE scope_id = $1 AND enabled = TRUE AND ($2 = '' OR language = $3 OR language = '')
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
		if err := rows.Scan(
			&entry.ID,
			&entry.Spoken,
			&entry.Canonical,
			&entry.Language,
			&entry.Source,
			&entry.Enabled,
			&entry.UsageCount,
			&entry.CreatedAt,
			&entry.UpdatedAt,
		); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *PostgresStore) RecordUserDictionaryUsage(ctx context.Context, canonical, language string) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("postgres: resolve scope: %w", err)
	}
	canonical = strings.TrimSpace(canonical)
	language = normalizeDictionaryLanguage(language)
	if canonical == "" {
		return nil
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE user_dictionary_entries
		 SET usage_count = usage_count + 1, updated_at = NOW()
		 WHERE scope_id = $1 AND enabled = TRUE AND lower(canonical) = lower($2) AND ($3 = '' OR language = $4 OR language = '')`,
		scopeID, canonical, language, language,
	)
	if err != nil {
		return fmt.Errorf("record user dictionary usage: %w", err)
	}
	return nil
}

func (s *PostgresStore) TranscriptionCount(ctx context.Context) (int, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	var count int
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transcriptions WHERE scope_id = $1`, scopeID).Scan(&count)
	return count, err
}

func (s *PostgresStore) SaveQuickNote(ctx context.Context, text, language, provider string, durationMs, latencyMs int64, audioData []byte) (int64, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	audioPath, err := s.persistAudio(audioAssetInputFromBytes(audioData), "qn_")
	if err != nil {
		return 0, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO quick_notes (scope_id, text, language, language_base, provider, duration_ms, latency_ms, word_count)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id`,
		scopeID, text, language, normalizeDictionaryLanguage(language), provider, durationMs, latencyMs, countWords(text),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert quick note: %w", err)
	}
	if audioPath != "" {
		if err := recordScopedAudioAsset(ctx, tx, "postgres", scopeID, "quick_note", id, audioPath, durationMs); err != nil {
			return 0, fmt.Errorf("record audio asset: %w", err)
		}
	}
	if err := refreshPostgresStoreStats(ctx, tx, scopeID); err != nil {
		return 0, fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit quick note: %w", err)
	}

	s.scheduleMaintenance() //nolint:contextcheck // maintenance goroutines must not be bound to request context
	return id, nil
}

func (s *PostgresStore) GetQuickNote(ctx context.Context, id int64) (*QuickNote, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT q.id, q.text, q.language, q.provider, COALESCE(q.duration_ms, 0), COALESCE(q.latency_ms, 0),
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
		 WHERE q.id = $1 AND q.scope_id = $2`,
		id, scopeID,
	)

	var n QuickNote
	var audioStorageKind, audioMimeType string
	var audioSizeBytes, audioDurationMs int64
	if err := row.Scan(&n.ID, &n.Text, &n.Language, &n.Provider, &n.DurationMs, &n.LatencyMs,
		&n.AudioPath, &audioStorageKind, &audioMimeType, &audioSizeBytes, &audioDurationMs, &n.Pinned, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, err
	}
	n.Audio = buildAudioAsset(audioStorageKind, n.AudioPath, audioMimeType, audioSizeBytes, audioDurationMs)
	return &n, nil
}

func (s *PostgresStore) ListQuickNotes(ctx context.Context, opts ListOpts) ([]QuickNote, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	limit, offset := normalizedListPagination(opts)

	query := `SELECT q.id, q.text, q.language, q.provider, COALESCE(q.duration_ms, 0), COALESCE(q.latency_ms, 0),
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
		 )`
	args := []any{scopeID}
	clauses := []string{"q.scope_id = $1"}
	clauses, args = appendPostgresNormalizedLanguageFilter(clauses, args, opts.Language)
	if !opts.After.IsZero() {
		args = append(args, opts.After.UTC())
		clauses = append(clauses, fmt.Sprintf("q.created_at > $%d", len(args)))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ") // #nosec G202 -- clauses are fixed internal snippets; values are parameterized.
	}
	args = append(args, limit)
	limitPos := len(args)
	args = append(args, offset)
	offsetPos := len(args)
	query += fmt.Sprintf(" ORDER BY q.pinned DESC, q.created_at DESC, q.id DESC LIMIT $%d OFFSET $%d", limitPos, offsetPos) // #nosec G202 -- limit/offset positions are derived from argument indexes.

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
		var audioStorageKind, audioMimeType string
		var audioSizeBytes, audioDurationMs int64
		if err := rows.Scan(&n.ID, &n.Text, &n.Language, &n.Provider, &n.DurationMs, &n.LatencyMs,
			&n.AudioPath, &audioStorageKind, &audioMimeType, &audioSizeBytes, &audioDurationMs, &n.Pinned, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		n.Audio = buildAudioAsset(audioStorageKind, n.AudioPath, audioMimeType, audioSizeBytes, audioDurationMs)
		results = append(results, n)
	}
	return results, rows.Err()
}

func (s *PostgresStore) UpdateQuickNote(ctx context.Context, id int64, text string) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("postgres: resolve scope: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	result, err := tx.ExecContext(ctx, `UPDATE quick_notes SET text = $1, word_count = $2, updated_at = NOW() WHERE id = $3 AND scope_id = $4`, text, countWords(text), id, scopeID)
	if err != nil {
		return fmt.Errorf("update quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	if err := refreshPostgresStoreStats(ctx, tx, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit quick note update: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpdateQuickNoteCapture(ctx context.Context, id int64, text, provider string, durationMs, latencyMs int64, audioData []byte) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("postgres: resolve scope: %w", err)
	}
	var currentAudioPath string
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: begin tx: %w", err)
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
		WHERE q.id = $1 AND q.scope_id = $2
		LIMIT 1`, id, scopeID).Scan(&currentAudioPath); err != nil {
		return fmt.Errorf("lookup quick note %d: %w", id, err)
	}

	nextAudioPath, err := s.persistAudio(audioAssetInputFromBytes(audioData), "qn_")
	if err != nil {
		return fmt.Errorf("postgres: persist quicknote audio: %w", err)
	}
	if nextAudioPath == "" {
		nextAudioPath = currentAudioPath
	}
	createdNewAudio := nextAudioPath != "" && currentAudioPath != nextAudioPath
	defer func() {
		if !committed && createdNewAudio {
			_ = os.Remove(nextAudioPath)
		}
	}()

	result, err := tx.ExecContext(ctx,
		`UPDATE quick_notes
		 SET text = $1, provider = $2, duration_ms = $3, latency_ms = $4, word_count = $5, updated_at = NOW()
		 WHERE id = $6 AND scope_id = $7`,
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
		if err := deleteAudioAsset(ctx, tx, "postgres", "quick_note", id, currentAudioPath); err != nil {
			return fmt.Errorf("delete previous audio asset: %w", err)
		}
	}
	if nextAudioPath != "" && currentAudioPath != nextAudioPath {
		if err := recordScopedAudioAsset(ctx, tx, "postgres", scopeID, "quick_note", id, nextAudioPath, durationMs); err != nil {
			return fmt.Errorf("record audio asset: %w", err)
		}
	}
	if err := refreshPostgresStoreStats(ctx, tx, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit quick note capture: %w", err)
	}
	committed = true
	if currentAudioPath != "" && currentAudioPath != nextAudioPath {
		_ = os.Remove(currentAudioPath)
	}

	s.scheduleMaintenance() //nolint:contextcheck // maintenance goroutines must not be bound to request context
	return nil
}

func (s *PostgresStore) PinQuickNote(ctx context.Context, id int64, pinned bool) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("postgres: resolve scope: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE quick_notes SET pinned = $1 WHERE id = $2 AND scope_id = $3`, pinned, id, scopeID)
	if err != nil {
		return fmt.Errorf("pin quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	return nil
}

func (s *PostgresStore) DeleteQuickNote(ctx context.Context, id int64) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("postgres: resolve scope: %w", err)
	}
	var audioPath string
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: begin tx: %w", err)
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
		WHERE q.id = $1 AND q.scope_id = $2
		LIMIT 1`, id, scopeID).Scan(&audioPath)

	if audioPath != "" {
		if err := deleteAudioAssetsForOwner(ctx, tx, "postgres", "quick_note", id); err != nil {
			return fmt.Errorf("delete quick note audio assets: %w", err)
		}
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM quick_notes WHERE id = $1 AND scope_id = $2`, id, scopeID)
	if err != nil {
		return fmt.Errorf("delete quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	if err := refreshPostgresStoreStats(ctx, tx, scopeID); err != nil {
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

func (s *PostgresStore) QuickNoteCount(ctx context.Context) (int, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	var count int
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quick_notes WHERE scope_id = $1`, scopeID).Scan(&count)
	return count, err
}

func (s *PostgresStore) Stats(ctx context.Context) (Stats, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return Stats{}, err
	}
	var stats Stats
	var totalLatency int64
	var latencyCount int64
	err = s.db.QueryRowContext(ctx, `SELECT transcriptions_count, quick_notes_count, total_words,
		total_audio_duration_ms, total_latency_ms, latency_count
		FROM store_stats WHERE scope_id = $1`, scopeID).
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

func (s *PostgresStore) persistAudio(audio AudioAssetInput, prefix string) (string, error) {
	audio = normalizeAudioAssetInput(audio)
	if !s.saveAudio || len(audio.Data) == 0 {
		return "", nil
	}
	if err := os.MkdirAll(s.audioDir, 0o700); err != nil {
		return "", fmt.Errorf("create audio dir: %w", err)
	}
	filename := fmt.Sprintf("%s%d%s", prefix, time.Now().UnixNano(), audio.Extension)
	audioPath := filepath.Join(s.audioDir, filename)
	if err := os.WriteFile(audioPath, audio.Data, 0o600); err != nil {
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

	tx, err := s.db.BeginTx(context.Background(), nil) //nolint:contextcheck // background goroutine should not be bound to request context
	if err != nil {
		slog.Warn("store: begin postgres cleanup tx", "err", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback, error not actionable

	rows, err := tx.QueryContext(context.Background(), //nolint:contextcheck // background goroutine should not be bound to request context
		`SELECT owner_kind AS kind, owner_id AS id, path
		 FROM audio_assets
		 WHERE path <> ''
		 ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

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
		if err := deleteAudioAsset(context.Background(), tx, "postgres", kind, id, path); err != nil { //nolint:contextcheck // background goroutine should not be bound to request context
			slog.Warn("store: delete postgres audio asset", "kind", kind, "id", id, "err", err)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Warn("store: postgres cleanup rows error", "err", err)
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
	tx, err := s.db.BeginTx(context.Background(), nil) //nolint:contextcheck // background goroutine should not be bound to request context
	if err != nil {
		slog.Warn("store: begin postgres retention tx", "err", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback, error not actionable

	rows, err := tx.QueryContext(context.Background(), //nolint:contextcheck // background goroutine should not be bound to request context
		`SELECT owner_kind AS kind, owner_id AS id, path
		 FROM audio_assets
		 WHERE path <> '' AND created_at < $1
		 ORDER BY created_at ASC, id ASC`,
		cutoff,
	)
	if err != nil {
		slog.Warn("store: query postgres retention rows", "err", err)
		return
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

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
		if err := deleteAudioAsset(context.Background(), tx, "postgres", kind, id, path); err != nil { //nolint:contextcheck // background goroutine should not be bound to request context
			slog.Warn("store: delete retained postgres audio asset", "kind", kind, "id", id, "err", err)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Warn("store: postgres retention rows error", "err", err)
	}

	if err := tx.Commit(); err != nil {
		slog.Warn("store: commit postgres retention tx", "err", err)
	}
}

func defaultAudioDir() string {
	return filepath.Join(runtimepath.DataDir(), "audio")
}
