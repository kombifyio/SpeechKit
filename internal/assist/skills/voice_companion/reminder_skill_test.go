package voice_companion

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

func TestReminderSkill_IntentIsReminder(t *testing.T) {
	if NewReminderSkill().Intent() != shortcuts.IntentReminder {
		t.Errorf("expected IntentReminder")
	}
}

func TestParseReminder_RelativeIn_DE(t *testing.T) {
	now := time.Date(2026, 5, 21, 14, 0, 0, 0, time.Local)
	label, fireAt, ok := parseReminderPayload("an arzttermin in 30 minuten", now, "de")
	if !ok {
		t.Fatal("expected match")
	}
	if label != "arzttermin" {
		t.Errorf("label = %q, want 'arzttermin'", label)
	}
	want := now.Add(30 * time.Minute)
	if !fireAt.Equal(want) {
		t.Errorf("fireAt = %v, want %v", fireAt, want)
	}
}

func TestParseReminder_RelativeIn_EN(t *testing.T) {
	now := time.Date(2026, 5, 21, 14, 0, 0, 0, time.Local)
	label, fireAt, ok := parseReminderPayload("to call mom in 2 hours", now, "en")
	if !ok {
		t.Fatal("expected match")
	}
	if label != "call mom" {
		t.Errorf("label = %q, want 'call mom'", label)
	}
	want := now.Add(2 * time.Hour)
	if !fireAt.Equal(want) {
		t.Errorf("fireAt = %v, want %v", fireAt, want)
	}
}

func TestParseReminder_AbsoluteUm_LaterToday(t *testing.T) {
	now := time.Date(2026, 5, 21, 14, 0, 0, 0, time.Local)
	label, fireAt, ok := parseReminderPayload("an meeting um 17 uhr 30", now, "de")
	if !ok {
		t.Fatal("expected match")
	}
	if label != "meeting" {
		t.Errorf("label = %q, want 'meeting'", label)
	}
	want := time.Date(2026, 5, 21, 17, 30, 0, 0, time.Local)
	if !fireAt.Equal(want) {
		t.Errorf("fireAt = %v, want %v", fireAt, want)
	}
}

func TestParseReminder_AbsoluteAt_EN_RollsToTomorrowWhenPast(t *testing.T) {
	now := time.Date(2026, 5, 21, 18, 0, 0, 0, time.Local)
	label, fireAt, ok := parseReminderPayload("to lunch at 09:00", now, "en")
	if !ok {
		t.Fatal("expected match")
	}
	if label != "lunch" {
		t.Errorf("label = %q, want 'lunch'", label)
	}
	want := time.Date(2026, 5, 22, 9, 0, 0, 0, time.Local)
	if !fireAt.Equal(want) {
		t.Errorf("fireAt = %v, want %v (next day)", fireAt, want)
	}
}

func TestParseReminder_TomorrowExplicit(t *testing.T) {
	now := time.Date(2026, 5, 21, 14, 0, 0, 0, time.Local)
	label, fireAt, ok := parseReminderPayload("an arzttermin morgen um 9 uhr", now, "de")
	if !ok {
		t.Fatal("expected match")
	}
	if label != "arzttermin" {
		t.Errorf("label = %q, want 'arzttermin'", label)
	}
	want := time.Date(2026, 5, 22, 9, 0, 0, 0, time.Local)
	if !fireAt.Equal(want) {
		t.Errorf("fireAt = %v, want %v", fireAt, want)
	}
}

func TestReminderSkill_NoSink_VerbalConfirmation(t *testing.T) {
	now := time.Date(2026, 5, 21, 14, 0, 0, 0, time.Local)
	skill := NewReminderSkill().WithClock(func() time.Time { return now })
	got, err := skill.Execute(context.Background(), assist.ToolCall{Payload: "an arzttermin morgen um 9 uhr", Locale: "de"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got.Text, "arzttermin") {
		t.Errorf("Text should mention the label, got %q", got.Text)
	}
	if got.Action != "execute" {
		t.Errorf("Action = %q, want execute", got.Action)
	}
}

type fakeReminderSink struct {
	calls int
	last  time.Time
	label string
	err   error
}

func (s *fakeReminderSink) Schedule(_ context.Context, fireAt time.Time, label, _ string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	s.calls++
	s.last = fireAt
	s.label = label
	return "stub-id", nil
}

func TestReminderSkill_WithSink_DelegatesToScheduler(t *testing.T) {
	now := time.Date(2026, 5, 21, 14, 0, 0, 0, time.Local)
	sink := &fakeReminderSink{}
	skill := NewReminderSkill().WithClock(func() time.Time { return now }).WithSink(sink)
	_, err := skill.Execute(context.Background(), assist.ToolCall{Payload: "an arzttermin in 30 minuten", Locale: "de"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sink.calls != 1 {
		t.Errorf("expected sink to be called once, got %d", sink.calls)
	}
	if sink.label != "arzttermin" {
		t.Errorf("label = %q, want 'arzttermin'", sink.label)
	}
	want := now.Add(30 * time.Minute)
	if !sink.last.Equal(want) {
		t.Errorf("fireAt = %v, want %v", sink.last, want)
	}
}

func TestReminderSkill_UnparseableIsSilent(t *testing.T) {
	skill := NewReminderSkill()
	got, _ := skill.Execute(context.Background(), assist.ToolCall{Payload: "irgendwas komisches", Locale: "de"})
	if got.Action != "silent" {
		t.Errorf("Action = %q, want silent", got.Action)
	}
}
