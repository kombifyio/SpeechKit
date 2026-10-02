package claimstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
