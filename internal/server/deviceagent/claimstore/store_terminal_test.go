package claimstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

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
