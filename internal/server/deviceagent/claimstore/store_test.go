package claimstore

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHMACDigestCanonicalAndDomainSeparated(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Millisecond)
	requestID := testUUIDV7(now, 1)
	key := bytes.Repeat([]byte{0x5a}, minimumDigestKeyLen)
	req := CanonicalRequest{
		PairedDeviceID: "pair-kitchen-1",
		RequestID:      requestID,
		RuleID:         "kitchen-light-off",
		Locale:         "de-DE",
		Text:           "Schalte das Licht aus",
		EntityID:       "light.kitchen",
		ExpectedState:  "off",
	}

	digest, err := HMACDigest(key, req)
	if err != nil {
		t.Fatalf("HMACDigest: %v", err)
	}
	trimmed, err := HMACDigest(key, CanonicalRequest{
		PairedDeviceID: " pair-kitchen-1 ",
		RequestID:      strings.ToUpper(requestID),
		RuleID:         " kitchen-light-off ",
		Locale:         " de-DE ",
		Text:           "  Schalte das Licht aus  ",
		EntityID:       " light.kitchen ",
		ExpectedState:  " off ",
	})
	if err != nil {
		t.Fatalf("HMACDigest normalized: %v", err)
	}
	if digest != trimmed {
		t.Fatal("equivalent normalized requests produced different digests")
	}

	changed, err := HMACDigest(key, CanonicalRequest{
		PairedDeviceID: req.PairedDeviceID,
		RequestID:      req.RequestID,
		RuleID:         req.RuleID,
		Locale:         req.Locale,
		Text:           "Schalte das Licht an",
		EntityID:       req.EntityID,
		ExpectedState:  req.ExpectedState,
	})
	if err != nil {
		t.Fatalf("HMACDigest changed: %v", err)
	}
	if changed == digest {
		t.Fatal("different command text produced the same digest")
	}
	if _, err := HMACDigest([]byte("short"), req); !errors.Is(err, ErrDigestKeyTooShort) {
		t.Fatalf("short key error = %v, want ErrDigestKeyTooShort", err)
	}
	tooLong := req
	tooLong.Text = strings.Repeat("x", maxCommandTextBytes+1)
	if _, err := HMACDigest(key, tooLong); !errors.Is(err, ErrInvalidCanonicalRequest) {
		t.Fatalf("oversized request error = %v, want ErrInvalidCanonicalRequest", err)
	}
	for _, mutate := range []func(*CanonicalRequest){
		func(request *CanonicalRequest) { request.RuleID = "" },
		func(request *CanonicalRequest) { request.EntityID = "switch.front_door" },
		func(request *CanonicalRequest) { request.ExpectedState = "unlocked" },
	} {
		invalid := req
		mutate(&invalid)
		if _, err := HMACDigest(key, invalid); !errors.Is(err, ErrInvalidCanonicalRequest) {
			t.Fatalf("unsafe canonical request error = %v, want ErrInvalidCanonicalRequest", err)
		}
	}

	// Length-prefixing must distinguish field-boundary changes.
	boundaryA, err := HMACDigest(key, CanonicalRequest{
		PairedDeviceID: "pair-a",
		RequestID:      requestID,
		RuleID:         "boundary-rule",
		Locale:         "en",
		Text:           "ab:c",
		EntityID:       "light.boundary",
		ExpectedState:  "off",
	})
	if err != nil {
		t.Fatal(err)
	}
	boundaryB, err := HMACDigest(key, CanonicalRequest{
		PairedDeviceID: "pair-a",
		RequestID:      requestID,
		RuleID:         "boundary-rule",
		Locale:         "en-a",
		Text:           "b:c",
		EntityID:       "light.boundary",
		ExpectedState:  "off",
	})
	if err != nil {
		t.Fatal(err)
	}
	if boundaryA == boundaryB {
		t.Fatal("length-delimited canonical fields collided")
	}
}

