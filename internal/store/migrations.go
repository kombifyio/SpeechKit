package store

// Migration authoring rules
// =========================
//
// SpeechKit uses two complementary forms for schema migrations against
// SQLite and Postgres. Pick the one that matches the change you need:
//
//  1. Pure DDL — use sqliteSQLMigration / postgresSQLMigration with an
//     embedded .sql file (see sqlite.go / postgres.go //go:embed).
//     Use this when the migration is a single statement (or simple
//     batch) that can be expressed in plain SQL with no host-language
//     logic.
//
//  2. Idempotent or programmatic — use {version, run: func(...)}.
//     Use this when the migration needs:
//       - idempotent ALTER TABLE … ADD COLUMN via ensureSQLiteColumn
//         (re-runnable on partially-upgraded DBs without errors).
//       - data backfill that joins source rows with derived values
//         (see backfillWordCounts, backfillAudioAssets).
//       - conditional logic (probe table_info, branch on dialect, etc.).
//
// Both forms append to the same []storeMigration slice. The applyMigration
// runner records each version once in schema_migrations and re-runs the
// migration body on every boot — every body MUST be idempotent.
//
// When adding a new migration:
//   - Allocate the next sequential version number per dialect
//     (sqlite:NNN_..., postgres:NNN_...).
//   - If pure DDL: drop a .sql file under migrations/{sqlite|postgres}/
//     and add an //go:embed line in sqlite.go or postgres.go, then
//     reference it via the SQL-form helper.
//   - If programmatic: write a {version, run: ...} entry inline; reuse
//     ensureSQLiteColumn / backfill helpers where they exist.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type storeMigration struct {
	version string
	run     func(context.Context, *sql.DB) error
}

func runSQLiteMigrations(ctx context.Context, db *sql.DB) error {
	if err := ensureSchemaMigrations(ctx, db, "sqlite"); err != nil {
		return fmt.Errorf("migrate sqlite ledger: %w", err)
	}
	migrations := []storeMigration{
		sqliteSQLMigration("sqlite:001_init", sqliteMigration001),
		{version: "sqlite:002_quick_notes_pinned", run: func(ctx context.Context, db *sql.DB) error {
			return ensureSQLiteColumn(ctx, db, "quick_notes", "pinned", "INTEGER NOT NULL DEFAULT 0")
		}},
		{version: "sqlite:003_durations", run: func(ctx context.Context, db *sql.DB) error {
			if err := ensureSQLiteColumn(ctx, db, "transcriptions", "duration_ms", "INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
			return ensureSQLiteColumn(ctx, db, "quick_notes", "duration_ms", "INTEGER NOT NULL DEFAULT 0")
		}},
		{version: "sqlite:004_transcription_model", run: func(ctx context.Context, db *sql.DB) error {
			return ensureSQLiteColumn(ctx, db, "transcriptions", "model", "TEXT NOT NULL DEFAULT ''")
		}},
		sqliteSQLMigration("sqlite:005_user_dictionary", sqliteMigration005),
		sqliteSQLMigration("sqlite:006_voice_agent_sessions", sqliteMigration006),
		sqliteSQLMigration("sqlite:007_personas", sqliteMigration007),
		{version: "sqlite:008_persona_default_sequence", run: func(ctx context.Context, db *sql.DB) error {
			return ensureSQLiteColumn(ctx, db, "voice_agent_personas", "default_sequence", "TEXT NOT NULL DEFAULT ''")
		}},
		{version: "sqlite:009_word_counts", run: func(ctx context.Context, db *sql.DB) error {
			if err := ensureSQLiteColumn(ctx, db, "transcriptions", "word_count", "INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
			if err := ensureSQLiteColumn(ctx, db, "quick_notes", "word_count", "INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
			return backfillSQLiteWordCounts(ctx, db)
		}},
		{version: "sqlite:010_voice_agent_normalized", run: func(ctx context.Context, db *sql.DB) error {
			if _, err := db.ExecContext(ctx, sqliteMigration010); err != nil {
				return err
			}
			return backfillSQLiteVoiceAgentNormalized(ctx, db)
		}},
		{version: "sqlite:011_audio_assets", run: func(ctx context.Context, db *sql.DB) error {
			if _, err := db.ExecContext(ctx, sqliteMigration011); err != nil {
				return err
			}
			return backfillSQLiteAudioAssets(ctx, db)
		}},
		sqliteSQLMigration("sqlite:012_indexes", sqliteMigration012),
	}
	for _, migration := range migrations {
		if err := applyMigration(ctx, db, "sqlite", migration); err != nil {
			return fmt.Errorf("migrate %s: %w", migration.version, err)
		}
	}
	return nil
}

func runPostgresMigrations(ctx context.Context, db *sql.DB) error {
	if err := ensureSchemaMigrations(ctx, db, "postgres"); err != nil {
		return fmt.Errorf("migrate postgres ledger: %w", err)
	}
	migrations := []storeMigration{
		postgresSQLMigration("postgres:001_init", postgresMigration001),
		postgresSQLMigration("postgres:002_voice_agent_sessions", postgresMigration002),
		postgresSQLMigration("postgres:003_personas", postgresMigration003),
		{version: "postgres:004_word_counts", run: func(ctx context.Context, db *sql.DB) error {
			if _, err := db.ExecContext(ctx, postgresMigration004); err != nil {
				return err
			}
			return backfillPostgresWordCounts(ctx, db)
		}},
		{version: "postgres:005_voice_agent_normalized", run: func(ctx context.Context, db *sql.DB) error {
			if _, err := db.ExecContext(ctx, postgresMigration005); err != nil {
				return err
			}
			return backfillPostgresVoiceAgentNormalized(ctx, db)
		}},
		{version: "postgres:006_audio_assets", run: func(ctx context.Context, db *sql.DB) error {
			if _, err := db.ExecContext(ctx, postgresMigration006); err != nil {
				return err
			}
			return backfillPostgresAudioAssets(ctx, db)
		}},
		postgresSQLMigration("postgres:007_indexes", postgresMigration007),
	}
	for _, migration := range migrations {
		if err := applyMigration(ctx, db, "postgres", migration); err != nil {
			return fmt.Errorf("migrate %s: %w", migration.version, err)
		}
	}
	return nil
}

func sqliteSQLMigration(version, statement string) storeMigration {
	return storeMigration{version: version, run: func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, statement)
		return err
	}}
}

func postgresSQLMigration(version, statement string) storeMigration {
	return storeMigration{version: version, run: func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, statement)
		return err
	}}
}

