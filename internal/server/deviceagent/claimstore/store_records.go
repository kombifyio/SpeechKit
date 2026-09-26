package claimstore

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"fmt"
)

type claimRecord struct {
	RequestDigest  []byte
	State          string
	ResultDigest   []byte
	Outcome        sql.NullString
	ConversationID string
	SpeechText     string
	Language       string
	ResponseType   string
	ErrorCode      string
	ReasonCode     string
	Retryable      int
	Action         string
}

type claimQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readClaim(ctx context.Context, query claimQuery, key Key) (claimRecord, error) {
	var record claimRecord
	err := query.QueryRowContext(ctx, `
		SELECT request_digest, state, result_digest, outcome, conversation_id, speech_text, language,
			response_type, error_code, reason_code, retryable, action_executed
		FROM ha_command_claims
		WHERE paired_device_id = ? AND request_id = ?`,
		key.PairedDeviceID,
		key.RequestID,
	).Scan(
		&record.RequestDigest,
		&record.State,
		&record.ResultDigest,
		&record.Outcome,
		&record.ConversationID,
		&record.SpeechText,
		&record.Language,
		&record.ResponseType,
		&record.ErrorCode,
		&record.ReasonCode,
		&record.Retryable,
		&record.Action,
	)
	return record, err
}

func resolveExisting(record claimRecord, digest Digest) (Decision, error) {
	if !sameDigest(record.RequestDigest, digest) {
		return Decision{Disposition: DigestConflict}, nil
	}
	return resolveAuthenticated(record)
}

func resolveAuthenticated(record claimRecord) (Decision, error) {
	switch record.State {
	case stateClaimed, stateIndeterminate:
		return Decision{Disposition: OutcomeIndeterminate}, nil
	case stateCompleted:
		if len(record.ResultDigest) != sha256.Size || !record.Outcome.Valid {
			return Decision{}, ErrSchema
		}
		result := &CompletedResult{
			Outcome:        record.Outcome.String,
			ConversationID: record.ConversationID,
			SpeechText:     record.SpeechText,
			Language:       record.Language,
			ResponseType:   record.ResponseType,
			ErrorCode:      record.ErrorCode,
			ReasonCode:     record.ReasonCode,
			Retryable:      record.Retryable == 1,
			ActionExecuted: record.Action,
		}
		normalized, err := normalizeCompletedResult(*result)
		if err != nil {
			return Decision{}, ErrSchema
		}
		expectedDigest := digestCompletedResult(normalized)
		if subtle.ConstantTimeCompare(record.ResultDigest, expectedDigest[:]) != 1 {
			return Decision{}, ErrSchema
		}
		return Decision{Disposition: ReplayCompleted, Result: result}, nil
	default:
		return Decision{}, fmt.Errorf("%w: unknown state %q", ErrSchema, record.State)
	}
}
