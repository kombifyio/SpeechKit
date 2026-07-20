package claimstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Register the repository's CGo-free SQLite driver.
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

// Open opens or creates the durable claim ledger. In-memory databases and
// non-regular paths are rejected because they cannot provide crash recovery.
func Open(ctx context.Context, options Options) (*Ledger, error) {
	normalized, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	absPath, err := prepareDatabaseFile(normalized.Path)
	if err != nil {
		return nil, err
	}
	normalized.Path = absPath

	db, err := sql.Open("sqlite", sqliteDSN(absPath))
	if err != nil {
		return nil, fmt.Errorf("open claim database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping claim database: %w", err)
	}
	if err := migrateAndValidate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Ledger{db: db, options: normalized}, nil
}

func normalizeOptions(options Options) (Options, error) {
	options.Path = strings.TrimSpace(options.Path)
	if options.Path == "" || options.Path == ":memory:" || strings.Contains(strings.ToLower(options.Path), "mode=memory") {
		return Options{}, fmt.Errorf("%w: a durable filesystem path is required", ErrInvalidOptions)
	}
	if options.MaxEntries == 0 {
		options.MaxEntries = defaultMaxEntries
	}
	if options.Retention == 0 {
		options.Retention = defaultRetention
	}
	if options.MaxRequestAge == 0 {
		options.MaxRequestAge = defaultMaxRequestAge
	}
	if options.FutureSkew == 0 {
		options.FutureSkew = defaultFutureSkew
	}
	if options.CleanupBatch == 0 {
		options.CleanupBatch = defaultCleanupBatch
	}
	if options.MaxEntries < 1 || options.Retention <= 0 || options.MaxRequestAge <= 0 || options.FutureSkew < 0 || options.CleanupBatch < 1 {
		return Options{}, ErrInvalidOptions
	}
	if options.Retention <= options.MaxRequestAge+options.FutureSkew {
		return Options{}, ErrUnsafeRetention
	}
	return options, nil
}

func prepareDatabaseFile(path string) (string, error) {
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve claim database path: %w", err)
	}
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create claim database directory: %w", err)
	}
	info, err := os.Lstat(absPath)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("%w: database path must be a regular file", ErrInvalidOptions)
		}
	case errors.Is(err, os.ErrNotExist):
		// The caller-selected state path was made absolute and rejected above
		// when it already named a symlink or non-regular file.
		file, openErr := os.OpenFile(absPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600) //nolint:gosec // G304: validated local state path is the intended file input.
		if openErr != nil {
			return "", fmt.Errorf("create claim database file: %w", openErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return "", fmt.Errorf("close new claim database file: %w", closeErr)
		}
	default:
		return "", fmt.Errorf("inspect claim database file: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(absPath, 0o600); err != nil {
			return "", fmt.Errorf("restrict claim database permissions: %w", err)
		}
	}
	return absPath, nil
}

func sqliteDSN(path string) string {
	uriPath := filepath.ToSlash(path)
	// A Windows drive path must become file:///C:/...; without the leading
	// slash URL parsing treats "C:" as an authority and SQLite rejects it.
	if filepath.VolumeName(path) != "" && !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := &url.URL{Scheme: "file", Path: uriPath}
	query := uri.Query()
	for _, pragma := range []string{
		"journal_mode(WAL)",
		"synchronous(FULL)",
		"busy_timeout(5000)",
		"foreign_keys(ON)",
		"secure_delete(FAST)",
		"journal_size_limit(4194304)",
		"wal_autocheckpoint(1000)",
	} {
		query.Add("_pragma", pragma)
	}
	query.Set("_txlock", "immediate")
	query.Set("_dqs", "false")
	uri.RawQuery = query.Encode()
	return uri.String()
}

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

// Close releases the database. It does not remove the durable ledger.
func (l *Ledger) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.Close()
}

func (l *Ledger) validateKey(key Key, now time.Time) (Key, error) {
	key.PairedDeviceID = strings.TrimSpace(key.PairedDeviceID)
	key.RequestID = strings.ToLower(strings.TrimSpace(key.RequestID))
	if err := validatePairedDeviceID(key.PairedDeviceID); err != nil {
		return Key{}, err
	}
	if _, err := ValidateRequestID(key.RequestID, now, l.options.MaxRequestAge, l.options.FutureSkew); err != nil {
		return Key{}, err
	}
	return key, nil
}

