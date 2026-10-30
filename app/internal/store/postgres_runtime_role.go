package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

// A runtime-role server (PostgresRuntimeRole) never runs DDL and refuses to
// start unless its login role cannot escape row-level security. Only the
// migration step (MigratePostgres, `speechkit-server migrate`) changes the
// schema, with the migration role. A server that owns its database keeps
// migrating at start (PostgresRuntimeRole off).

// ErrPostgresRuntimeRole is wrapped by every *PostgresRuntimeRoleError.
var ErrPostgresRuntimeRole = errors.New("store: postgres runtime role assertion failed")

// PostgresRuntimeRoleError lists every way the connected role breaks the
// isolation invariant. A server that receives it must refuse to start.
type PostgresRuntimeRoleError struct {
	Role       string
	Violations []string
}

func (e *PostgresRuntimeRoleError) Error() string {
	return fmt.Sprintf("store: postgres role %q is not a safe runtime role: %s", e.Role, strings.Join(e.Violations, "; "))
}

func (e *PostgresRuntimeRoleError) Unwrap() error { return ErrPostgresRuntimeRole }

// MigratePostgres is the governed migration step: it applies the ordered
// Postgres chain with the migration role's DSN and, when runtimeRole is set,
// grants that role exactly the row privileges it serves with. Run it on the
// non-production database first, then on production.
func MigratePostgres(ctx context.Context, dsn, runtimeRole string) error {
	return migratePostgres(ctx, dsn, runtimeRole, false, nil)
}

// MigrateHostedPostgres uses the same native chain for hosted publication and
// applies only unrecorded steps. Local callers retain MigratePostgres repair.
func MigrateHostedPostgres(ctx context.Context, dsn, runtimeRole string, indexReadback map[string][]string) error {
	return migratePostgres(ctx, dsn, runtimeRole, true, indexReadback)
}

func migratePostgres(ctx context.Context, dsn, runtimeRole string, pendingOnly bool, indexReadback map[string][]string) error {
	if strings.TrimSpace(dsn) == "" {
		return errors.New("store: migrate needs the migration role's postgres DSN")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	if err := runPostgresMigrationChain(ctx, db, pendingOnly, indexReadback); err != nil {
		return err
	}
	if runtimeRole == "" {
		return nil
	}
	return grantPostgresRuntimeRole(ctx, db, runtimeRole)
}

var runtimeRolePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// grantPostgresRuntimeRole gives the runtime role row access to every table
// and sequence of the migration role's schema and nothing else: no CREATE,
// TRUNCATE, TRIGGER or REFERENCES, and only SELECT on the migration ledger.
func grantPostgresRuntimeRole(ctx context.Context, db *sql.DB, role string) error {
	if !runtimeRolePattern.MatchString(role) {
		return fmt.Errorf("store: runtime role %q is not a plain lower-case role name", role)
	}
	var exists, isSelf bool
	var schema string
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1), $1 = current_user, current_schema()`, role).Scan(&exists, &isSelf, &schema); err != nil {
		return fmt.Errorf("store: look up runtime role: %w", err)
	}
	if !exists {
		return fmt.Errorf("store: runtime role %q does not exist; create it (LOGIN NOSUPERUSER NOBYPASSRLS, owning nothing) first", role)
	}
	if isSelf {
		return errors.New("store: the migration role cannot be the runtime role")
	}
	quote := func(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }
	s, r := quote(schema), quote(role)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin runtime grants: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`REVOKE CREATE ON SCHEMA ` + s + ` FROM PUBLIC`,
		`REVOKE CREATE ON SCHEMA ` + s + ` FROM ` + r,
		`GRANT USAGE ON SCHEMA ` + s + ` TO ` + r,
		`REVOKE ALL ON ALL TABLES IN SCHEMA ` + s + ` FROM ` + r,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ` + s + ` TO ` + r,
		`REVOKE INSERT, UPDATE, DELETE ON TABLE ` + s + `.schema_migrations FROM ` + r,
		`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA ` + s + ` TO ` + r,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("store: runtime grants: %w", err)
		}
	}
	var hostedOperations bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('schema_migration_operations') IS NOT NULL`).Scan(&hostedOperations); err != nil {
		return err
	}
	if hostedOperations {
		if _, err := tx.ExecContext(ctx, `REVOKE INSERT, UPDATE, DELETE ON TABLE `+s+`.schema_migration_operations FROM `+r); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PostgresSchemaFloor is the newest Postgres migration this code needs (its
// schema floor). Raise it in the change that adds a migration the code depends
// on. A runtime-role server requires every migration of the chain up to and
// including the floor; a newer schema always passes, so a binary and its
// schema roll out in any order.
const PostgresSchemaFloor = "postgres:029_notebook_site"

// SchemaStatus is the schema block of /healthz: the floor, the newest
// migration this binary carries and the newest recorded one, as numbers.
// AppliedMax is nil when the ledger could not be read.
type SchemaStatus struct {
	Floor       int  `json:"floor"`
	EmbeddedMax int  `json:"embedded_max"`
	AppliedMax  *int `json:"applied_max"`
}

// migrationNumber is the sequence number of a "postgres:NNN_name" version.
func migrationNumber(version string) int {
	digits := strings.TrimPrefix(version, "postgres:")
	n := 0
	for _, c := range digits {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// readPostgresLedger returns the recorded versions and the versions whose
// hosted migration operation started but never completed (dirty).
func readPostgresLedger(ctx context.Context, db *sql.DB) (recorded, dirty map[string]bool, err error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, nil, fmt.Errorf("store: read schema_migrations (run `speechkit-server migrate` with the migration role first): %w", err)
	}
	defer func() { _ = rows.Close() }()
	recorded = map[string]bool{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, nil, fmt.Errorf("store: read schema_migrations: %w", err)
		}
		recorded[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("store: read schema_migrations: %w", err)
	}
	dirty = map[string]bool{}
	var operations bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('schema_migration_operations') IS NOT NULL`).Scan(&operations); err != nil {
		return nil, nil, fmt.Errorf("store: read schema_migration_operations: %w", err)
	}
	if operations {
		opRows, err := db.QueryContext(ctx, `SELECT version FROM schema_migration_operations WHERE completed_at IS NULL`)
		if err != nil {
			return nil, nil, fmt.Errorf("store: read schema_migration_operations: %w", err)
		}
		defer func() { _ = opRows.Close() }()
		for opRows.Next() {
			var version string
			if err := opRows.Scan(&version); err != nil {
				return nil, nil, fmt.Errorf("store: read schema_migration_operations: %w", err)
			}
			dirty[version] = true
		}
		if err := opRows.Err(); err != nil {
			return nil, nil, fmt.Errorf("store: read schema_migration_operations: %w", err)
		}
	}
	return recorded, dirty, nil
}