func ensureSchemaMigrations(ctx context.Context, db *sql.DB, dialect string) error {
	statement := `CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`
	if dialect == "postgres" {
		statement = `CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`
	}
	_, err := db.ExecContext(ctx, statement)
	return err
}

func applyMigration(ctx context.Context, db *sql.DB, dialect string, migration storeMigration) error {
	var existing string
	var err error
	if dialect == "postgres" {
		err = db.QueryRowContext(ctx, `SELECT version FROM schema_migrations WHERE version = $1`, migration.version).Scan(&existing)
	} else {
		err = db.QueryRowContext(ctx, `SELECT version FROM schema_migrations WHERE version = ?`, migration.version).Scan(&existing)
	}
	alreadyApplied := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Run every migration even when it is already recorded. All migrations are
	// deliberately idempotent so startup can repair partially-upgraded DBs.
	if err := migration.run(ctx, db); err != nil {
		return err
	}
	if alreadyApplied {
		return nil
	}
	if dialect == "postgres" {
		_, err = db.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT (version) DO NOTHING`, migration.version)
	} else {
		_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (version) VALUES (?)`, migration.version)
	}
	return err
}

func ensureSQLiteColumn(ctx context.Context, db *sql.DB, table, column, definition string) error {
	exists, err := sqliteColumnExists(ctx, db, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func sqliteColumnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable.
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return false, err
		}
		if strings.EqualFold(name, column) {
			return true, nil
		}
	}
	return false, rows.Err()
}

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
		if err := replaceVoiceAgentSessionChildren(ctx, db, "sqlite", session.ID, session); err != nil {
			return err
		}
	}
	return nil
}

//nolint:rowserrcheck // scanVoiceAgentSessions checks rows.Err() after consuming rows.
func backfillPostgresVoiceAgentNormalized(ctx context.Context, db *sql.DB) error {
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
		if err := replaceVoiceAgentSessionChildren(ctx, db, "postgres", session.ID, session); err != nil {
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
