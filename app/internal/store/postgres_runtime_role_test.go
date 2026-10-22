package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/app/internal/testutil"
)

// A runtime-role server: the migration step alone changes the schema, the
// serving process never runs DDL, and it refuses to start on a role that
// could escape row-level security. SPEECHKIT_POSTGRES_TEST_DSN must name a role that may
// create roles and databases; each test makes its own database and roles.

func runtimeRoleAdmin(t *testing.T) (adminDSN string, exec func(string)) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("SPEECHKIT_POSTGRES_TEST_DSN"))
	if dsn == "" {
		testutil.SkipOrFailExplicitMissingConfig(t, "SPEECHKIT_POSTGRES_TEST_DSN", "Set it to a role that may create roles and databases to run the runtime-role tests.")
	}
	root, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	name := fmt.Sprintf("sk_runtime_%d_%d", os.Getpid(), time.Now().UnixNano())
	if _, err := root.Exec(`CREATE DATABASE ` + name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	adminDSN = u.String()
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	return adminDSN, func(stmt string) {
		t.Helper()
		if _, err := admin.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func dsnAs(t *testing.T, dsn, role string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(role, "runtime")
	return u.String()
}

func freshRole(prefix string) string {
	return fmt.Sprintf("%s_%d_%d", prefix, os.Getpid(), time.Now().UnixNano()%1_000_000_000)
}

func openRuntime(t *testing.T, dsn string) error {
	t.Helper()
	t.Setenv("APPDATA", t.TempDir())
	s, err := NewPostgresStore(StoreConfig{Backend: "postgres", PostgresDSN: dsn, PostgresRuntimeRole: true})
	if err == nil {
		_ = s.Close()
	}
	return err
}

func TestRuntimeRoleServerNeverMigrates(t *testing.T) {
	admin, exec := runtimeRoleAdmin(t)
	role := freshRole("sk_app")
	exec(`CREATE ROLE ` + role + ` LOGIN PASSWORD 'runtime' NOSUPERUSER NOBYPASSRLS`)

	err := openRuntime(t, dsnAs(t, admin, role))
	if err == nil || !strings.Contains(err.Error(), "speechkit-server migrate") {
		t.Fatalf("a runtime-role server started on an unmigrated database or did not name the migration: %v", err)
	}
	db, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var ledger sql.NullString
	if err := db.QueryRow(`SELECT to_regclass('schema_migrations')::text`).Scan(&ledger); err != nil || ledger.Valid {
		t.Fatalf("the serving process ran DDL: %v %v", ledger, err)
	}
}

func TestRuntimeRoleServerStartsOnlyAsASafeRole(t *testing.T) {
	admin, exec := runtimeRoleAdmin(t)
	role := freshRole("sk_app")
	exec(`CREATE ROLE ` + role + ` LOGIN PASSWORD 'runtime' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE`)
	if err := MigratePostgres(context.Background(), admin, role); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := openRuntime(t, dsnAs(t, admin, role)); err != nil {
		t.Fatalf("the migrated runtime role was refused: %v", err)
	}

	owner, member, truncater := freshRole("sk_owner"), freshRole("sk_member"), freshRole("sk_trunc")
	for _, r := range []string{owner, member, truncater} {
		exec(`CREATE ROLE ` + r + ` LOGIN PASSWORD 'runtime'`)
		exec(`GRANT USAGE ON SCHEMA public TO ` + r)
		exec(`GRANT SELECT ON schema_migrations TO ` + r)
	}
	exec(`ALTER TABLE transcriptions OWNER TO ` + owner)
	exec(`GRANT ` + role + ` TO ` + member)
	exec(`GRANT TRUNCATE ON quick_notes TO ` + truncater)
	for name, dsn := range map[string]string{
		"migration role (owner of everything)": admin,
		"table owner":                          dsnAs(t, admin, owner),
		"member of another role":               dsnAs(t, admin, member),
		"TRUNCATE grantee":                     dsnAs(t, admin, truncater),
	} {
		if err := openRuntime(t, dsn); !errors.Is(err, ErrPostgresRuntimeRole) {
			t.Errorf("%s: started or failed for another reason: %v", name, err)
		}
	}

	// The runtime role reads and writes rows but cannot change the schema,
	// truncate, or change role.
	db, err := sql.Open("pgx", dsnAs(t, admin, role))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`SELECT count(*) FROM quick_notes`); err != nil {
		t.Fatalf("the runtime role cannot read its rows: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE escape (id int)`,
		`TRUNCATE quick_notes CASCADE`,
		`ALTER TABLE quick_notes ADD COLUMN escape int`,
		`DELETE FROM schema_migrations`,
		`SET ROLE ` + owner,
	} {
		if _, err := db.Exec(stmt); err == nil {
			t.Errorf("the runtime role may run %q", stmt)
		}
	}
}