// verifyPostgresSchema is read-only: every migration of the chain up to and
// including PostgresSchemaFloor must be recorded and none of them may be left
// incomplete. A runtime-role server refuses a schema below its floor instead
// of migrating it. Recorded versions this binary does not know and chain
// migrations above the floor that are not recorded yet pass and are only
// logged.
func verifyPostgresSchema(ctx context.Context, db *sql.DB) error {
	recorded, dirty, err := readPostgresLedger(ctx, db)
	if err != nil {
		return err
	}
	var missing, pendingAbove []string
	known := map[string]bool{}
	reachedFloor := false
	for _, migration := range postgresMigrations() {
		known[migration.version] = true
		if reachedFloor {
			if !recorded[migration.version] {
				pendingAbove = append(pendingAbove, migration.version)
			}
			continue
		}
		if !recorded[migration.version] || dirty[migration.version] {
			missing = append(missing, migration.version)
		}
		reachedFloor = migration.version == PostgresSchemaFloor
	}
	if !reachedFloor {
		return fmt.Errorf("store: schema floor %s is not in this binary's migration chain", PostgresSchemaFloor)
	}
	if len(missing) > 0 {
		return fmt.Errorf("store: postgres schema lacks %s (schema floor %s); run `speechkit-server migrate` with the migration role first", strings.Join(missing, ", "), PostgresSchemaFloor)
	}
	var unknown []string
	for version := range recorded {
		if !known[version] {
			unknown = append(unknown, version)
		}
	}
	if len(pendingAbove) > 0 || len(unknown) > 0 {
		slog.InfoContext(ctx, "store: schema floor satisfied with a different schema", "floor", PostgresSchemaFloor,
			"pending_above_floor", pendingAbove, "recorded_unknown_to_binary", unknown)
	}
	return nil
}

// SchemaStatus reports the schema block for /healthz. The ledger is read on
// each call with a short timeout; a failed read reports applied_max null.
func (s *PostgresStore) SchemaStatus(ctx context.Context) SchemaStatus {
	status := SchemaStatus{Floor: migrationNumber(PostgresSchemaFloor)}
	for _, migration := range postgresMigrations() {
		status.EmbeddedMax = max(status.EmbeddedMax, migrationNumber(migration.version))
	}
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(readCtx, `SELECT version FROM schema_migrations WHERE version LIKE 'postgres:%'`)
	if err != nil {
		return status
	}
	defer func() { _ = rows.Close() }()
	applied := 0
	for rows.Next() {
		var version string
		if rows.Scan(&version) != nil {
			return status
		}
		applied = max(applied, migrationNumber(version))
	}
	if rows.Err() == nil {
		status.AppliedMax = &applied
	}
	return status
}

