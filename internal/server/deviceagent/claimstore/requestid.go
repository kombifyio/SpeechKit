package claimstore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidRequestID = errors.New("claimstore: request id must be a canonical UUIDv7")
	ErrStaleRequestID   = errors.New("claimstore: request id is outside the accepted age window")
	ErrFutureRequestID  = errors.New("claimstore: request id is too far in the future")
)

// ValidateRequestID verifies the UUIDv7 format and its embedded Unix
// millisecond timestamp. This freshness gate is what makes bounded claim
// retention safe: once a claim can be pruned, its request id is too old to be
// admitted again.
func ValidateRequestID(id string, now time.Time, maxAge, futureSkew time.Duration) (time.Time, error) {
	id = strings.TrimSpace(id)
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 || parsed.Variant() != uuid.RFC4122 || parsed.String() != id {
		return time.Time{}, ErrInvalidRequestID
	}
	if now.IsZero() || maxAge <= 0 || futureSkew < 0 {
		return time.Time{}, fmt.Errorf("%w: invalid validation window", ErrInvalidRequestID)
	}

	seconds, nanoseconds := parsed.Time().UnixTime()
	issuedAt := time.Unix(seconds, nanoseconds).UTC()
	now = now.UTC()
	if issuedAt.Before(now.Add(-maxAge)) {
		return time.Time{}, ErrStaleRequestID
	}
	if issuedAt.After(now.Add(futureSkew)) {
		return time.Time{}, ErrFutureRequestID
	}
	return issuedAt, nil
}
