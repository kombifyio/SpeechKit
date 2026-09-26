package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type storeMigration struct {
	version string
	run     func(context.Context, *sql.DB) error
}

func runSQLiteMigrations(ctx context.Context, db *sql.DB) error {
	if err := ensureSchemaMigrations(ctx, db, "sqlite"); err != nil {
		return fmt.Errorf("migrate sqlite ledger: %w", err)
	}
	if err := ensureSQLiteLegacyColumnsForCurrentMigrations(ctx, db); err != nil {
		return err
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
		{version: "sqlite:011_audio_assets", run: runSQLiteAudioAssetsMigration},
		sqliteSQLMigration("sqlite:012_indexes", sqliteMigration012),
		{version: "sqlite:013_storage_scopes", run: runSQLiteStorageScopesMigration},
		{version: "sqlite:014_storage_v3_model", run: runSQLiteStorage3ModelMigration},
		sqliteSQLMigration("sqlite:015_wakeword_activations", sqliteMigration015),
		{version: "sqlite:016_transcription_speakers", run: func(ctx context.Context, db *sql.DB) error {
			return ensureSQLiteColumn(ctx, db, "transcriptions", "speaker_json", "TEXT NOT NULL DEFAULT ''")
		}},
		sqliteSQLMigration("sqlite:017_customization", sqliteMigration017),
		sqliteSQLMigration("sqlite:018_recording_sessions", sqliteMigration018),
		{version: "sqlite:019_recording_session_state", run: runSQLiteRecordingSessionStateMigration},
		{version: "sqlite:020_recording_session_channels", run: runSQLiteRecordingSessionChannelsMigration},
		sqliteSQLMigration("sqlite:021_recording_session_notes", sqliteMigration021),
		sqliteSQLMigration("sqlite:022_recording_session_enhancements", sqliteMigration022),
		{version: "sqlite:023_meeting_retention", run: func(ctx context.Context, db *sql.DB) error {
			return ensureSQLiteColumn(ctx, db, "recording_sessions", "retention_pinned", "INTEGER NOT NULL DEFAULT 0")
		}},
		sqliteSQLMigration("sqlite:024_customization_word_identity", sqliteMigration024),
		{version: "sqlite:025_meeting_enhancement_jobs", run: runSQLiteMeetingEnhancementJobsMigration},
		sqliteSQLMigration("sqlite:026_meeting_summary_batches", sqliteMigration026),
		sqliteSQLMigration("sqlite:027_recording_session_snapshots", sqliteMigration027),
		{version: "sqlite:028_transcription_pinned", run: func(ctx context.Context, db *sql.DB) error {
			if err := ensureSQLiteColumn(ctx, db, "transcriptions", "pinned", "INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, sqliteMigration028)
			return err
		}},
		sqliteSQLMigration("sqlite:029_recording_session_imports", sqliteMigration029),
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
		{version: "postgres:002_voice_agent_sessions", run: runPostgresVoiceAgentSessionsMigration},
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
		{version: "postgres:006_audio_assets", run: runPostgresAudioAssetsMigration},
		postgresSQLMigration("postgres:007_indexes", postgresMigration007),
		postgresSQLMigration("postgres:008_storage_scopes", postgresMigration008),
		{version: "postgres:009_storage_v3_model", run: runPostgresStorage3ModelMigration},
		postgresSQLMigration("postgres:010_wakeword_activations", postgresMigration010),
		postgresSQLMigration("postgres:011_storage_scope_sequence", postgresMigration011),
		postgresSQLMigration("postgres:012_transcription_speakers", postgresMigration012),
		postgresSQLMigration("postgres:013_customization", postgresMigration013),
		postgresSQLMigration("postgres:014_recording_sessions", postgresMigration014),
		postgresSQLMigration("postgres:015_recording_session_state", postgresMigration015),
		postgresSQLMigration("postgres:016_recording_session_channels", postgresMigration016),
		postgresSQLMigration("postgres:017_recording_session_notes", postgresMigration017),
		postgresSQLMigration("postgres:018_recording_session_enhancements", postgresMigration018),
		postgresSQLMigration("postgres:019_meeting_retention", postgresMigration019),
		postgresSQLMigration("postgres:020_customization_word_identity", postgresMigration020),
		postgresSQLMigration("postgres:021_meeting_enhancement_jobs", postgresMigration021),
		postgresSQLMigration("postgres:022_meeting_summary_batches", postgresMigration022),
		postgresSQLMigration("postgres:023_recording_session_snapshots", postgresMigration023),
		postgresSQLMigration("postgres:024_transcription_pinned", postgresMigration024),
		postgresSQLMigration("postgres:025_recording_session_imports", postgresMigration025),
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
