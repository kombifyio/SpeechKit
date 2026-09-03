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

// The bundled local server runs with --ctx-size 4096; a budget computed from
// the 8192 default produced batches the server rejected with 400.
func TestConservativeContextWindowMatchesTheBundledLocalServer(t *testing.T) {
	if got := ConservativeContextWindow("local", "gemma-4-E2B-it-Q8_0.gguf"); got != LocalContextWindowTokens {
		t.Fatalf("local window = %d, want %d", got, LocalContextWindowTokens)
	}
	if got := ConservativeContextWindow("google", "gemini-2.5-flash"); got != 32768 {
		t.Fatalf("gemini window = %d, want 32768", got)
	}
	if got := ConservativeContextWindow("groq", "unknown-model"); got != DefaultContextWindowTokens {
		t.Fatalf("unknown window = %d, want the default %d", got, DefaultContextWindowTokens)
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
