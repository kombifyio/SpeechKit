package claimstore

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
)

// Complete records the allow-listed Home Assistant result. Callers must wait
// for this commit before returning success or starting TTS.
func (l *Ledger) Complete(ctx context.Context, handle Handle, result CompletedResult, now time.Time) error {
	if err := validateHandle(handle); err != nil {
		return err
	}
	if now.IsZero() {
		return fmt.Errorf("%w: completion time is required", ErrInvalidResult)
	}
	normalized, err := normalizeCompletedResult(result)
	if err != nil {
		return err
	}
	resultDigest := digestCompletedResult(normalized)

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin completion transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit.
	existing, err := readClaim(ctx, tx, handle.key)
	if err != nil {
		return fmt.Errorf("read claim for completion: %w", err)
	}
	if !sameDigest(existing.RequestDigest, handle.digest) {
		return ErrDigestConflict
	}
	switch existing.State {
	case stateCompleted:
		if bytes.Equal(existing.ResultDigest, resultDigest[:]) {
			return tx.Commit()
		}
		return ErrTerminalConflict
	case stateIndeterminate:
		return ErrIndeterminate
	case stateClaimed:
	default:
		return fmt.Errorf("%w: unknown state %q", ErrInvalidTransition, existing.State)
	}

	retryable := 0
	if normalized.Retryable {
		retryable = 1
	}
	resultExec, err := tx.ExecContext(ctx, `
		UPDATE ha_command_claims
		SET state = ?, result_digest = ?, outcome = ?, conversation_id = ?, speech_text = ?, language = ?,
			response_type = ?, error_code = ?, reason_code = ?, retryable = ?,
			action_executed = ?, terminal_at_ms = ?
		WHERE paired_device_id = ? AND request_id = ? AND request_digest = ? AND state = ?`,
		stateCompleted,
		resultDigest[:],
		normalized.Outcome,
		normalized.ConversationID,
		normalized.SpeechText,
		normalized.Language,
		normalized.ResponseType,
		normalized.ErrorCode,
		normalized.ReasonCode,
		retryable,
		normalized.ActionExecuted,
		now.UTC().UnixMilli(),
		handle.key.PairedDeviceID,
		handle.key.RequestID,
		handle.digest[:],
		stateClaimed,
	)
	if err != nil {
		return fmt.Errorf("complete durable claim: %w", err)
	}
	rows, err := resultExec.RowsAffected()
	if err != nil || rows != 1 {
		return fmt.Errorf("%w: completion updated %d rows", ErrInvalidTransition, rows)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit completed claim: %w", err)
	}
	return nil
}

// MarkIndeterminate terminalizes a claim whose outbound HA outcome cannot be
// proven. Such a request remains permanently non-dispatchable until it safely
// ages out of the request admission window and retention period.
func (l *Ledger) MarkIndeterminate(ctx context.Context, handle Handle, reasonCode string, now time.Time) error {
	if err := validateHandle(handle); err != nil {
		return err
	}
	reasonCode = strings.TrimSpace(reasonCode)
	if now.IsZero() || !validCode(reasonCode) {
		return fmt.Errorf("%w: a stable reason code and time are required", ErrInvalidResult)
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin indeterminate transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit.
	existing, err := readClaim(ctx, tx, handle.key)
	if err != nil {
		return fmt.Errorf("read claim for indeterminate transition: %w", err)
	}
	if !sameDigest(existing.RequestDigest, handle.digest) {
		return ErrDigestConflict
	}
	switch existing.State {
	case stateIndeterminate:
		return tx.Commit()
	case stateCompleted:
		return ErrTerminalConflict
	case stateClaimed:
	default:
		return fmt.Errorf("%w: unknown state %q", ErrInvalidTransition, existing.State)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE ha_command_claims
		SET state = ?, reason_code = ?, action_executed = ?, terminal_at_ms = ?
		WHERE paired_device_id = ? AND request_id = ? AND request_digest = ? AND state = ?`,
		stateIndeterminate,
		reasonCode,
		ActionExecutedUnknown,
		now.UTC().UnixMilli(),
		handle.key.PairedDeviceID,
		handle.key.RequestID,
		handle.digest[:],
		stateClaimed,
	)
	if err != nil {
		return fmt.Errorf("mark durable claim indeterminate: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return fmt.Errorf("%w: indeterminate transition updated %d rows", ErrInvalidTransition, rows)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit indeterminate claim: %w", err)
	}
	return nil
}
