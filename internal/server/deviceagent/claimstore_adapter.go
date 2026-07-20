package deviceagent

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/deviceagent/claimstore"
)

// DurableClaimLedger adapts the single-purpose SQLite ledger to the bridge
// contract. Handles exist only for claims won by this process; after a restart
// every previously nonterminal claim remains indeterminate and cannot be
// dispatched again.
type DurableClaimLedger struct {
	store *claimstore.Ledger
	mu    sync.Mutex
	owned map[durableHandleKey]claimstore.Handle
}

type durableHandleKey struct {
	claim  ClaimKey
	digest [32]byte
}

func NewDurableClaimLedger(store *claimstore.Ledger) (*DurableClaimLedger, error) {
	if store == nil {
		return nil, errors.New("device-agent bridge: durable claim store is required")
	}
	return &DurableClaimLedger{
		store: store,
		owned: make(map[durableHandleKey]claimstore.Handle),
	}, nil
}

func (l *DurableClaimLedger) Claim(ctx context.Context, key ClaimKey, digest [32]byte, now time.Time) (ClaimDecision, error) {
	decision, err := l.store.Claim(ctx, claimstore.Key{
		PairedDeviceID: key.PairingID,
		RequestID:      key.RequestID,
	}, claimstore.Digest(digest), now)
	if err != nil {
		return ClaimDecision{}, err
	}
	switch decision.Disposition {
	case claimstore.DispatchNew:
		l.mu.Lock()
		l.owned[durableHandleKey{claim: key, digest: digest}] = decision.Handle
		l.mu.Unlock()
		return ClaimDecision{Disposition: ClaimDispatchNew}, nil
	case claimstore.ReplayCompleted:
		if decision.Result == nil {
			return ClaimDecision{}, claimstore.ErrSchema
		}
		return ClaimDecision{
			Disposition: ClaimReplayCompleted,
			Result:      storedResultFromClaimstore(*decision.Result),
		}, nil
	case claimstore.OutcomeIndeterminate:
		return ClaimDecision{Disposition: ClaimIndeterminate}, nil
	case claimstore.DigestConflict:
		return ClaimDecision{Disposition: ClaimDigestConflict}, nil
	default:
		return ClaimDecision{}, claimstore.ErrSchema
	}
}

func (l *DurableClaimLedger) Lookup(ctx context.Context, key ClaimKey, now time.Time) (ClaimDecision, error) {
	decision, err := l.store.Lookup(ctx, claimstore.Key{
		PairedDeviceID: key.PairingID,
		RequestID:      key.RequestID,
	}, now)
	if errors.Is(err, claimstore.ErrNotFound) {
		return ClaimDecision{Disposition: ClaimNotFound}, nil
	}
	if err != nil {
		return ClaimDecision{}, err
	}
	switch decision.Disposition {
	case claimstore.ReplayCompleted:
		if decision.Result == nil {
			return ClaimDecision{}, claimstore.ErrSchema
		}
		return ClaimDecision{Disposition: ClaimReplayCompleted, Result: storedResultFromClaimstore(*decision.Result)}, nil
	case claimstore.OutcomeIndeterminate:
		return ClaimDecision{Disposition: ClaimIndeterminate}, nil
	default:
		return ClaimDecision{}, claimstore.ErrSchema
	}
}

func (l *DurableClaimLedger) Complete(ctx context.Context, key ClaimKey, digest [32]byte, result StoredResult, now time.Time) error {
	handleKey := durableHandleKey{claim: key, digest: digest}
	handle, ok := l.ownedHandle(handleKey)
	if !ok {
		return claimstore.ErrInvalidTransition
	}
	outcome := claimstore.OutcomeSuccess
	if result.Status == "denied" {
		outcome = claimstore.OutcomeRejected
	}
	err := l.store.Complete(ctx, handle, claimstore.CompletedResult{
		Outcome:        outcome,
		ConversationID: result.ConversationID,
		SpeechText:     result.Speech,
		Language:       result.Language,
		ResponseType:   result.ResponseType,
		ErrorCode:      result.ErrorCode,
		ReasonCode:     result.ReasonCode,
		Retryable:      result.Retryable,
		ActionExecuted: result.ActionExecuted,
	}, now)
	if err == nil {
		l.forgetHandle(handleKey)
	}
	return err
}

func (l *DurableClaimLedger) MarkIndeterminate(ctx context.Context, key ClaimKey, digest [32]byte, reason string, now time.Time) error {
	handleKey := durableHandleKey{claim: key, digest: digest}
	handle, ok := l.ownedHandle(handleKey)
	if !ok {
		return claimstore.ErrInvalidTransition
	}
	err := l.store.MarkIndeterminate(ctx, handle, reason, now)
	if err == nil {
		l.forgetHandle(handleKey)
	}
	return err
}

func (l *DurableClaimLedger) ownedHandle(key durableHandleKey) (claimstore.Handle, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	handle, ok := l.owned[key]
	return handle, ok
}

func (l *DurableClaimLedger) forgetHandle(key durableHandleKey) {
	l.mu.Lock()
	delete(l.owned, key)
	l.mu.Unlock()
}

func storedResultFromClaimstore(result claimstore.CompletedResult) *StoredResult {
	status := "success"
	if result.Outcome == claimstore.OutcomeRejected {
		status = "denied"
	}
	return &StoredResult{
		Status:         status,
		ConversationID: result.ConversationID,
		ResponseType:   result.ResponseType,
		Speech:         result.SpeechText,
		Language:       result.Language,
		ErrorCode:      result.ErrorCode,
		ReasonCode:     result.ReasonCode,
		Retryable:      result.Retryable,
		ActionExecuted: result.ActionExecuted,
	}
}
