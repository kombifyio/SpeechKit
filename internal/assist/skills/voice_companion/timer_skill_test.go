package voice_companion

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

func TestTimerSkill_IntentIsTimer(t *testing.T) {
	if NewTimerSkill().Intent() != shortcuts.IntentTimer {
		t.Errorf("expected IntentTimer")
	}
}

func TestParseDuration_GermanForms(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"auf 5 minuten", 5 * time.Minute},
		{"auf 1 minute", 1 * time.Minute},
		{"fuer 30 sekunden", 30 * time.Second},
		{"auf 1 stunde", time.Hour},
		{"auf 1 stunde 30 minuten", 90 * time.Minute},
		{"auf 2 stunden 15 minuten 45 sekunden", 2*time.Hour + 15*time.Minute + 45*time.Second},
		{"15 minuten", 15 * time.Minute},
	}
	for _, c := range cases {
		got, _, ok := ParseDuration(c.in, "de")
		if !ok || got != c.want {
			t.Errorf("ParseDuration(%q) = (%v, %v), want (%v, true)", c.in, got, ok, c.want)
		}
	}
}

func TestParseDuration_EnglishForms(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"for 5 minutes", 5 * time.Minute},
		{"for 1 minute", 1 * time.Minute},
		{"for 30 seconds", 30 * time.Second},
		{"for 1 hour", time.Hour},
		{"for 2 hours 15 minutes", 2*time.Hour + 15*time.Minute},
		{"10 min", 10 * time.Minute},
	}
	for _, c := range cases {
		got, _, ok := ParseDuration(c.in, "en")
		if !ok || got != c.want {
			t.Errorf("ParseDuration(%q) = (%v, %v), want (%v, true)", c.in, got, ok, c.want)
		}
	}
}

func TestParseDuration_NoMatch(t *testing.T) {
	cases := []string{"", "auf gar nichts", "for the weather", "wie spät"}
	for _, c := range cases {
		_, _, ok := ParseDuration(c, "de")
		if ok {
			t.Errorf("ParseDuration(%q) should not match", c)
		}
	}
}

func TestTimerSkill_NoSink_VerbalConfirmation(t *testing.T) {
	skill := NewTimerSkill()
	got, err := skill.Execute(context.Background(), assist.ToolCall{Payload: "auf 5 minuten", Locale: "de"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Text != "Timer auf 5 Minuten gesetzt." {
		t.Errorf("Text = %q, want 'Timer auf 5 Minuten gesetzt.'", got.Text)
	}
	if got.Action != "execute" {
		t.Errorf("Action = %q, want execute", got.Action)
	}
}

func TestTimerSkill_NoSink_EN(t *testing.T) {
	skill := NewTimerSkill()
	got, err := skill.Execute(context.Background(), assist.ToolCall{Payload: "for 10 minutes", Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Text != "Timer set for 10 minutes." {
		t.Errorf("Text = %q, want 'Timer set for 10 minutes.'", got.Text)
	}
}

// v0.38.0 multi-turn: an unparseable first turn now asks "Für wie
// lange?" / "How long?" rather than silently falling through to the
// LLM. Second-pass unparseable still falls through to silent so the
// LLM can take over.
func TestTimerSkill_UnparseableFirstTurnAsksForDuration(t *testing.T) {
	skill := NewTimerSkill()
	got, err := skill.Execute(context.Background(), assist.ToolCall{Payload: "auf gar nichts", Locale: "de"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !got.FollowupNeeded {
		t.Error("expected FollowupNeeded=true on unparseable first turn")
	}
	if got.Action != "respond" {
		t.Errorf("Action = %q, want 'respond'", got.Action)
	}
	if got.Text != "Für wie lange?" {
		t.Errorf("Text = %q, want 'Für wie lange?'", got.Text)
	}
	if got.FollowupState["awaiting_duration"] != "1" {
		t.Errorf("FollowupState = %v, want awaiting_duration=1", got.FollowupState)
	}
}

func TestTimerSkill_FollowupUnparseableFallsThroughToSilent(t *testing.T) {
	skill := NewTimerSkill()
	// Second turn carries the "awaiting_duration=1" marker in
	// Context (added by pipeline.withFollowupState). If we still
	// can't parse, we give up and let the LLM take over.
	got, _ := skill.Execute(context.Background(), assist.ToolCall{
		Payload: "schwirbel",
		Locale:  "de",
		Context: "awaiting_duration=1",
	})
	if got.Action != "silent" {
		t.Errorf("Action = %q, want silent on second-pass fail", got.Action)
	}
}

type fakeTimerSink struct {
	calls     int
	last      time.Duration
	lastLabel string
	err       error
}

func (s *fakeTimerSink) Schedule(_ context.Context, d time.Duration, label, _ string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	s.calls++
	s.last = d
	s.lastLabel = label
	return "stub-id", nil
}

func TestTimerSkill_WithSink_DelegatesToScheduler(t *testing.T) {
	sink := &fakeTimerSink{}
	skill := NewTimerSkill().WithSink(sink)
	_, err := skill.Execute(context.Background(), assist.ToolCall{Payload: "auf 5 minuten", Locale: "de"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sink.calls != 1 {
		t.Errorf("expected sink to be called once, got %d", sink.calls)
	}
	if sink.last != 5*time.Minute {
		t.Errorf("sink got %v, want 5m", sink.last)
	}
	if sink.lastLabel == "" {
		t.Error("expected non-empty label for notification")
	}
}

func TestTimerSkill_SinkError_PropagatesAsError(t *testing.T) {
	sink := &fakeTimerSink{err: errors.New("scheduler down")}
	skill := NewTimerSkill().WithSink(sink)
	_, err := skill.Execute(context.Background(), assist.ToolCall{Payload: "auf 5 minuten", Locale: "de"})
	if err == nil {
		t.Fatal("expected sink error to propagate")
	}
}

func TestFormatTimerLabel_PluralisesCorrectly(t *testing.T) {
	cases := []struct {
		d    time.Duration
		loc  string
		want string
	}{
		{1 * time.Minute, "de", "1 Minute"},
		{2 * time.Minute, "de", "2 Minuten"},
		{1 * time.Hour, "de", "1 Stunde"},
		{2 * time.Hour, "de", "2 Stunden"},
		{1*time.Hour + 30*time.Minute, "de", "1 Stunde und 30 Minuten"},
		{1 * time.Minute, "en", "1 minute"},
		{2 * time.Minute, "en", "2 minutes"},
		{1*time.Hour + 30*time.Minute, "en", "1 hour and 30 minutes"},
	}
	for _, c := range cases {
		got := formatTimerLabel(c.d, c.loc)
		if got != c.want {
			t.Errorf("formatTimerLabel(%v, %q) = %q, want %q", c.d, c.loc, got, c.want)
		}
	}
}
