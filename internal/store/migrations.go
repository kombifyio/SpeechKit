package store

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

func ensureSQLiteLegacyColumnsForCurrentMigrations(ctx context.Context, db *sql.DB) error {
	specs := map[string]map[string]string{
		"transcriptions": {
			"language_base": "TEXT NOT NULL DEFAULT ''",
			"audio_path":    "TEXT NOT NULL DEFAULT ''",
			"owner_user_id": "TEXT NOT NULL DEFAULT ''",
			"owner_org_id":  "TEXT NOT NULL DEFAULT ''",
			"owner_source":  "TEXT NOT NULL DEFAULT ''",
			"speaker_json":  "TEXT NOT NULL DEFAULT ''",
		},
		"quick_notes": {
			"language_base": "TEXT NOT NULL DEFAULT ''",
			"audio_path":    "TEXT NOT NULL DEFAULT ''",
		},
		"voice_agent_sessions": {
			"language_base":       "TEXT NOT NULL DEFAULT ''",
			"owner_user_id":       "TEXT NOT NULL DEFAULT ''",
			"owner_org_id":        "TEXT NOT NULL DEFAULT ''",
			"owner_source":        "TEXT NOT NULL DEFAULT ''",
			"raw_summary":         "TEXT NOT NULL DEFAULT ''",
			"transcript":          "TEXT NOT NULL DEFAULT ''",
			"runtime_kind":        "TEXT NOT NULL DEFAULT ''",
			"turns_json":          "TEXT NOT NULL DEFAULT '[]'",
			"ideas_json":          "TEXT NOT NULL DEFAULT '[]'",
			"decisions_json":      "TEXT NOT NULL DEFAULT '[]'",
			"open_questions_json": "TEXT NOT NULL DEFAULT '[]'",
			"next_steps_json":     "TEXT NOT NULL DEFAULT '[]'",
			"started_at":          "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
			"ended_at":            "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
			"created_at":          "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
			"provider_profile_id": "TEXT NOT NULL DEFAULT ''",
		},
		"voice_agent_personas": {
			"description":      "TEXT NOT NULL DEFAULT ''",
			"voice":            "TEXT NOT NULL DEFAULT ''",
			"locale":           "TEXT NOT NULL DEFAULT ''",
			"default_role":     "TEXT NOT NULL DEFAULT ''",
			"default_sequence": "TEXT NOT NULL DEFAULT ''",
			"tags_json":        "TEXT NOT NULL DEFAULT '[]'",
			"metadata_json":    "TEXT NOT NULL DEFAULT '{}'",
			"created_at":       "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
			"updated_at":       "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		},
		"voice_agent_roles": {
			"refinement_prompt":              "TEXT NOT NULL DEFAULT ''",
			"locale":                         "TEXT NOT NULL DEFAULT ''",
			"vocabulary_hint":                "TEXT NOT NULL DEFAULT ''",
			"tool_allowlist_json":            "TEXT NOT NULL DEFAULT '[]'",
			"temperature":                    "REAL NOT NULL DEFAULT 0",
			"thinking_enabled":               "INTEGER NOT NULL DEFAULT 0",
			"thinking_level":                 "TEXT NOT NULL DEFAULT ''",
			"include_thoughts":               "INTEGER NOT NULL DEFAULT 0",
			"thinking_budget":                "INTEGER NOT NULL DEFAULT 0",
			"automatic_activity_detection":   "INTEGER NOT NULL DEFAULT 0",
			"vad_start_sensitivity":          "TEXT NOT NULL DEFAULT ''",
			"vad_end_sensitivity":            "TEXT NOT NULL DEFAULT ''",
			"vad_prefix_padding_ms":          "INTEGER NOT NULL DEFAULT 0",
			"vad_silence_duration_ms":        "INTEGER NOT NULL DEFAULT 0",
			"activity_handling":              "TEXT NOT NULL DEFAULT ''",
			"turn_coverage":                  "TEXT NOT NULL DEFAULT ''",
			"context_compression_enabled":    "INTEGER NOT NULL DEFAULT 0",
			"context_compression_trigger_tk": "INTEGER NOT NULL DEFAULT 0",
			"context_compression_target_tk":  "INTEGER NOT NULL DEFAULT 0",
			"enable_affective_dialog":        "INTEGER NOT NULL DEFAULT 0",
			"created_at":                     "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
			"updated_at":                     "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		},
		"voice_agent_sequences": {
			"description": "TEXT NOT NULL DEFAULT ''",
			"completion":  "TEXT NOT NULL DEFAULT ''",
			"max_turns":   "INTEGER NOT NULL DEFAULT 0",
			"steps_json":  "TEXT NOT NULL DEFAULT '[]'",
			"created_at":  "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
			"updated_at":  "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		},
		"audio_assets": {
			"owner_kind": "TEXT NOT NULL DEFAULT ''",
			"owner_id":   "INTEGER NOT NULL DEFAULT 0",
		},
	}
	for table, columns := range specs {
		exists, err := tableExists(ctx, db, "sqlite", table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		for column, definition := range columns {
			if err := ensureSQLiteColumn(ctx, db, table, column, definition); err != nil {
				return err
			}
		}
	}
	return nil
}

func runSQLiteAudioAssetsMigration(ctx context.Context, db *sql.DB) error {
	if err := ensureSQLiteColumn(ctx, db, "audio_assets", "owner_kind", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "audio_assets", "owner_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, sqliteMigration011); err != nil {
		return err
	}
	return backfillSQLiteAudioAssets(ctx, db)
}

func runPostgresAudioAssetsMigration(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
ALTER TABLE transcriptions
	ADD COLUMN IF NOT EXISTS audio_path TEXT NOT NULL DEFAULT '';
ALTER TABLE quick_notes
	ADD COLUMN IF NOT EXISTS audio_path TEXT NOT NULL DEFAULT '';
ALTER TABLE audio_assets
	ADD COLUMN IF NOT EXISTS owner_kind TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS owner_id BIGINT NOT NULL DEFAULT 0;
`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, postgresMigration006); err != nil {
		return err
	}
	return backfillPostgresAudioAssets(ctx, db)
}

func runPostgresVoiceAgentSessionsMigration(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, postgresMigration002); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `
ALTER TABLE voice_agent_sessions
	ADD COLUMN IF NOT EXISTS turns_json JSONB NOT NULL DEFAULT '[]'::jsonb,
	ADD COLUMN IF NOT EXISTS ideas_json JSONB NOT NULL DEFAULT '[]'::jsonb,
	ADD COLUMN IF NOT EXISTS decisions_json JSONB NOT NULL DEFAULT '[]'::jsonb,
	ADD COLUMN IF NOT EXISTS open_questions_json JSONB NOT NULL DEFAULT '[]'::jsonb,
	ADD COLUMN IF NOT EXISTS next_steps_json JSONB NOT NULL DEFAULT '[]'::jsonb;
`)
	return err
}

func tableExists(ctx context.Context, db *sql.DB, dialect, table string) (bool, error) {
	var count int
	var err error
	if dialect == "postgres" {
		err = db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = $1`,
			table,
		).Scan(&count)
	} else {
		err = db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
			table,
		).Scan(&count)
	}
	return count > 0, err
}

func postgresColumnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`,
		table,
		column,
	).Scan(&count)
	return count > 0, err
}

func runSQLiteStorageScopesMigration(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, sqliteMigration013); err != nil {
		return err
	}
	if err := ensureSQLiteScopedStoreStats(ctx, db); err != nil {
		return err
	}
	for _, table := range []string{
		"transcriptions",
		"quick_notes",
		"user_dictionary_entries",
		"voice_agent_sessions",
		"audio_assets",
	} {
		if err := ensureSQLiteColumn(ctx, db, table, "scope_id", "INTEGER NOT NULL DEFAULT 1 REFERENCES storage_scopes(id)"); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO store_stats (scope_id) VALUES (1)`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO store_stats (scope_id)
		SELECT DISTINCT scope_id FROM transcriptions
		UNION
		SELECT DISTINCT scope_id FROM quick_notes`)
	return err
}

func ensureSQLiteScopedStoreStats(ctx context.Context, db *sql.DB) error {
	hasScopeID, err := sqliteColumnExists(ctx, db, "store_stats", "scope_id")
	if err != nil {
		return err
	}
	if hasScopeID {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE store_stats_scoped (
			scope_id                INTEGER PRIMARY KEY REFERENCES storage_scopes(id) ON DELETE CASCADE,
			transcriptions_count    INTEGER NOT NULL DEFAULT 0,
			quick_notes_count       INTEGER NOT NULL DEFAULT 0,
			total_words             INTEGER NOT NULL DEFAULT 0,
			total_audio_duration_ms INTEGER NOT NULL DEFAULT 0,
			total_latency_ms        INTEGER NOT NULL DEFAULT 0,
			latency_count           INTEGER NOT NULL DEFAULT 0,
			updated_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT OR IGNORE INTO store_stats_scoped
			(scope_id, transcriptions_count, quick_notes_count, total_words, total_audio_duration_ms, total_latency_ms, latency_count, updated_at)
		SELECT 1, transcriptions_count, quick_notes_count, total_words, total_audio_duration_ms, total_latency_ms, latency_count, updated_at
		FROM store_stats;
		DROP TABLE store_stats;
		ALTER TABLE store_stats_scoped RENAME TO store_stats;
	`); err != nil {
		return err
	}
	return tx.Commit()
}

