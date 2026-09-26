package claimstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Prune removes at most limit claims whose retention window elapsed. Safety
// additionally depends on Claim rejecting stale UUIDv7 request ids.
func (l *Ledger) Prune(ctx context.Context, now time.Time, limit int) (int64, error) {
	if now.IsZero() || limit < 1 {
		return 0, ErrInvalidOptions
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin claim prune transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit.
	deleted, err := pruneTx(ctx, tx, now.UTC().UnixMilli(), limit)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit claim pruning: %w", err)
	}
	return deleted, nil
}

func pruneTx(ctx context.Context, tx *sql.Tx, nowMillis int64, limit int) (int64, error) {
	result, err := tx.ExecContext(ctx, `
		WITH expired AS (
			SELECT paired_device_id, request_id
			FROM ha_command_claims
			WHERE expires_at_ms <= ?
			ORDER BY expires_at_ms, paired_device_id, request_id
			LIMIT ?
		)
		DELETE FROM ha_command_claims
		WHERE (paired_device_id, request_id) IN (
			SELECT paired_device_id, request_id FROM expired
		)`, nowMillis, limit)
	if err != nil {
		return 0, fmt.Errorf("delete expired claims: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted claims: %w", err)
	}
	return deleted, nil
}
