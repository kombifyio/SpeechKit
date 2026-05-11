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
		{version: "sqlite:013_owner_scope", run: func(ctx context.Context, db *sql.DB) error {
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
			_, err := db.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS idx_transcriptions_owner_created
	ON transcriptions(owner_org_id, owner_user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_voice_agent_sessions_owner_created
	ON voice_agent_sessions(owner_org_id, owner_user_id, created_at DESC, id DESC);
`)
			return err
		}},
		{version: "sqlite:014_persona_schema_repair", run: repairSQLitePersonaSchema},
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
		{version: "postgres:008_owner_scope", run: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `
ALTER TABLE transcriptions
	ADD COLUMN IF NOT EXISTS owner_user_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS owner_org_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS owner_source TEXT NOT NULL DEFAULT '';
ALTER TABLE voice_agent_sessions
	ADD COLUMN IF NOT EXISTS owner_user_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS owner_org_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS owner_source TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_transcriptions_owner_created
	ON transcriptions(owner_org_id, owner_user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_voice_agent_sessions_owner_created
	ON voice_agent_sessions(owner_org_id, owner_user_id, created_at DESC, id DESC);
`)
			return err
		}},
		{version: "postgres:009_persona_schema_repair", run: repairPostgresPersonaSchema},
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

type sqliteColumnRepair struct {
	table      string
	column     string
	definition string
}

func repairSQLitePersonaSchema(ctx context.Context, db *sql.DB) error {
	repairs := []sqliteColumnRepair{
		{"voice_agent_personas", "display_name", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_personas", "description", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_personas", "voice", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_personas", "locale", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_personas", "default_role", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_personas", "default_sequence", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_personas", "tags_json", "TEXT NOT NULL DEFAULT '[]'"},
		{"voice_agent_personas", "metadata_json", "TEXT NOT NULL DEFAULT '{}'"},
		{"voice_agent_personas", "created_at", "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP"},
		{"voice_agent_personas", "updated_at", "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP"},
		{"voice_agent_roles", "display_name", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_roles", "system_prompt", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_roles", "refinement_prompt", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_roles", "locale", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_roles", "vocabulary_hint", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_roles", "tool_allowlist_json", "TEXT NOT NULL DEFAULT '[]'"},
		{"voice_agent_roles", "temperature", "REAL NOT NULL DEFAULT 0"},
		{"voice_agent_roles", "thinking_enabled", "INTEGER NOT NULL DEFAULT 0"},
		{"voice_agent_roles", "thinking_level", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_roles", "include_thoughts", "INTEGER NOT NULL DEFAULT 0"},
		{"voice_agent_roles", "thinking_budget", "INTEGER NOT NULL DEFAULT 0"},
		{"voice_agent_roles", "automatic_activity_detection", "INTEGER NOT NULL DEFAULT 0"},
		{"voice_agent_roles", "vad_start_sensitivity", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_roles", "vad_end_sensitivity", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_roles", "vad_prefix_padding_ms", "INTEGER NOT NULL DEFAULT 0"},
		{"voice_agent_roles", "vad_silence_duration_ms", "INTEGER NOT NULL DEFAULT 0"},
		{"voice_agent_roles", "activity_handling", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_roles", "turn_coverage", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_roles", "context_compression_enabled", "INTEGER NOT NULL DEFAULT 0"},
		{"voice_agent_roles", "context_compression_trigger_tk", "INTEGER NOT NULL DEFAULT 0"},
		{"voice_agent_roles", "context_compression_target_tk", "INTEGER NOT NULL DEFAULT 0"},
		{"voice_agent_roles", "enable_affective_dialog", "INTEGER NOT NULL DEFAULT 0"},
		{"voice_agent_roles", "created_at", "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP"},
		{"voice_agent_roles", "updated_at", "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP"},
		{"voice_agent_sequences", "display_name", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_sequences", "description", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_sequences", "completion", "TEXT NOT NULL DEFAULT ''"},
		{"voice_agent_sequences", "max_turns", "INTEGER NOT NULL DEFAULT 0"},
		{"voice_agent_sequences", "steps_json", "TEXT NOT NULL DEFAULT '[]'"},
		{"voice_agent_sequences", "created_at", "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP"},
		{"voice_agent_sequences", "updated_at", "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP"},
	}
	for _, repair := range repairs {
		if err := ensureSQLiteColumn(ctx, db, repair.table, repair.column, repair.definition); err != nil {
			return err
		}
	}
	return nil
}

func repairPostgresPersonaSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback is harmless after commit and not actionable.
	for _, repair := range postgresPersonaRepairColumnSpecs() {
		if err := repairPostgresColumn(ctx, tx, repair); err != nil {
			return fmt.Errorf("repair %s.%s: %w", repair.table, repair.column, err)
		}
	}
	return tx.Commit()
}

type postgresColumnRepair struct {
	table       string
	column      string
	dataType    string
	defaultExpr string
	notNull     bool
}

func postgresPersonaRepairColumnSpecs() []postgresColumnRepair {
	text := func(table, column string) postgresColumnRepair {
		return postgresColumnRepair{table: table, column: column, dataType: "TEXT", defaultExpr: "''", notNull: true}
	}
	jsonb := func(table, column, defaultExpr string) postgresColumnRepair {
		return postgresColumnRepair{table: table, column: column, dataType: "JSONB", defaultExpr: defaultExpr, notNull: true}
	}
	bigint := func(table, column string) postgresColumnRepair {
		return postgresColumnRepair{table: table, column: column, dataType: "BIGINT", defaultExpr: "0", notNull: true}
	}
	roleBoolean := func(column string) postgresColumnRepair {
		return postgresColumnRepair{table: "voice_agent_roles", column: column, dataType: "BOOLEAN", defaultExpr: "FALSE", notNull: true}
	}
	timestamptz := func(table, column string) postgresColumnRepair {
		return postgresColumnRepair{table: table, column: column, dataType: "TIMESTAMPTZ", defaultExpr: "NOW()", notNull: true}
	}

	return []postgresColumnRepair{
		text("voice_agent_personas", "display_name"),
		text("voice_agent_personas", "description"),
		text("voice_agent_personas", "voice"),
		text("voice_agent_personas", "locale"),
		text("voice_agent_personas", "default_role"),
		text("voice_agent_personas", "default_sequence"),
		jsonb("voice_agent_personas", "tags_json", "'[]'::jsonb"),
		jsonb("voice_agent_personas", "metadata_json", "'{}'::jsonb"),
		timestamptz("voice_agent_personas", "created_at"),
		timestamptz("voice_agent_personas", "updated_at"),

		text("voice_agent_roles", "display_name"),
		text("voice_agent_roles", "system_prompt"),
		text("voice_agent_roles", "refinement_prompt"),
		text("voice_agent_roles", "locale"),
		text("voice_agent_roles", "vocabulary_hint"),
		jsonb("voice_agent_roles", "tool_allowlist_json", "'[]'::jsonb"),
		{table: "voice_agent_roles", column: "temperature", dataType: "DOUBLE PRECISION", defaultExpr: "0", notNull: true},
		roleBoolean("thinking_enabled"),
		text("voice_agent_roles", "thinking_level"),
		roleBoolean("include_thoughts"),
		bigint("voice_agent_roles", "thinking_budget"),
		roleBoolean("automatic_activity_detection"),
		text("voice_agent_roles", "vad_start_sensitivity"),
		text("voice_agent_roles", "vad_end_sensitivity"),
		bigint("voice_agent_roles", "vad_prefix_padding_ms"),
		bigint("voice_agent_roles", "vad_silence_duration_ms"),
		text("voice_agent_roles", "activity_handling"),
		text("voice_agent_roles", "turn_coverage"),
		roleBoolean("context_compression_enabled"),
		bigint("voice_agent_roles", "context_compression_trigger_tk"),
		bigint("voice_agent_roles", "context_compression_target_tk"),
		roleBoolean("enable_affective_dialog"),
		timestamptz("voice_agent_roles", "created_at"),
		timestamptz("voice_agent_roles", "updated_at"),

		text("voice_agent_sequences", "display_name"),
		text("voice_agent_sequences", "description"),
		text("voice_agent_sequences", "completion"),
		bigint("voice_agent_sequences", "max_turns"),
		jsonb("voice_agent_sequences", "steps_json", "'[]'::jsonb"),
		timestamptz("voice_agent_sequences", "created_at"),
		timestamptz("voice_agent_sequences", "updated_at"),
	}
}

type postgresRepairDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func repairPostgresColumn(ctx context.Context, db postgresRepairDB, repair postgresColumnRepair) error {
	table := postgresQuoteIdent(repair.table)
	column := postgresQuoteIdent(repair.column)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s DEFAULT %s`,
		table, column, repair.dataType, repair.defaultExpr)); err != nil {
		return fmt.Errorf("add column: %w", err)
	}

	info, err := postgresColumnInfo(ctx, db, repair.table, repair.column)
	if err != nil {
		return fmt.Errorf("inspect column: %w", err)
	}
	if info == nil {
		return fmt.Errorf("column missing after repair add")
	}
	if info.dataType != postgresInformationSchemaType(repair.dataType) {
		usingExpr, err := postgresRepairUsingExpression(repair)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT`,
			table, column)); err != nil {
			return fmt.Errorf("drop incompatible default: %w", err)
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s TYPE %s USING %s`,
			table, column, repair.dataType, usingExpr)); err != nil {
			return fmt.Errorf("alter type to %s: %w", repair.dataType, err)
		}
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET %s = %s WHERE %s IS NULL`,
		table, column, repair.defaultExpr, column)); err != nil {
		return fmt.Errorf("backfill nulls: %w", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s`,
		table, column, repair.defaultExpr)); err != nil {
		return fmt.Errorf("set default: %w", err)
	}
	if repair.notNull {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s SET NOT NULL`,
			table, column)); err != nil {
			return fmt.Errorf("set not null: %w", err)
		}
	}
	return nil
}

type postgresColumnMetadata struct {
	dataType string
}

func postgresColumnInfo(ctx context.Context, db postgresRepairDB, table, column string) (*postgresColumnMetadata, error) {
	var info postgresColumnMetadata
	err := db.QueryRowContext(ctx, `
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = $1
		  AND column_name = $2`,
		table, column,
	).Scan(&info.dataType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info.dataType = strings.ToLower(strings.TrimSpace(info.dataType))
	return &info, nil
}

func postgresRepairUsingExpression(repair postgresColumnRepair) (string, error) {
	column := repair.column
	switch strings.ToUpper(strings.TrimSpace(repair.dataType)) {
	case "TEXT":
		return fmt.Sprintf("COALESCE(%s::text, %s)", column, repair.defaultExpr), nil
	case "JSONB":
		return fmt.Sprintf("CASE WHEN %s IS NULL OR BTRIM(%s::text) = '' THEN %s ELSE %s::jsonb END",
			column, column, repair.defaultExpr, column), nil
	case "BIGINT":
		return fmt.Sprintf("COALESCE(NULLIF(TRIM(%s::text), '')::bigint, %s)", column, repair.defaultExpr), nil
	case "DOUBLE PRECISION":
		return fmt.Sprintf("COALESCE(NULLIF(TRIM(%s::text), '')::double precision, %s)", column, repair.defaultExpr), nil
	case "BOOLEAN":
		return fmt.Sprintf(`CASE
			WHEN %s IS NULL THEN %s
			WHEN LOWER(TRIM(%s::text)) IN ('true', 't', '1', 'yes', 'on') THEN TRUE
			WHEN LOWER(TRIM(%s::text)) IN ('false', 'f', '0', 'no', 'off', '') THEN FALSE
			ELSE %s::boolean
		END`, column, repair.defaultExpr, column, column, column), nil
	case "TIMESTAMPTZ":
		return fmt.Sprintf("COALESCE(NULLIF(TRIM(%s::text), '')::timestamptz, %s)", column, repair.defaultExpr), nil
	default:
		return "", fmt.Errorf("unsupported postgres repair type %q", repair.dataType)
	}
}

func postgresInformationSchemaType(dataType string) string {
	switch strings.ToUpper(strings.TrimSpace(dataType)) {
	case "TIMESTAMPTZ":
		return "timestamp with time zone"
	default:
		return strings.ToLower(strings.TrimSpace(dataType))
	}
}

func postgresQuoteIdent(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
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
		if err := backfillVoiceAgentSessionChildren(ctx, db, "sqlite", session.ID, session); err != nil {
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
		if err := backfillVoiceAgentSessionChildren(ctx, db, "postgres", session.ID, session); err != nil {
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
