package claimstore

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
	"os"
	"strconv"
	"testing"
	"time"
)

func runCrashHelper() {
	path := os.Getenv("SPEECHKIT_CLAIMSTORE_CRASH_PATH")
	requestID := os.Getenv("SPEECHKIT_CLAIMSTORE_CRASH_REQUEST_ID")
	nowMillis, err := strconv.ParseInt(os.Getenv("SPEECHKIT_CLAIMSTORE_CRASH_NOW"), 10, 64)
	if err != nil {
		os.Exit(91)
	}
	ledger, err := Open(context.Background(), Options{Path: path})
	if err != nil {
		os.Exit(92)
	}
	decision, err := ledger.Claim(
		context.Background(),
		Key{PairedDeviceID: "paired-crash", RequestID: requestID},
		testDigest(7),
		time.UnixMilli(nowMillis).UTC(),
	)
	if err != nil || decision.Disposition != DispatchNew {
		os.Exit(93)
	}
	// Intentionally skip Close and all test cleanup to simulate a process
	// disappearing immediately after the durable claim commit.
	os.Exit(0)
}

func openTestLedger(t *testing.T, path string, overrides Options) *Ledger {
	t.Helper()
	overrides.Path = path
	ledger, err := Open(context.Background(), overrides)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	t.Cleanup(func() {
		_ = ledger.Close()
	})
	return ledger
}

func testDigest(seed byte) Digest {
	var digest Digest
	for index := range digest {
		digest[index] = seed + byte(index) + 1
	}
	return digest
}

func testUUIDV7(at time.Time, entropy byte) string {
	var id uuid.UUID
	milliseconds := uint64(at.UTC().UnixMilli())
	id[0] = byte(milliseconds >> 40)
	id[1] = byte(milliseconds >> 32)
	id[2] = byte(milliseconds >> 24)
	id[3] = byte(milliseconds >> 16)
	id[4] = byte(milliseconds >> 8)
	id[5] = byte(milliseconds)
	for index := 6; index < len(id); index++ {
		id[index] = entropy + byte(index)
	}
	id[6] = (id[6] & 0x0f) | 0x70
	id[8] = (id[8] & 0x3f) | 0x80
	return id.String()
}

func seedSQLite(t *testing.T, path, statement string) {
	t.Helper()
	if _, err := prepareDatabaseFile(path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck // test cleanup.
	if _, err := db.Exec(statement); err != nil {
		t.Fatal(err)
	}
}

func seedV1CompletedClaim(t *testing.T, path string, key Key, requestDigest Digest, result CompletedResult, now time.Time, tamper bool) {
	t.Helper()
	if result.ConversationID != "" {
		t.Fatal("v1 migration fixture cannot contain a conversation id")
	}
	result, err := normalizeCompletedResult(result)
	if err != nil {
		t.Fatalf("normalize v1 result fixture: %v", err)
	}
	if _, err := prepareDatabaseFile(path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit
	if _, err := tx.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	legacyDigest := digestCompletedResultV1(result)
	retryable := 0
	if result.Retryable {
		retryable = 1
	}
	if _, err := tx.Exec(`
		INSERT INTO ha_command_claims (
			paired_device_id, request_id, request_schema, request_digest, state,
			result_digest, outcome, speech_text, language, response_type, error_code,
			reason_code, retryable, action_executed, claimed_at_ms, terminal_at_ms, expires_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		key.PairedDeviceID,
		key.RequestID,
		requestSchema,
		requestDigest[:],
		stateCompleted,
		legacyDigest[:],
		result.Outcome,
		result.SpeechText,
		result.Language,
		result.ResponseType,
		result.ErrorCode,
		result.ReasonCode,
		retryable,
		result.ActionExecuted,
		now.UnixMilli(),
		now.Add(time.Second).UnixMilli(),
		now.Add(time.Hour).UnixMilli(),
	); err != nil {
		t.Fatal(err)
	}
	if tamper {
		if _, err := tx.Exec(`
			UPDATE ha_command_claims SET speech_text = ?
			WHERE paired_device_id = ? AND request_id = ?`,
			"tampered result", key.PairedDeviceID, key.RequestID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
