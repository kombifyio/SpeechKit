package claimstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const schemaVersion = 2

var (
	ErrSchemaTooNew = errors.New("claimstore: database schema is newer than this binary")
	ErrSchema       = errors.New("claimstore: invalid database schema")
)

const schemaV1 = `
CREATE TABLE IF NOT EXISTS ha_command_claims (
    paired_device_id TEXT NOT NULL
        CHECK(length(paired_device_id) BETWEEN 1 AND 128),
    request_id TEXT NOT NULL
        CHECK(length(request_id) = 36),
    request_schema TEXT NOT NULL
        CHECK(request_schema = 'speechkit.ha_assist.v1'),
    request_digest BLOB NOT NULL
        CHECK(length(request_digest) = 32),
    state TEXT NOT NULL
        CHECK(state IN ('claimed', 'completed', 'indeterminate')),
    result_digest BLOB
        CHECK(result_digest IS NULL OR length(result_digest) = 32),
    outcome TEXT
        CHECK(outcome IS NULL OR outcome IN ('success', 'rejected')),
    speech_text TEXT NOT NULL DEFAULT ''
        CHECK(length(speech_text) <= 8192),
    language TEXT NOT NULL DEFAULT ''
        CHECK(length(language) <= 64),
    response_type TEXT NOT NULL DEFAULT ''
        CHECK(length(response_type) <= 64),
    error_code TEXT NOT NULL DEFAULT ''
        CHECK(length(error_code) <= 64),
    reason_code TEXT NOT NULL DEFAULT ''
        CHECK(length(reason_code) <= 64),
    retryable INTEGER NOT NULL DEFAULT 0
        CHECK(retryable IN (0, 1)),
    action_executed TEXT NOT NULL DEFAULT ''
        CHECK(action_executed IN ('', 'yes', 'no', 'not_applicable', 'unknown')),
    claimed_at_ms INTEGER NOT NULL,
    terminal_at_ms INTEGER,
    expires_at_ms INTEGER NOT NULL,
    PRIMARY KEY (paired_device_id, request_id),
    CHECK (
        (state = 'claimed'
            AND result_digest IS NULL
            AND terminal_at_ms IS NULL)
        OR
        (state = 'completed'
            AND result_digest IS NOT NULL
            AND terminal_at_ms IS NOT NULL)
        OR
        (state = 'indeterminate'
            AND result_digest IS NULL
            AND terminal_at_ms IS NOT NULL
            AND action_executed = 'unknown')
    )
) WITHOUT ROWID;

CREATE INDEX IF NOT EXISTS idx_ha_command_claims_expiry
    ON ha_command_claims(expires_at_ms);
`

const schemaV2Migration = `
ALTER TABLE ha_command_claims
    ADD COLUMN conversation_id TEXT NOT NULL DEFAULT ''
        CHECK(length(conversation_id) <= 256);
`

var requiredColumns = []string{
	"paired_device_id",
	"request_id",
	"request_schema",
	"request_digest",
	"state",
	"result_digest",
	"outcome",
	"conversation_id",
	"speech_text",
	"language",
	"response_type",
	"error_code",
	"reason_code",
	"retryable",
	"action_executed",
	"claimed_at_ms",
	"terminal_at_ms",
	"expires_at_ms",
}

func migrateAndValidate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin claim schema migration: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit.

	var version int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read claim schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("%w: found %d, support up to %d", ErrSchemaTooNew, version, schemaVersion)
	}
	switch version {
	case 0:
		if _, err := tx.ExecContext(ctx, schemaV1); err != nil {
			return fmt.Errorf("create claim schema v1: %w", err)
		}
		fallthrough
	case 1:
		if err := migrateV1ToV2(ctx, tx); err != nil {
			return err
		}
	case schemaVersion:
	default:
		return fmt.Errorf("%w: unsupported version %d", ErrSchema, version)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit claim schema migration: %w", err)
	}

	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check(1)`).Scan(&integrity); err != nil {
		return fmt.Errorf("run claim database quick check: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("%w: quick_check returned %q", ErrSchema, integrity)
	}
	if err := validateColumns(ctx, db); err != nil {
		return err
	}
	var indexCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_ha_command_claims_expiry'`,
	).Scan(&indexCount); err != nil {
		return fmt.Errorf("validate claim expiry index: %w", err)
	}
	if indexCount != 1 {
		return fmt.Errorf("%w: expiry index is missing", ErrSchema)
	}
	return nil
}

