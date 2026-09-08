package speechkit

import (
	"errors"
	"fmt"
	"testing"
)

type reasonedBlockedError struct{ reason string }

func (e *reasonedBlockedError) Error() string             { return "speechkit: output blocked: " + e.reason }
func (e *reasonedBlockedError) Unwrap() error             { return ErrOutputBlocked }
func (e *reasonedBlockedError) OutputBlockReason() string { return e.reason }

func TestOutputBlockReasonOf(t *testing.T) {
	reasoned := &reasonedBlockedError{reason: "target window is unavailable"}
	if got := OutputBlockReasonOf(reasoned); got != "target window is unavailable" {
		t.Fatalf("OutputBlockReasonOf(reasoned) = %q", got)
	}
	if got := OutputBlockReasonOf(fmt.Errorf("output: inject: %w", reasoned)); got != "target window is unavailable" {
		t.Fatalf("OutputBlockReasonOf(wrapped) = %q, want the reason through the wrap", got)
	}
	if got := OutputBlockReasonOf(ErrOutputBlocked); got != "" {
		t.Fatalf("OutputBlockReasonOf(bare sentinel) = %q, want empty", got)
	}
	if got := OutputBlockReasonOf(nil); got != "" {
		t.Fatalf("OutputBlockReasonOf(nil) = %q, want empty", got)
	}
	if got := OutputBlockReasonOf(errors.New("chord failed")); got != "" {
		t.Fatalf("OutputBlockReasonOf(untyped) = %q, want empty", got)
	}
	// The wrapper still classifies as "no text was sent".
	if f := (TranscriptionFinalization{}).WithOutputResult(reasoned); f.Output != OutputBlocked {
		t.Fatalf("WithOutputResult(reasoned).Output = %q, want %q", f.Output, OutputBlocked)
	}
}
