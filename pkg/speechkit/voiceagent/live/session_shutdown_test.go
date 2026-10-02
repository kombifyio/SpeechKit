package live

import (
	"context"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestSessionShutdownLeavesNoGoroutinesAndIsIdempotent(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	session := NewSession(newSessionTestProvider(), Callbacks{})
	if err := session.Start(context.Background(), LiveConfig{}, DefaultIdleConfig()); err != nil {
		t.Fatalf("start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- session.Shutdown(ctx) }()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}
	if err := session.Shutdown(ctx); err != nil {
		t.Fatalf("repeat shutdown: %v", err)
	}
	if got := session.CurrentState(); got != StateInactive {
		t.Fatalf("state after shutdown = %v", got)
	}
}
