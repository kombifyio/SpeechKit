package auditlog

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSink acknowledges records only while up is true.
type fakeSink struct{ up atomic.Bool }

func (*fakeSink) Name() string         { return "fake" }
func (*fakeSink) Bound() time.Duration { return 50 * time.Millisecond }
func (s *fakeSink) Forward(ctx context.Context, _ Record) error {
	if s.up.Load() {
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestForwardRequiredDegradesWithoutAckAndRecoversOnForward(t *testing.T) {
	sink := &fakeSink{}
	configureForTest(t, ConfigOptions{
		Enabled:         true,
		Dir:             t.TempDir(),
		ForwardRequired: true,
		Forwarders:      []Forwarder{sink},
	})
	t.Cleanup(ResetForTests)

	if err := AppendEvent(context.Background(), Record{Event: EventModeStart}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if !ForwardStatus().Degraded {
		t.Fatal("no sink acknowledged the record, but the state is not degraded")
	}
	if err := CloudSessionAllowed(); !errors.Is(err, ErrForwardDegraded) {
		t.Fatalf("CloudSessionAllowed() = %v, want ErrForwardDegraded", err)
	}

	sink.up.Store(true)
	if err := AppendEvent(context.Background(), Record{Event: EventModeStop}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if ForwardStatus().Degraded {
		t.Fatal("a forward succeeded, but the state is still degraded")
	}
	if err := CloudSessionAllowed(); err != nil {
		t.Fatalf("CloudSessionAllowed() after recovery = %v, want nil", err)
	}
}