// userSchemas filters out the catalogs, TOAST and temporary schemas.
const userSchemas = `n.nspname !~ '^pg_' AND n.nspname <> 'information_schema'`

// assertPostgresRuntimeRole is the mandatory boot check of a runtime-role
// server: NOSUPERUSER, NOBYPASSRLS,
// no CREATEROLE, CREATEDB or REPLICATION, owns no database, schema, table,
// sequence, view or function, has no CREATE on the database or any schema, is
// a member of no role (no SET ROLE) and no SET ROLE is active, has no TRUNCATE
// or TRIGGER on any table, and no policy in the database reads the retired
// app.service_scope escape.
func assertPostgresRuntimeRole(ctx context.Context, db *sql.DB) error {
	var (
		current, session                          string
		super, bypass, createRole, createDB, repl bool
		ownsDB, dbCreate                          bool
	)
	if err := db.QueryRowContext(ctx, `
		SELECT current_user, session_user, r.rolsuper, r.rolbypassrls, r.rolcreaterole,
		       r.rolcreatedb, r.rolreplication, d.datdba = r.oid,
		       has_database_privilege(current_database(), 'CREATE')
		FROM pg_roles r, pg_database d
		WHERE r.rolname = session_user AND d.datname = current_database()`).Scan(
		&current, &session, &super, &bypass, &createRole, &createDB, &repl, &ownsDB, &dbCreate); err != nil {
		return fmt.Errorf("store: read postgres role attributes: %w", err)
	}
	var v []string
	flag := func(bad bool, what string) {
		if bad {
			v = append(v, what)
		}
	}
	flag(current != session, fmt.Sprintf("SET ROLE is active (session %s, current %s)", session, current))
	flag(super, "is SUPERUSER")
	flag(bypass, "has BYPASSRLS")
	flag(createRole, "has CREATEROLE")
	flag(createDB, "has CREATEDB")
	flag(repl, "has REPLICATION")
	flag(ownsDB, "owns the database")
	flag(dbCreate, "has CREATE on the database")
	lists := []struct{ what, query string }{
		{"is a member of ", `
			SELECT g.rolname FROM pg_auth_members m
			JOIN pg_roles g ON g.oid = m.roleid
			JOIN pg_roles u ON u.oid = m.member
			WHERE u.rolname = session_user ORDER BY 1`},
		{"owns ", `
			SELECT o.label FROM (
				SELECT format('%s %I.%I', CASE c.relkind WHEN 'S' THEN 'sequence'
				       WHEN 'v' THEN 'view' WHEN 'm' THEN 'materialized view'
				       WHEN 'f' THEN 'foreign table' ELSE 'table' END,
				       n.nspname, c.relname) AS label, c.relowner AS owner
				FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S') AND ` + userSchemas + `
				UNION ALL
				SELECT format('schema %I', n.nspname), n.nspowner
				FROM pg_namespace n WHERE ` + userSchemas + `
				UNION ALL
				SELECT format('function %I.%I', n.nspname, p.proname), p.proowner
				FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
				WHERE ` + userSchemas + `
			) o
			WHERE pg_has_role(session_user, o.owner, 'MEMBER')
			ORDER BY 1 LIMIT 25`},
		{"has CREATE on schema ", `
			SELECT n.nspname FROM pg_namespace n
			WHERE ` + userSchemas + ` AND has_schema_privilege(session_user, n.oid, 'CREATE')
			ORDER BY 1`},
		{"has TRUNCATE or TRIGGER on ", `
			SELECT format('%I.%I', n.nspname, c.relname)
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relkind IN ('r', 'p') AND ` + userSchemas + `
			  AND (has_table_privilege(session_user, c.oid, 'TRUNCATE')
			       OR has_table_privilege(session_user, c.oid, 'TRIGGER'))
			ORDER BY 1 LIMIT 25`},
		{"RLS policies read the retired app.service_scope escape: ", `
			SELECT format('%I.%I/%I', schemaname, tablename, policyname) FROM pg_policies
			WHERE COALESCE(qual, '') ~* 'app\.service_scope' OR COALESCE(with_check, '') ~* 'app\.service_scope'
			ORDER BY 1 LIMIT 25`},
	}
	for _, l := range lists {
		values, err := queryStringList(ctx, db, l.query)
		if err != nil {
			return err
		}
		if len(values) > 0 {
			v = append(v, l.what+strings.Join(values, ", "))
		}
	}
	if len(v) > 0 {
		return &PostgresRuntimeRoleError{Role: session, Violations: v}
	}
	return nil
}

func queryStringList(ctx context.Context, db *sql.DB, query string) ([]string, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: role assertion query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("store: role assertion scan: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: role assertion rows: %w", err)
	}
	return out, nil
}
