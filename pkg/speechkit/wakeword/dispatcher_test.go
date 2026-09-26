package wakeword

import (
	"context"
	"sync"
	"testing"
	"time"
)

type capturingHotkeySink struct {
	mu     sync.Mutex
	events []HotkeyEvent
}

func (s *capturingHotkeySink) Submit(ev HotkeyEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *capturingHotkeySink) snapshot() []HotkeyEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]HotkeyEvent, len(s.events))
	copy(out, s.events)
	return out
}

func TestDispatcher_EmitsKeyDownOnly(t *testing.T) {
	sink := &capturingHotkeySink{}
	d := NewDispatcher(sink, DispatcherOptions{})
	d.Emit(DetectionEvent{Phrase: "Hey Quby", Mode: "voice_agent", Probability: 0.9, At: time.Now()})
	if err := d.Close(testCtx(t)); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got := sink.snapshot()
	if len(got) != 1 {
		t.Fatalf("event count = %d, want 1", len(got))
	}
	if !got[0].KeyDown || got[0].Binding != "voice_agent" {
		t.Errorf("event = %+v, want KeyDown voice_agent", got[0])
	}
}

func TestDispatcher_SyntheticReleaseEmitsPair(t *testing.T) {
	sink := &capturingHotkeySink{}
	d := NewDispatcher(sink, DispatcherOptions{SyntheticRelease: true, ReleaseAfter: 5 * time.Millisecond})
	d.Emit(DetectionEvent{Mode: "dictate"})
	if err := d.Close(testCtx(t)); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got := sink.snapshot()
	if len(got) != 2 {
		t.Fatalf("event count = %d, want 2", len(got))
	}
	if !got[0].KeyDown || got[0].Binding != "dictate" {
		t.Errorf("first event = %+v", got[0])
	}
	if got[1].KeyDown || got[1].Binding != "dictate" {
		t.Errorf("second event = %+v", got[1])
	}
}

func TestDispatcher_IgnoresEventsWithoutMode(t *testing.T) {
	sink := &capturingHotkeySink{}
	d := NewDispatcher(sink, DispatcherOptions{})
	d.Emit(DetectionEvent{Phrase: "Hey Quby"})
	if err := d.Close(testCtx(t)); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := sink.snapshot(); len(got) != 0 {
		t.Errorf("expected 0 events, got %d (%+v)", len(got), got)
	}
}

func TestDispatcher_IgnoresEventsAfterClose(t *testing.T) {
	sink := &capturingHotkeySink{}
	d := NewDispatcher(sink, DispatcherOptions{})
	if err := d.Close(testCtx(t)); err != nil {
		t.Fatalf("Close: %v", err)
	}
	d.Emit(DetectionEvent{Mode: "assist"})
	if got := sink.snapshot(); len(got) != 0 {
		t.Errorf("expected 0 events after Close, got %d", len(got))
	}
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}
