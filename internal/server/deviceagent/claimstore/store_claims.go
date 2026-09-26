package claimstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Claim atomically resolves an existing request or creates a durable claim.
// DispatchNew is returned only after the insert commit succeeds.
func (l *Ledger) Claim(ctx context.Context, key Key, digest Digest, now time.Time) (Decision, error) {
	key, err := l.validateKey(key, now)
	if err != nil {
		return Decision{}, err
	}
	if isZeroDigest(digest) {
		return Decision{}, ErrInvalidDigest
	}

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return Decision{}, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit.

	existing, err := readClaim(ctx, tx, key)
	if err == nil {
		decision, resolveErr := resolveExisting(existing, digest)
		if resolveErr != nil {
			return Decision{}, resolveErr
		}
		if err := tx.Commit(); err != nil {
			return Decision{}, fmt.Errorf("commit existing claim read: %w", err)
		}
		return decision, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Decision{}, fmt.Errorf("read existing claim: %w", err)
	}

	if _, err := pruneTx(ctx, tx, now.UTC().UnixMilli(), l.options.CleanupBatch); err != nil {
		return Decision{}, fmt.Errorf("prune expired claims before insert: %w", err)
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ha_command_claims`).Scan(&count); err != nil {
		return Decision{}, fmt.Errorf("count durable claims: %w", err)
	}
	if count >= l.options.MaxEntries {
		return Decision{}, ErrCapacity
	}

	claimedAt := now.UTC().UnixMilli()
	expiresAt := now.UTC().Add(l.options.Retention).UnixMilli()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ha_command_claims (
			paired_device_id, request_id, request_schema, request_digest, state,
			claimed_at_ms, expires_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		key.PairedDeviceID,
		key.RequestID,
		requestSchema,
		digest[:],
		stateClaimed,
		claimedAt,
		expiresAt,
	); err != nil {
		return Decision{}, fmt.Errorf("insert durable claim: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Decision{}, fmt.Errorf("commit durable claim: %w", err)
	}
	return Decision{
		Disposition: DispatchNew,
		Handle:      Handle{key: key, digest: digest},
	}, nil
}

// Lookup returns the authenticated pairing's existing claim without accepting
// a caller-supplied digest. It is used only to authorize downstream work, such
// as TTS of the exact persisted HA response, and can never create or redispatch
// a command claim.
func (l *Ledger) Lookup(ctx context.Context, key Key, now time.Time) (Decision, error) {
	key, err := l.validateKey(key, now)
	if err != nil {
		return Decision{}, err
	}
	record, err := readClaim(ctx, l.db, key)
	if errors.Is(err, sql.ErrNoRows) {
		return Decision{}, ErrNotFound
	}
	if err != nil {
		return Decision{}, fmt.Errorf("read claim lookup: %w", err)
	}
	return resolveAuthenticated(record)
}