type v1CompletedRow struct {
	pairedDeviceID string
	requestID      string
	resultDigest   []byte
	result         CompletedResult
}

func migrateV1ToV2(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT paired_device_id, request_id, result_digest, outcome, speech_text, language,
			response_type, error_code, reason_code, retryable, action_executed
		FROM ha_command_claims
		WHERE state = ?`, stateCompleted)
	if err != nil {
		return fmt.Errorf("%w: inspect v1 completed results: %w", ErrSchema, err)
	}

	completed := make([]v1CompletedRow, 0)
	for rows.Next() {
		var row v1CompletedRow
		var outcome sql.NullString
		var retryable int
		if err := rows.Scan(
			&row.pairedDeviceID,
			&row.requestID,
			&row.resultDigest,
			&outcome,
			&row.result.SpeechText,
			&row.result.Language,
			&row.result.ResponseType,
			&row.result.ErrorCode,
			&row.result.ReasonCode,
			&retryable,
			&row.result.ActionExecuted,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("%w: scan v1 completed result: %w", ErrSchema, err)
		}
		if !outcome.Valid {
			_ = rows.Close()
			return fmt.Errorf("%w: v1 completed result has no outcome", ErrSchema)
		}
		row.result.Outcome = outcome.String
		row.result.Retryable = retryable == 1
		normalized, err := normalizeCompletedResult(row.result)
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("%w: v1 completed result is invalid", ErrSchema)
		}
		row.result = normalized
		expected := digestCompletedResultV1(normalized)
		if !sameDigest(row.resultDigest, expected) {
			_ = rows.Close()
			return fmt.Errorf("%w: v1 completed result digest mismatch", ErrSchema)
		}
		completed = append(completed, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("%w: inspect v1 completed results: %w", ErrSchema, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("%w: close v1 result scan: %w", ErrSchema, err)
	}

	if _, err := tx.ExecContext(ctx, schemaV2Migration); err != nil {
		return fmt.Errorf("%w: add v2 conversation id column: %w", ErrSchema, err)
	}
	for _, row := range completed {
		digest := digestCompletedResult(row.result)
		updated, err := tx.ExecContext(ctx, `
			UPDATE ha_command_claims
			SET result_digest = ?
			WHERE paired_device_id = ? AND request_id = ? AND state = ? AND result_digest = ?`,
			digest[:], row.pairedDeviceID, row.requestID, stateCompleted, row.resultDigest)
		if err != nil {
			return fmt.Errorf("%w: rewrite v2 completed result digest: %w", ErrSchema, err)
		}
		count, err := updated.RowsAffected()
		if err != nil || count != 1 {
			return fmt.Errorf("%w: v2 digest rewrite updated %d rows", ErrSchema, count)
		}
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = 2`); err != nil {
		return fmt.Errorf("record claim schema v2: %w", err)
	}
	return nil
}

func validateColumns(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(ha_command_claims)`)
	if err != nil {
		return fmt.Errorf("inspect claim table: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows error checked below.

	found := make(map[string]bool, len(requiredColumns))
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan claim table definition: %w", err)
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect claim table definition: %w", err)
	}
	for _, column := range requiredColumns {
		if !found[column] {
			return fmt.Errorf("%w: required column %q is missing", ErrSchema, column)
		}
	}
	return nil
}
