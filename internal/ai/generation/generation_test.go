package generation

import (
	"context"
	"errors"
	"testing"
)

func TestSafeInputBudgetUsesConservativeUnknownContext(t *testing.T) {
	model := Model{}
	budget := SafeInputBudget(model, 1024)
	if budget >= DefaultContextWindowTokens {
		t.Fatalf("safe budget = %d, want room for prompt and output", budget)
	}
}

func TestEstimateTokensDoesNotUnderestimateShortMultilingualText(t *testing.T) {
	if got := EstimateTokens("Besprechung – 决定"); got < 4 {
		t.Fatalf("estimated tokens = %d, want conservative multilingual estimate", got)
	}
}

func TestKindPreservesCancellationAndTypedContextLimit(t *testing.T) {
	if got := Kind(context.Canceled); got != ErrorCancelled {
		t.Fatalf("cancellation kind = %q", got)
	}
	err := &Error{Kind: ErrorContextLimit, Err: errors.New("too large")}
	if got := Kind(err); got != ErrorContextLimit {
		t.Fatalf("typed kind = %q", got)
	}
}
