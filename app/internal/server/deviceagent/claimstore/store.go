package claimstore

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"
)

const (
	requestSchema          = "speechkit.ha_assist.v1"
	resultDigestDomainV1   = "speechkit.ha_assist.result.v1"
	resultDigestDomain     = "speechkit.ha_assist.result.v2"
	stateClaimed           = "claimed"
	stateCompleted         = "completed"
	stateIndeterminate     = "indeterminate"
	OutcomeSuccess         = "success"
	OutcomeRejected        = "rejected"
	ActionExecutedYes      = "yes"
	ActionExecutedNo       = "no"
	ActionNotApplicable    = "not_applicable"
	ActionExecutedUnknown  = "unknown"
	maxPairedDeviceIDBytes = 128
	maxConversationIDBytes = 256
	maxSpeechTextBytes     = 8 * 1024
	maxLanguageBytes       = 64
	maxCodeBytes           = 64

	defaultMaxEntries    = 10_000
	defaultRetention     = 24 * time.Hour
	defaultMaxRequestAge = 10 * time.Minute
	defaultFutureSkew    = time.Minute
	defaultCleanupBatch  = 256
)

var (
	ErrInvalidOptions    = errors.New("claimstore: invalid options")
	ErrUnsafeRetention   = errors.New("claimstore: retention must exceed the request admission window")
	ErrInvalidKey        = errors.New("claimstore: invalid claim key")
	ErrInvalidDigest     = errors.New("claimstore: invalid request digest")
	ErrNotFound          = errors.New("claimstore: claim not found")
	ErrCapacity          = errors.New("claimstore: claim capacity reached")
	ErrIndeterminate     = errors.New("claimstore: Home Assistant outcome is indeterminate")
	ErrDigestConflict    = errors.New("claimstore: request id was already used with different content")
	ErrTerminalConflict  = errors.New("claimstore: claim already has a different terminal outcome")
	ErrInvalidResult     = errors.New("claimstore: invalid completed result")
	ErrInvalidTransition = errors.New("claimstore: invalid claim state transition")
)

// Digest is the HMAC-SHA-256 fingerprint of a canonical request.
type Digest [sha256.Size]byte

// Key is scoped to the server-authenticated pairing identity. Callers must
// derive PairedDeviceID from authentication state, never from an untrusted
// request-body device id.
type Key struct {
	PairedDeviceID string
	RequestID      string
}

// Options configures the dedicated local ledger.
type Options struct {
	Path          string
	MaxEntries    int
	Retention     time.Duration
	MaxRequestAge time.Duration
	FutureSkew    time.Duration
	CleanupBatch  int
}

// ClaimDisposition describes the only safe action after Claim returns.
type ClaimDisposition uint8

const (
	DispatchNew ClaimDisposition = iota + 1
	ReplayCompleted
	OutcomeIndeterminate
	DigestConflict
)

// CompletedResult is the allow-listed, client-visible subset persisted for a
// replay. It intentionally has no field for raw Home Assistant JSON, entity
// context, headers, credentials, SSML, or audio.
type CompletedResult struct {
	Outcome        string
	ConversationID string
	SpeechText     string
	Language       string
	ResponseType   string
	ErrorCode      string
	ReasonCode     string
	Retryable      bool
	ActionExecuted string
}

// Handle proves that this process won a new durable claim. Its fields are
// intentionally private so only Claim can mint it.
type Handle struct {
	key    Key
	digest Digest
}

// Decision is returned only after the corresponding transaction committed.
type Decision struct {
	Disposition ClaimDisposition
	Handle      Handle
	Result      *CompletedResult
}

// Ledger owns a single-purpose SQLite database. It must not be shared with the
// configurable transcription/content store.
type Ledger struct {
	db      *sql.DB
	options Options
}