func validatePairedDeviceID(id string) error {
	if id == "" || len(id) > maxPairedDeviceIDBytes {
		return ErrInvalidKey
	}
	for _, character := range id {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '.', character == '_', character == '-', character == ':':
		default:
			return ErrInvalidKey
		}
	}
	return nil
}

func validateHandle(handle Handle) error {
	if err := validatePairedDeviceID(handle.key.PairedDeviceID); err != nil || handle.key.RequestID == "" {
		return ErrInvalidKey
	}
	if isZeroDigest(handle.digest) {
		return ErrInvalidDigest
	}
	return nil
}

func isZeroDigest(digest Digest) bool {
	var zero Digest
	return subtle.ConstantTimeCompare(digest[:], zero[:]) == 1
}

func sameDigest(stored []byte, candidate Digest) bool {
	return len(stored) == len(candidate) && subtle.ConstantTimeCompare(stored, candidate[:]) == 1
}

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

func normalizeCompletedResult(result CompletedResult) (CompletedResult, error) {
	result.Outcome = strings.TrimSpace(result.Outcome)
	result.ConversationID = strings.TrimSpace(result.ConversationID)
	result.SpeechText = strings.TrimSpace(result.SpeechText)
	result.Language = strings.TrimSpace(result.Language)
	result.ResponseType = strings.TrimSpace(result.ResponseType)
	result.ErrorCode = strings.TrimSpace(result.ErrorCode)
	result.ReasonCode = strings.TrimSpace(result.ReasonCode)
	result.ActionExecuted = strings.TrimSpace(result.ActionExecuted)

	if len(result.ConversationID) > maxConversationIDBytes || len(result.SpeechText) > maxSpeechTextBytes || len(result.Language) > maxLanguageBytes ||
		len(result.ResponseType) > maxCodeBytes || len(result.ErrorCode) > maxCodeBytes || len(result.ReasonCode) > maxCodeBytes {
		return CompletedResult{}, ErrInvalidResult
	}
	if result.Language != "" && !validLanguage(result.Language) {
		return CompletedResult{}, ErrInvalidResult
	}
	if result.ResponseType != "" && !validCode(result.ResponseType) {
		return CompletedResult{}, ErrInvalidResult
	}
	if result.ErrorCode != "" && !validCode(result.ErrorCode) {
		return CompletedResult{}, ErrInvalidResult
	}
	if result.ReasonCode != "" && !validCode(result.ReasonCode) {
		return CompletedResult{}, ErrInvalidResult
	}
	switch result.Outcome {
	case OutcomeSuccess:
		if result.SpeechText == "" || result.ErrorCode != "" || result.ReasonCode != "" {
			return CompletedResult{}, ErrInvalidResult
		}
	case OutcomeRejected:
		if result.ErrorCode == "" || result.ReasonCode == "" {
			return CompletedResult{}, ErrInvalidResult
		}
	default:
		return CompletedResult{}, ErrInvalidResult
	}
	switch result.ActionExecuted {
	case ActionExecutedYes, ActionExecutedNo, ActionNotApplicable:
	default:
		return CompletedResult{}, ErrInvalidResult
	}
	return result, nil
}

func validCode(code string) bool {
	if code == "" || len(code) > maxCodeBytes {
		return false
	}
	for _, character := range code {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= '0' && character <= '9':
		case character == '.', character == '_', character == '-':
		default:
			return false
		}
	}
	return true
}

func validLanguage(language string) bool {
	if language == "" || len(language) > maxLanguageBytes {
		return false
	}
	for _, character := range language {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-':
		default:
			return false
		}
	}
	return true
}

func digestCompletedResult(result CompletedResult) Digest {
	return digestCompletedResultWithDomain(resultDigestDomain, result, true)
}

func digestCompletedResultV1(result CompletedResult) Digest {
	return digestCompletedResultWithDomain(resultDigestDomainV1, result, false)
}

func digestCompletedResultWithDomain(domain string, result CompletedResult, includeConversationID bool) Digest {
	hash := sha256.New()
	writeResultField := func(value string) {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(value)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(value))
	}
	writeResultField(domain)
	writeResultField(result.Outcome)
	if includeConversationID {
		writeResultField(result.ConversationID)
	}
	writeResultField(result.SpeechText)
	writeResultField(result.Language)
	writeResultField(result.ResponseType)
	writeResultField(result.ErrorCode)
	writeResultField(result.ReasonCode)
	if result.Retryable {
		writeResultField("true")
	} else {
		writeResultField("false")
	}
	writeResultField(result.ActionExecuted)
	var digest Digest
	copy(digest[:], hash.Sum(nil))
	return digest
}
