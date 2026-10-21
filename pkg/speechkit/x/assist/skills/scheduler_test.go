package skills

import (
	"context"
	"testing"
	"time"
)

func TestSchedulerFiresTimer(t *testing.T) {
	fired := make(chan Alarm, 1)
	s := newScheduler(func(a Alarm) { fired <- a })
	t.Cleanup(s.Close)

	id, err := (timerSink{s}).Schedule(context.Background(), 15*time.Millisecond, "15 ms", "en")
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	select {
	case a := <-fired:
		if a.Kind != "timer" {
			t.Errorf("kind = %q, want timer", a.Kind)
		}
		if a.ID != id {
			t.Errorf("fired id = %q, want %q", a.ID, id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timer never fired")
	}
}

func TestSchedulerCloseCancelsPendingAndRejects(t *testing.T) {
	fired := make(chan Alarm, 1)
	s := newScheduler(func(a Alarm) { fired <- a })

	if _, err := (reminderSink{s}).Schedule(context.Background(), time.Now().Add(10*time.Second), "later", "en"); err != nil {
		t.Fatalf("schedule reminder: %v", err)
	}
	s.Close()

	select {
	case <-fired:
		t.Fatal("alarm fired after Close")
	case <-time.After(80 * time.Millisecond):
		// cancelled as expected
	}

	if _, err := (timerSink{s}).Schedule(context.Background(), time.Millisecond, "x", "en"); err == nil {
		t.Error("scheduling after Close should return an error")
	}
}
