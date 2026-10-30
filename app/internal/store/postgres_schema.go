package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PostgresSchemaMigration describes one step of the native migration chain.
type PostgresSchemaMigration struct {
	ID   string `json:"id"`
	File string `json:"file"`
	Body []byte `json:"-"`
}

// PostgresSchemaMigrations exposes SQL steps and code-driven steps in their
// actual execution order, including steps without a separate SQL file.
func PostgresSchemaMigrations() []PostgresSchemaMigration {
	var result []PostgresSchemaMigration
	for _, migration := range postgresMigrations() {
		id := strings.TrimPrefix(migration.version, "postgres:")
		file, body := id+".sql", migration.statement
		if body == "" {
			file = id + ".go"
			body = "package store\n\n// " + migration.version + " is executed by the native postgresMigrations chain.\n"
		}
		result = append(result, PostgresSchemaMigration{ID: id, File: file, Body: []byte(body)})
	}
	return result
}

// ExportPostgresSchema writes the native mixed chain without a connection.
// An empty destination prevents stale files from entering a later export.
func ExportPostgresSchema(directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("store: schema export directory must be empty")
	}
	migrations := PostgresSchemaMigrations()
	for _, migration := range migrations {
		if err := os.WriteFile(filepath.Join(directory, migration.File), migration.Body, 0o600); err != nil {
			return err
		}
	}
	body, err := json.MarshalIndent(migrations, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "native-chain.json"), append(body, '\n'), 0o600)
}

// ReadPostgresSchemaLedger reads IDs from the native migration ledger.
func ReadPostgresSchemaLedger(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("store: read native schema ledger: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := []string{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		result = append(result, strings.TrimPrefix(version, "postgres:"))
	}
	return result, rows.Err()
}

// ApplyPostgresSchemaPrefix applies an admitted pending prefix through the
// existing native steps. Recorded steps are never replayed by this lifecycle.
func ApplyPostgresSchemaPrefix(ctx context.Context, db *sql.DB, admitted []string, indexReadback map[string][]string) ([]string, error) {
	ledger, err := ReadPostgresSchemaLedger(ctx, db)
	if err != nil {
		return nil, err
	}
	done := map[string]bool{}
	for _, id := range ledger {
		done[id] = true
	}
	var pending []storeMigration
	for _, migration := range postgresMigrations() {
		if !done[strings.TrimPrefix(migration.version, "postgres:")] {
			pending = append(pending, migration)
		}
	}
	if len(admitted) > len(pending) {
		return nil, errors.New("store: admitted prefix exceeds the pending native chain")
	}
	for i, id := range admitted {
		if pending[i].version != "postgres:"+id {
			return nil, errors.New("store: admitted prefix differs from the pending native chain")
		}
	}
	applied := []string{}
	// An entirely held publication is a ledger readback, with no DDL even on
	// databases that predate the durable operation table.
	if len(admitted) == 0 {
		if err := refuseUnfinishedPostgresOperations(ctx, db); err != nil {
			return nil, err
		}
		return applied, nil
	}
	if err := prepareHostedPostgresMigrations(ctx, db); err != nil {
		return nil, err
	}
	for i, id := range admitted {
		if err := applyHostedPostgresMigration(ctx, db, pending[i], indexReadback[id]); err != nil {
			return applied, fmt.Errorf("store: publish native schema %s: %w", id, err)
		}
		applied = append(applied, id)
	}
	return applied, nil
}

// PostgresSchemaOperation retains uncertain native effects across process exits.
// A started operation is never automatically replayed or marked complete.
type PostgresSchemaOperation struct {
	Version string `json:"version"`
	Started string `json:"started_at"`
}

// InspectPostgresSchemaOperations is read-only, including before first bootstrap.
func InspectPostgresSchemaOperations(ctx context.Context, db *sql.DB) ([]PostgresSchemaOperation, error) {
	result := []PostgresSchemaOperation{}
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('schema_migration_operations') IS NOT NULL`).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return result, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT version, started_at::text FROM schema_migration_operations WHERE completed_at IS NULL ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var operation PostgresSchemaOperation
		if err := rows.Scan(&operation.Version, &operation.Started); err != nil {
			return nil, err
		}
		result = append(result, operation)
	}
	return result, rows.Err()
}

func refuseUnfinishedPostgresOperations(ctx context.Context, db *sql.DB) error {
	operations, err := InspectPostgresSchemaOperations(ctx, db)
	if err != nil {
		return err
	}
	if len(operations) != 0 {
		return fmt.Errorf("store: hosted migration %s has uncertain effects since %s; inspect native state before repair", operations[0].Version, operations[0].Started)
	}
	return nil
}

func prepareHostedPostgresMigrations(ctx context.Context, db *sql.DB) error {
	if err := refuseUnfinishedPostgresOperations(ctx, db); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migration_operations (
 version TEXT PRIMARY KEY, started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), completed_at TIMESTAMPTZ
)`)
	return err
}

// Refuse transaction control conservatively, even inside comments or literals.
// In particular, a classifier-admitted outer COMMIT must not escape our ledger
// transaction. Native code steps use the durable nontransactional seam instead.
var hostedTransactionControl = regexp.MustCompile(`(?i)\b(begin|start|commit|end|rollback|abort|savepoint|release|prepare)\b`)

func applyHostedPostgresMigration(ctx context.Context, db *sql.DB, migration storeMigration, indexes []string) error {
	if err := refuseUnfinishedPostgresOperations(ctx, db); err != nil {
		return err
	}
	if migration.statement != "" && hostedTransactionControl.MatchString(migration.statement) {
		return errors.New("store: hosted SQL contains transaction control; keep the native SQL step inside its ledger transaction")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Serialize admission with another hosted caller of this same native step.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, migration.version); err != nil {
		return err
	}
	var recorded bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, migration.version).Scan(&recorded); err != nil {
		return err
	}
	if recorded {
		return nil
	}
	// Plain native SQL and its ledger row share a commit. If Commit's result is
	// lost, the next attempt observes either both effects or neither.
	if migration.statement != "" && len(indexes) == 0 {
		if _, err := tx.ExecContext(ctx, migration.statement); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, migration.version); err != nil {
			return err
		}
		return tx.Commit()
	}
	// Code-driven steps may use multiple connections; concurrent indexes cannot
	// run in a transaction. Commit their unique marker before any native effect.
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migration_operations (version) VALUES ($1)`, migration.version); err != nil {
		return fmt.Errorf("store: native operation already exists or could not be durably started: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := migration.run(ctx, db); err != nil {
		return fmt.Errorf("store: native operation remains uncertain: %w", err)
	}
	for _, index := range indexes {
		var valid bool
		if err := db.QueryRowContext(ctx, `SELECT indisvalid FROM pg_index WHERE indexrelid = to_regclass($1)`, index).Scan(&valid); err != nil || !valid {
			return fmt.Errorf("store: native operation remains uncertain: index %s was not verified", index)
		}
	}
	completion, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = completion.Rollback() }()
	if _, err := completion.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, migration.version); err != nil {
		return err
	}
	if _, err := completion.ExecContext(ctx, `UPDATE schema_migration_operations SET completed_at = NOW() WHERE version = $1 AND completed_at IS NULL`, migration.version); err != nil {
		return err
	}
	return completion.Commit()
}