func runSQLiteStorage3ModelMigration(ctx context.Context, db *sql.DB) error {
	for _, table := range []string{"transcriptions", "voice_agent_sessions"} {
		if err := ensureSQLiteColumn(ctx, db, table, "owner_user_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
		if err := ensureSQLiteColumn(ctx, db, table, "owner_org_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
		if err := ensureSQLiteColumn(ctx, db, table, "owner_source", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	for _, table := range []string{"transcriptions", "quick_notes", "voice_agent_sessions"} {
		if err := ensureSQLiteColumn(ctx, db, table, "language_base", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	needsDictionaryRebuild, err := sqliteUserDictionaryNeedsScopedRebuild(ctx, db)
	if err != nil {
		return err
	}
	if needsDictionaryRebuild {
		if err := rebuildSQLiteUserDictionaryForScopes(ctx, db); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, sqliteMigration014); err != nil {
		return err
	}
	return refreshAllSQLiteStoreStats(ctx, db)
}

func runSQLiteRecordingSessionStateMigration(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name       string
		definition string
	}{
		{name: "capture_status", definition: "TEXT NOT NULL DEFAULT 'idle'"},
		{name: "capture_started_at", definition: "DATETIME"},
		{name: "capture_paused_at", definition: "DATETIME"},
		{name: "capture_stopped_at", definition: "DATETIME"},
		{name: "summary_status", definition: "TEXT NOT NULL DEFAULT 'idle'"},
		{name: "summary_error", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "summary_updated_at", definition: "DATETIME"},
	}
	for _, column := range columns {
		if err := ensureSQLiteColumn(ctx, db, "recording_sessions", column.name, column.definition); err != nil {
			return err
		}
	}
	_, err := db.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS idx_recording_sessions_scope_capture
    ON recording_sessions(scope_id, capture_status, updated_at DESC, id DESC);
`)
	return err
}

func runPostgresStorage3ModelMigration(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
ALTER TABLE transcriptions
	ADD COLUMN IF NOT EXISTS owner_user_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS owner_org_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS owner_source TEXT NOT NULL DEFAULT '';
ALTER TABLE voice_agent_sessions
	ADD COLUMN IF NOT EXISTS owner_user_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS owner_org_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS owner_source TEXT NOT NULL DEFAULT '';
`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, postgresMigration009); err != nil {
		return err
	}
	return refreshAllPostgresStoreStats(ctx, db)
}

func rebuildSQLiteUserDictionaryForScopes(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if _, err := tx.ExecContext(ctx, `
		DROP TABLE IF EXISTS user_dictionary_entries_v3;
		CREATE TABLE user_dictionary_entries_v3 (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			scope_id    INTEGER NOT NULL DEFAULT 1 REFERENCES storage_scopes(id),
			spoken      TEXT NOT NULL,
			canonical   TEXT NOT NULL,
			language    TEXT NOT NULL DEFAULT '',
			source      TEXT NOT NULL DEFAULT 'settings',
			enabled     INTEGER NOT NULL DEFAULT 1,
			usage_count INTEGER NOT NULL DEFAULT 0,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(scope_id, spoken, canonical, language, source)
		);
		INSERT OR IGNORE INTO user_dictionary_entries_v3
			(id, scope_id, spoken, canonical, language, source, enabled, usage_count, created_at, updated_at)
		SELECT id, COALESCE(scope_id, 1), spoken, canonical, language, source, enabled, usage_count, created_at, updated_at
		FROM user_dictionary_entries;
		DROP TABLE user_dictionary_entries;
		ALTER TABLE user_dictionary_entries_v3 RENAME TO user_dictionary_entries;
		CREATE INDEX IF NOT EXISTS idx_user_dictionary_language
			ON user_dictionary_entries(scope_id, language, enabled, id);
		CREATE INDEX IF NOT EXISTS idx_user_dictionary_canonical_lookup
			ON user_dictionary_entries(scope_id, lower(canonical), language, enabled);
	`); err != nil {
		return err
	}
	return tx.Commit()
}

func sqliteUserDictionaryNeedsScopedRebuild(ctx context.Context, db *sql.DB) (bool, error) {
	var createSQL string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(sql, '') FROM sqlite_master WHERE type = 'table' AND name = 'user_dictionary_entries'`).Scan(&createSQL); err != nil {
		return false, err
	}
	normalized := strings.ToLower(createSQL)
	normalized = strings.NewReplacer("\r", "", "\n", "", "\t", "", " ", "").Replace(normalized)
	return !strings.Contains(normalized, "unique(scope_id,spoken,canonical,language,source)"), nil
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
