package claimstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSchemaVersionAndShapeFailClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t.Run("newer version", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "newer.db")
		seedSQLite(t, path, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion+1))
		if _, err := Open(ctx, Options{Path: path}); !errors.Is(err, ErrSchemaTooNew) {
			t.Fatalf("Open newer schema error = %v, want ErrSchemaTooNew", err)
		}
	})
	t.Run("missing table", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.db")
		seedSQLite(t, path, `PRAGMA user_version = 1`)
		if _, err := Open(ctx, Options{Path: path}); !errors.Is(err, ErrSchema) {
			t.Fatalf("Open missing schema error = %v, want ErrSchema", err)
		}
	})
	t.Run("corrupt file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "corrupt.db")
		if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
			t.Fatal(err)
		}
		if ledger, err := Open(ctx, Options{Path: path}); err == nil {
			_ = ledger.Close()
			t.Fatal("Open corrupt database unexpectedly succeeded")
		}
	})
}

func TestSchemaV1CompletedResultsMigrateToV2(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	path := filepath.Join(t.TempDir(), "claims-v1.db")
	key := Key{PairedDeviceID: "paired-v1-migration", RequestID: testUUIDV7(now, 61)}
	requestDigest := testDigest(61)
	result := CompletedResult{
		Outcome:        OutcomeSuccess,
		SpeechText:     "The migrated light is off.",
		Language:       "en-US",
		ResponseType:   "action_done",
		ActionExecuted: ActionExecutedYes,
	}
	seedV1CompletedClaim(t, path, key, requestDigest, result, now, false)

	ledger, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open migrated v1 ledger: %v", err)
	}
	defer ledger.Close() //nolint:errcheck // test cleanup

	var version int
	if err := ledger.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != schemaVersion {
		t.Fatalf("migrated schema version = %d, want %d", version, schemaVersion)
	}
	decision, err := ledger.Lookup(ctx, key, now.Add(2*time.Second))
	if err != nil || decision.Disposition != ReplayCompleted || decision.Result == nil {
		t.Fatalf("migrated Lookup = %#v, %v; want replay", decision, err)
	}
	if *decision.Result != result {
		t.Fatalf("migrated result = %#v, want %#v", *decision.Result, result)
	}
	var storedDigest []byte
	if err := ledger.db.QueryRowContext(ctx, `
		SELECT result_digest FROM ha_command_claims
		WHERE paired_device_id = ? AND request_id = ?`, key.PairedDeviceID, key.RequestID).Scan(&storedDigest); err != nil {
		t.Fatal(err)
	}
	if expected := digestCompletedResult(result); !sameDigest(storedDigest, expected) {
		t.Fatal("migrated completed result was not rebound to the v2 digest")
	}
}

func TestSchemaV1MigrationRejectsTamperedCompletedResults(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Millisecond)
	path := filepath.Join(t.TempDir(), "claims-v1-tampered.db")
	key := Key{PairedDeviceID: "paired-v1-tampered", RequestID: testUUIDV7(now, 62)}
	result := CompletedResult{
		Outcome:        OutcomeSuccess,
		SpeechText:     "The original result.",
		Language:       "en-US",
		ResponseType:   "action_done",
		ActionExecuted: ActionExecutedYes,
	}
	seedV1CompletedClaim(t, path, key, testDigest(62), result, now, true)

	if ledger, err := Open(context.Background(), Options{Path: path}); !errors.Is(err, ErrSchema) {
		if ledger != nil {
			_ = ledger.Close()
		}
		t.Fatalf("Open tampered v1 ledger error = %v, want ErrSchema", err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("schema version after rejected migration = %d, want 1", version)
	}
	rows, err := db.Query(`PRAGMA table_info(ha_command_claims)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close() //nolint:errcheck // test cleanup
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "conversation_id" {
			t.Fatal("rejected v1 migration left the v2 conversation_id column behind")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
