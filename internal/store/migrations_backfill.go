package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func countWords(text string) int {
	return len(strings.Fields(text))
}

func backfillSQLiteWordCounts(ctx context.Context, db *sql.DB) error {
	if err := backfillWordCounts(ctx, db, "sqlite", "transcriptions"); err != nil {
		return err
	}
	return backfillWordCounts(ctx, db, "sqlite", "quick_notes")
}

func backfillPostgresWordCounts(ctx context.Context, db *sql.DB) error {
	if err := backfillWordCounts(ctx, db, "postgres", "transcriptions"); err != nil {
		return err
	}
	return backfillWordCounts(ctx, db, "postgres", "quick_notes")
}

func backfillWordCounts(ctx context.Context, db *sql.DB, dialect, table string) error {
	selectQuery, updateQuery, err := wordCountBackfillQueries(dialect, table)
	if err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, selectQuery)
	if err != nil {
		return err
	}
	type row struct {
		id   int64
		text string
	}
	var pending []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.text); err != nil {
			_ = rows.Close()
			return err
		}
		pending = append(pending, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range pending {
		if _, err := db.ExecContext(ctx, updateQuery, countWords(item.text), item.id); err != nil {
			return err
		}
	}
	return nil
}

func wordCountBackfillQueries(dialect, table string) (string, string, error) {
	var selectQuery string
	var updateSQLite string
	var updatePostgres string
	switch table {
	case "transcriptions":
		selectQuery = `SELECT id, text FROM transcriptions WHERE word_count = 0 AND TRIM(text) != ''`
		updateSQLite = `UPDATE transcriptions SET word_count = ? WHERE id = ?`
		updatePostgres = `UPDATE transcriptions SET word_count = $1 WHERE id = $2`
	case "quick_notes":
		selectQuery = `SELECT id, text FROM quick_notes WHERE word_count = 0 AND TRIM(text) != ''`
		updateSQLite = `UPDATE quick_notes SET word_count = ? WHERE id = ?`
		updatePostgres = `UPDATE quick_notes SET word_count = $1 WHERE id = $2`
	default:
		return "", "", fmt.Errorf("unsupported word_count backfill table %q", table)
	}
	if dialect == "postgres" {
		return selectQuery, updatePostgres, nil
	}
	return selectQuery, updateSQLite, nil
}

//nolint:rowserrcheck // scanVoiceAgentSessions checks rows.Err() after consuming rows.
func backfillSQLiteVoiceAgentNormalized(ctx context.Context, db *sql.DB) error {
	hasTurnsJSON, err := sqliteColumnExists(ctx, db, "voice_agent_sessions", "turns_json")
	if err != nil {
		return err
	}
	if !hasTurnsJSON {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id, title, summary, raw_summary, transcript, language, provider_profile_id, runtime_kind,
		turns_json, ideas_json, decisions_json, open_questions_json, next_steps_json, started_at, ended_at, created_at
		FROM voice_agent_sessions ORDER BY id ASC`)
	if err != nil {
		return err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable.
	sessions, err := scanVoiceAgentSessions(rows)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if err := replaceVoiceAgentSessionChildren(ctx, db, dialectSQLite, session.ID, session); err != nil {
			return err
		}
	}
	return nil
}

//nolint:rowserrcheck // scanVoiceAgentSessions checks rows.Err() after consuming rows.
func backfillPostgresVoiceAgentNormalized(ctx context.Context, db *sql.DB) error {
	hasTurnsJSON, err := postgresColumnExists(ctx, db, "voice_agent_sessions", "turns_json")
	if err != nil {
		return err
	}
	if !hasTurnsJSON {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id, title, summary, raw_summary, transcript, language, provider_profile_id, runtime_kind,
		turns_json::text, ideas_json::text, decisions_json::text, open_questions_json::text, next_steps_json::text,
		started_at, ended_at, created_at
		FROM voice_agent_sessions ORDER BY id ASC`)
	if err != nil {
		return err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable.
	sessions, err := scanVoiceAgentSessions(rows)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if err := replaceVoiceAgentSessionChildren(ctx, db, dialectPostgres, session.ID, session); err != nil {
			return err
		}
	}
	return nil
}

func backfillSQLiteAudioAssets(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO audio_assets
		(owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms, created_at, updated_at)
		SELECT 'transcription', id, 'local-file', audio_path, 'audio/wav', 0, COALESCE(duration_ms, 0), created_at, CURRENT_TIMESTAMP
		FROM transcriptions WHERE COALESCE(audio_path, '') != ''`)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO audio_assets
		(owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms, created_at, updated_at)
		SELECT 'quick_note', id, 'local-file', audio_path, 'audio/wav', 0, COALESCE(duration_ms, 0), created_at, CURRENT_TIMESTAMP
		FROM quick_notes WHERE COALESCE(audio_path, '') != ''`)
	return err
}

func backfillPostgresAudioAssets(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `INSERT INTO audio_assets
		(owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms, created_at, updated_at)
		SELECT 'transcription', id, 'local-file', audio_path, 'audio/wav', 0, COALESCE(duration_ms, 0), created_at, NOW()
		FROM transcriptions WHERE COALESCE(audio_path, '') <> ''
		ON CONFLICT(owner_kind, owner_id, path) DO NOTHING`)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO audio_assets
		(owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms, created_at, updated_at)
		SELECT 'quick_note', id, 'local-file', audio_path, 'audio/wav', 0, COALESCE(duration_ms, 0), created_at, NOW()
		FROM quick_notes WHERE COALESCE(audio_path, '') <> ''
		ON CONFLICT(owner_kind, owner_id, path) DO NOTHING`)
	return err
}