func TestValidateRequestID(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Millisecond)
	valid := testUUIDV7(now.Add(-time.Minute), 2)
	issuedAt, err := ValidateRequestID(valid, now, 5*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatalf("ValidateRequestID(valid): %v", err)
	}
	if issuedAt.UnixMilli() != now.Add(-time.Minute).UnixMilli() {
		t.Fatalf("issuedAt = %s, want %s", issuedAt, now.Add(-time.Minute))
	}

	tests := []struct {
		name string
		id   string
		want error
	}{
		{name: "v4", id: uuid.NewString(), want: ErrInvalidRequestID},
		{name: "uppercase", id: strings.ToUpper(valid), want: ErrInvalidRequestID},
		{name: "stale", id: testUUIDV7(now.Add(-5*time.Minute-time.Millisecond), 3), want: ErrStaleRequestID},
		{name: "future", id: testUUIDV7(now.Add(30*time.Second+time.Millisecond), 4), want: ErrFutureRequestID},
		{name: "garbage", id: "not-a-uuid", want: ErrInvalidRequestID},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ValidateRequestID(test.id, now, 5*time.Minute, 30*time.Second); !errors.Is(err, test.want) {
				t.Fatalf("ValidateRequestID error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestClaimCompleteReplayConflictAndReopen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	path := filepath.Join(t.TempDir(), "claims.db")
	ledger := openTestLedger(t, path, Options{})
	key := Key{PairedDeviceID: "paired-kitchen", RequestID: testUUIDV7(now, 5)}
	digest := testDigest(5)

	first, err := ledger.Claim(ctx, key, digest, now)
	if err != nil {
		t.Fatalf("first Claim: %v", err)
	}
	if first.Disposition != DispatchNew {
		t.Fatalf("first disposition = %v, want DispatchNew", first.Disposition)
	}
	duplicate, err := ledger.Claim(ctx, key, digest, now)
	if err != nil {
		t.Fatalf("duplicate Claim: %v", err)
	}
	if duplicate.Disposition != OutcomeIndeterminate {
		t.Fatalf("claimed duplicate disposition = %v, want OutcomeIndeterminate", duplicate.Disposition)
	}
	lookup, err := ledger.Lookup(ctx, key, now)
	if err != nil || lookup.Disposition != OutcomeIndeterminate {
		t.Fatalf("claimed Lookup = %#v, %v; want OutcomeIndeterminate", lookup, err)
	}
	other := digest
	other[0] ^= 0xff
	conflict, err := ledger.Claim(ctx, key, other, now)
	if err != nil {
		t.Fatalf("conflicting Claim: %v", err)
	}
	if conflict.Disposition != DigestConflict {
		t.Fatalf("conflicting disposition = %v, want DigestConflict", conflict.Disposition)
	}

	completed := CompletedResult{
		Outcome:        OutcomeSuccess,
		ConversationID: "ha-conversation-kitchen-1",
		SpeechText:     "Das Licht ist aus.",
		Language:       "de-DE",
		ResponseType:   "action_done",
		ActionExecuted: ActionExecutedYes,
	}
	if err := ledger.Complete(ctx, first.Handle, completed, now.Add(time.Second)); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := ledger.Complete(ctx, first.Handle, completed, now.Add(2*time.Second)); err != nil {
		t.Fatalf("idempotent Complete: %v", err)
	}
	differentConversation := completed
	differentConversation.ConversationID = "ha-conversation-kitchen-2"
	if err := ledger.Complete(ctx, first.Handle, differentConversation, now.Add(2*time.Second)); !errors.Is(err, ErrTerminalConflict) {
		t.Fatalf("different conversation id error = %v, want ErrTerminalConflict", err)
	}
	differentResult := completed
	differentResult.SpeechText = "Ein anderes Ergebnis"
	if err := ledger.Complete(ctx, first.Handle, differentResult, now.Add(2*time.Second)); !errors.Is(err, ErrTerminalConflict) {
		t.Fatalf("different terminal result error = %v, want ErrTerminalConflict", err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened := openTestLedger(t, path, Options{})
	replay, err := reopened.Claim(ctx, key, digest, now.Add(3*time.Second))
	if err != nil {
		t.Fatalf("reopened Claim: %v", err)
	}
	if replay.Disposition != ReplayCompleted || replay.Result == nil {
		t.Fatalf("reopened disposition/result = %v/%#v, want replay", replay.Disposition, replay.Result)
	}
	if *replay.Result != completed {
		t.Fatalf("replayed result = %#v, want %#v", *replay.Result, completed)
	}
	lookup, err = reopened.Lookup(ctx, key, now.Add(3*time.Second))
	if err != nil || lookup.Disposition != ReplayCompleted || lookup.Result == nil || *lookup.Result != completed {
		t.Fatalf("completed Lookup = %#v, %v; want replay", lookup, err)
	}
	missing := Key{PairedDeviceID: key.PairedDeviceID, RequestID: testUUIDV7(now, 55)}
	if _, err := reopened.Lookup(ctx, missing, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Lookup error = %v, want ErrNotFound", err)
	}
}

func TestLookupRejectsTamperedCompletedResult(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	ledger := openTestLedger(t, filepath.Join(t.TempDir(), "claims.db"), Options{})
	key := Key{PairedDeviceID: "paired-kitchen", RequestID: testUUIDV7(now, 56)}
	decision, err := ledger.Claim(ctx, key, testDigest(56), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Complete(ctx, decision.Handle, CompletedResult{
		Outcome: OutcomeSuccess, ConversationID: "ha-conversation-tamper-1", SpeechText: "The light is off.", Language: "en-US",
		ResponseType: "action_done", ActionExecuted: ActionExecutedYes,
	}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.db.ExecContext(ctx, `UPDATE ha_command_claims SET conversation_id = ? WHERE paired_device_id = ? AND request_id = ?`,
		"ha-conversation-attacker", key.PairedDeviceID, key.RequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Lookup(ctx, key, now.Add(2*time.Second)); !errors.Is(err, ErrSchema) {
		t.Fatalf("tampered Lookup error = %v, want ErrSchema", err)
	}
}

func TestReopenClaimNeverRedispatches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	path := filepath.Join(t.TempDir(), "claims.db")
	key := Key{PairedDeviceID: "paired-office", RequestID: testUUIDV7(now, 6)}
	digest := testDigest(6)

	firstLedger := openTestLedger(t, path, Options{})
	first, err := firstLedger.Claim(ctx, key, digest, now)
	if err != nil || first.Disposition != DispatchNew {
		t.Fatalf("first Claim = %#v, %v", first, err)
	}
	if err := firstLedger.Close(); err != nil {
		t.Fatal(err)
	}

	secondLedger := openTestLedger(t, path, Options{})
	second, err := secondLedger.Claim(ctx, key, digest, now.Add(time.Second))
	if err != nil {
		t.Fatalf("Claim after reopen: %v", err)
	}
	if second.Disposition != OutcomeIndeterminate {
		t.Fatalf("Claim after reopen = %v, want OutcomeIndeterminate", second.Disposition)
	}
}

func TestAbruptProcessExitLeavesIndeterminateClaim(t *testing.T) {
	if os.Getenv("SPEECHKIT_CLAIMSTORE_CRASH_HELPER") == "1" {
		runCrashHelper()
		return
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	path := filepath.Join(t.TempDir(), "crash-claims.db")
	requestID := testUUIDV7(now, 7)
	command := exec.Command(os.Args[0], "-test.run=^TestAbruptProcessExitLeavesIndeterminateClaim$")
	command.Env = append(os.Environ(),
		"SPEECHKIT_CLAIMSTORE_CRASH_HELPER=1",
		"SPEECHKIT_CLAIMSTORE_CRASH_PATH="+path,
		"SPEECHKIT_CLAIMSTORE_CRASH_REQUEST_ID="+requestID,
		fmt.Sprintf("SPEECHKIT_CLAIMSTORE_CRASH_NOW=%d", now.UnixMilli()),
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("crash helper failed: %v\n%s", err, output)
	}

	ledger := openTestLedger(t, path, Options{})
	decision, err := ledger.Claim(ctx,
		Key{PairedDeviceID: "paired-crash", RequestID: requestID},
		testDigest(7),
		now.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("Claim after abrupt exit: %v", err)
	}
	if decision.Disposition != OutcomeIndeterminate {
		t.Fatalf("Claim after abrupt exit = %v, want OutcomeIndeterminate", decision.Disposition)
	}
}

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

func TestConcurrentClaimHasExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	path := filepath.Join(t.TempDir(), "claims.db")
	left := openTestLedger(t, path, Options{})
	right := openTestLedger(t, path, Options{})
	key := Key{PairedDeviceID: "paired-concurrent", RequestID: testUUIDV7(now, 8)}
	digest := testDigest(8)

	const callers = 64
	start := make(chan struct{})
	errorsCh := make(chan error, callers)
	var winners atomic.Int32
	var indeterminate atomic.Int32
	var wait sync.WaitGroup
	for i := 0; i < callers; i++ {
		wait.Add(1)
		ledger := left
		if i%2 == 1 {
			ledger = right
		}
		go func() {
			defer wait.Done()
			<-start
			decision, err := ledger.Claim(ctx, key, digest, now)
			if err != nil {
				errorsCh <- err
				return
			}
			switch decision.Disposition {
			case DispatchNew:
				winners.Add(1)
			case OutcomeIndeterminate:
				indeterminate.Add(1)
			default:
				errorsCh <- fmt.Errorf("unexpected disposition %v", decision.Disposition)
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Errorf("concurrent Claim: %v", err)
	}
	if winners.Load() != 1 {
		t.Fatalf("dispatch winners = %d, want 1", winners.Load())
	}
	if indeterminate.Load() != callers-1 {
		t.Fatalf("indeterminate decisions = %d, want %d", indeterminate.Load(), callers-1)
	}
}

func TestIndeterminateTransitionIsTerminal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	ledger := openTestLedger(t, filepath.Join(t.TempDir(), "claims.db"), Options{})
	key := Key{PairedDeviceID: "paired-bedroom", RequestID: testUUIDV7(now, 9)}
	digest := testDigest(9)
	decision, err := ledger.Claim(ctx, key, digest, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.MarkIndeterminate(ctx, decision.Handle, "ha.transport_ambiguous", now.Add(time.Second)); err != nil {
		t.Fatalf("MarkIndeterminate: %v", err)
	}
	if err := ledger.MarkIndeterminate(ctx, decision.Handle, "ha.other_reason", now.Add(2*time.Second)); err != nil {
		t.Fatalf("idempotent MarkIndeterminate: %v", err)
	}
	completed := CompletedResult{
		Outcome:        OutcomeSuccess,
		SpeechText:     "Fertig",
		Language:       "de-DE",
		ResponseType:   "action_done",
		ActionExecuted: ActionExecutedYes,
	}
	if err := ledger.Complete(ctx, decision.Handle, completed, now.Add(3*time.Second)); !errors.Is(err, ErrIndeterminate) {
		t.Fatalf("Complete after indeterminate error = %v, want ErrIndeterminate", err)
	}
	retry, err := ledger.Claim(ctx, key, digest, now.Add(4*time.Second))
	if err != nil || retry.Disposition != OutcomeIndeterminate {
		t.Fatalf("retry after indeterminate = %#v, %v", retry, err)
	}
}

func TestRetentionCapacityAndStaleReplaySafety(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	options := Options{
		MaxEntries:    2,
		Retention:     20 * time.Minute,
		MaxRequestAge: 5 * time.Minute,
		FutureSkew:    30 * time.Second,
		CleanupBatch:  1,
	}
	ledger := openTestLedger(t, filepath.Join(t.TempDir(), "claims.db"), options)
	firstKey := Key{PairedDeviceID: "pair-cap", RequestID: testUUIDV7(now, 10)}
	secondKey := Key{PairedDeviceID: "pair-cap", RequestID: testUUIDV7(now, 11)}
	thirdKey := Key{PairedDeviceID: "pair-cap", RequestID: testUUIDV7(now, 12)}
	for index, key := range []Key{firstKey, secondKey} {
		decision, err := ledger.Claim(ctx, key, testDigest(byte(index+10)), now)
		if err != nil || decision.Disposition != DispatchNew {
			t.Fatalf("capacity seed %d = %#v, %v", index, decision, err)
		}
	}
	if _, err := ledger.Claim(ctx, thirdKey, testDigest(12), now); !errors.Is(err, ErrCapacity) {
		t.Fatalf("full ledger error = %v, want ErrCapacity", err)
	}
	existing, err := ledger.Claim(ctx, firstKey, testDigest(10), now)
	if err != nil || existing.Disposition != OutcomeIndeterminate {
		t.Fatalf("existing claim at capacity = %#v, %v", existing, err)
	}

	afterRetention := now.Add(21 * time.Minute)
	deleted, err := ledger.Prune(ctx, afterRetention, 1)
	if err != nil || deleted != 1 {
		t.Fatalf("first bounded Prune = %d, %v, want 1", deleted, err)
	}
	deleted, err = ledger.Prune(ctx, afterRetention, 1)
	if err != nil || deleted != 1 {
		t.Fatalf("second bounded Prune = %d, %v, want 1", deleted, err)
	}
	if _, err := ledger.Claim(ctx, firstKey, testDigest(10), afterRetention); !errors.Is(err, ErrStaleRequestID) {
		t.Fatalf("pruned old request error = %v, want ErrStaleRequestID", err)
	}
	newKey := Key{PairedDeviceID: "pair-cap", RequestID: testUUIDV7(afterRetention, 13)}
	decision, err := ledger.Claim(ctx, newKey, testDigest(13), afterRetention)
	if err != nil || decision.Disposition != DispatchNew {
		t.Fatalf("claim after safe prune = %#v, %v", decision, err)
	}
}

func TestUnsafeRetentionAndInMemoryPathsFailClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "claims.db")
	_, err := Open(ctx, Options{
		Path:          path,
		Retention:     5 * time.Minute,
		MaxRequestAge: 5 * time.Minute,
		FutureSkew:    time.Second,
	})
	if !errors.Is(err, ErrUnsafeRetention) {
		t.Fatalf("unsafe retention error = %v, want ErrUnsafeRetention", err)
	}
	if _, err := Open(ctx, Options{Path: ":memory:"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("in-memory path error = %v, want ErrInvalidOptions", err)
	}
}

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

func TestPrivacyAndFilePermissions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	dir := filepath.Join(t.TempDir(), "private")
	path := filepath.Join(dir, "claims.db")
	ledger := openTestLedger(t, path, Options{})
	requestID := testUUIDV7(now, 14)
	commandText := "ultra-unique-private-command-52bd7d"
	pairingKey := []byte("ultra-unique-private-pairing-key-32-bytes-minimum")
	canonical := CanonicalRequest{
		PairedDeviceID: "pair-private",
		RequestID:      requestID,
		RuleID:         "private-light-off",
		Locale:         "en-US",
		Text:           commandText,
		EntityID:       "light.private",
		ExpectedState:  "off",
	}
	digest, err := HMACDigest(pairingKey, canonical)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := ledger.Claim(ctx, Key{
		PairedDeviceID: canonical.PairedDeviceID,
		RequestID:      canonical.RequestID,
	}, digest, now)
	if err != nil {
		t.Fatal(err)
	}
	result := CompletedResult{
		Outcome:        OutcomeSuccess,
		ConversationID: "ha-conversation-private-1",
		SpeechText:     "The private command completed.",
		Language:       "en-US",
		ResponseType:   "action_done",
		ActionExecuted: ActionExecutedYes,
	}
	if err := ledger.Complete(ctx, decision.Handle, result, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("checkpoint privacy fixture: %v", err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("database permissions = %#o, want 0600", got)
		}
		dirInfo, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := dirInfo.Mode().Perm(); got != 0o700 {
			t.Fatalf("new database directory permissions = %#o, want 0700", got)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{[]byte(commandText), pairingKey} {
		if bytes.Contains(raw, forbidden) {
			t.Fatalf("database contains forbidden plaintext %q", forbidden)
		}
	}
	if !bytes.Contains(raw, []byte(result.SpeechText)) {
		t.Fatal("database does not contain the explicitly allow-listed replay speech")
	}
	if !bytes.Contains(raw, []byte(result.ConversationID)) {
		t.Fatal("database does not contain the explicitly allow-listed conversation id")
	}
}

func TestCompletedResultValidation(t *testing.T) {
	t.Parallel()
	valid := CompletedResult{
		Outcome:        OutcomeRejected,
		Language:       "zh-Hans",
		ResponseType:   "error",
		ErrorCode:      "ha.request_rejected",
		ReasonCode:     "ha.auth_failed",
		ActionExecuted: ActionExecutedNo,
	}
	if _, err := normalizeCompletedResult(valid); err != nil {
		t.Fatalf("valid rejected result: %v", err)
	}
	tests := []CompletedResult{
		{},
		{Outcome: OutcomeSuccess, ActionExecuted: ActionExecutedYes},
		{Outcome: OutcomeRejected, ErrorCode: "ha.error", ActionExecuted: ActionExecutedNo},
		{Outcome: OutcomeRejected, ErrorCode: "HA ERROR", ReasonCode: "ha.failed", ActionExecuted: ActionExecutedNo},
		{Outcome: OutcomeSuccess, ConversationID: strings.Repeat("x", maxConversationIDBytes+1), SpeechText: "ok", ActionExecuted: ActionExecutedYes},
		{Outcome: OutcomeSuccess, SpeechText: strings.Repeat("x", maxSpeechTextBytes+1), ActionExecuted: ActionExecutedYes},
		{Outcome: OutcomeSuccess, SpeechText: "ok", Language: "de_DE", ActionExecuted: ActionExecutedYes},
		{Outcome: OutcomeSuccess, SpeechText: "ok", ActionExecuted: ActionExecutedUnknown},
	}
	for index, result := range tests {
		if _, err := normalizeCompletedResult(result); !errors.Is(err, ErrInvalidResult) {
			t.Errorf("invalid result %d error = %v, want ErrInvalidResult", index, err)
		}
	}
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

func TestResultDigestStable(t *testing.T) {
	t.Parallel()
	result := CompletedResult{
		Outcome:        OutcomeSuccess,
		ConversationID: "ha-conversation-digest-1",
		SpeechText:     "done",
		Language:       "en-US",
		ResponseType:   "action_done",
		ActionExecuted: ActionExecutedYes,
	}
	first := digestCompletedResult(result)
	second := digestCompletedResult(result)
	if first != second {
		t.Fatal("result digest is not stable")
	}
	result.Retryable = true
	if first == digestCompletedResult(result) {
		t.Fatal("result digest did not bind retryability")
	}
	result.Retryable = false
	result.ConversationID = "ha-conversation-digest-2"
	if first == digestCompletedResult(result) {
		t.Fatal("result digest did not bind conversation id")
	}
}

func TestSQLiteDSNIsFileURI(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "claims with spaces.db")
	dsn := sqliteDSN(path)
	if !strings.HasPrefix(dsn, "file:") || !strings.Contains(dsn, "_txlock=immediate") {
		t.Fatalf("sqliteDSN(%q) = %q", path, dsn)
	}
	if strings.Contains(dsn, " ") {
		t.Fatalf("sqliteDSN contains an unescaped space: %q", dsn)
	}
}
