package voice_companion

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

// fixedClock returns a deterministic clock pinned to 2026-05-21
// 14:07:00 UTC. The skill calls .Local() on the result before
// formatting, which converts to the test host's timezone — to keep
// tests timezone-independent we use time.UTC in Date(), and tests
// assert the patterns rather than the exact hour offset on hosts
// outside UTC.
//
// For hour-sensitive assertions (TimeSkill DE/EN), we Pin via Local
// already so the formatter reads stable values regardless of host TZ.
func fixedClock(year int, month time.Month, day, hour, minute int) func() time.Time {
	return func() time.Time {
		// Use the host's local zone so .Local() is a no-op and we get
		// reproducible H/M values on any test runner.
		return time.Date(year, month, day, hour, minute, 0, 0, time.Local)
	}
}

func TestTimeSkill_DE_FormatsHourAndMinute(t *testing.T) {
	skill := NewTimeSkill().WithClock(fixedClock(2026, time.May, 21, 9, 5))
	got, err := skill.Execute(context.Background(), assist.ToolCall{Locale: "de"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Es ist 9 Uhr 05."
	if got.Text != want {
		t.Errorf("Text = %q, want %q", got.Text, want)
	}
	if got.SpeakText != want {
		t.Errorf("SpeakText = %q, want %q", got.SpeakText, want)
	}
	if got.Action != "respond" {
		t.Errorf("Action = %q, want respond", got.Action)
	}
	if got.Surface != assist.ResultSurfacePanel {
		t.Errorf("Surface = %q, want panel", got.Surface)
	}
}

func TestTimeSkill_EN_FormatsAfternoonAsPM(t *testing.T) {
	skill := NewTimeSkill().WithClock(fixedClock(2026, time.May, 21, 14, 30))
	got, err := skill.Execute(context.Background(), assist.ToolCall{Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "It is 2:30 PM."
	if got.Text != want {
		t.Errorf("Text = %q, want %q", got.Text, want)
	}
}

func TestTimeSkill_EN_FormatsMidnight(t *testing.T) {
	skill := NewTimeSkill().WithClock(fixedClock(2026, time.May, 21, 0, 0))
	got, err := skill.Execute(context.Background(), assist.ToolCall{Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "It is 12:00 AM."
	if got.Text != want {
		t.Errorf("Text = %q, want %q", got.Text, want)
	}
}

func TestTimeSkill_EN_FormatsNoon(t *testing.T) {
	skill := NewTimeSkill().WithClock(fixedClock(2026, time.May, 21, 12, 0))
	got, _ := skill.Execute(context.Background(), assist.ToolCall{Locale: "en"})
	if got.Text != "It is 12:00 PM." {
		t.Errorf("noon = %q, want 'It is 12:00 PM.'", got.Text)
	}
}

func TestTimeSkill_DefaultLocaleIsEnglish(t *testing.T) {
	skill := NewTimeSkill().WithClock(fixedClock(2026, time.May, 21, 14, 30))
	got, _ := skill.Execute(context.Background(), assist.ToolCall{Locale: ""})
	if !strings.Contains(got.Text, "PM") {
		t.Errorf("empty locale should fall back to EN/12h, got %q", got.Text)
	}
}

func TestTimeSkill_IntentIsTime(t *testing.T) {
	if NewTimeSkill().Intent() != shortcuts.IntentTime {
		t.Errorf("expected IntentTime")
	}
}

func TestDateSkill_DE_FormatsFullDate(t *testing.T) {
	skill := NewDateSkill().WithClock(fixedClock(2026, time.May, 21, 14, 30))
	got, err := skill.Execute(context.Background(), assist.ToolCall{Locale: "de"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 2026-05-21 is a Thursday.
	want := "Heute ist Donnerstag, der 21. Mai 2026."
	if got.Text != want {
		t.Errorf("Text = %q, want %q", got.Text, want)
	}
}

func TestDateSkill_EN_FormatsFullDate(t *testing.T) {
	skill := NewDateSkill().WithClock(fixedClock(2026, time.May, 21, 14, 30))
	got, err := skill.Execute(context.Background(), assist.ToolCall{Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Today is Thursday, May 21, 2026."
	if got.Text != want {
		t.Errorf("Text = %q, want %q", got.Text, want)
	}
}

func TestDateSkill_IntentIsDate(t *testing.T) {
	if NewDateSkill().Intent() != shortcuts.IntentDate {
		t.Errorf("expected IntentDate")
	}
}

func TestTimeSkill_WithClock_NilFallsBackToTimeNow(t *testing.T) {
	skill := NewTimeSkill().WithClock(nil)
	// Should not panic, returns *some* current time-formatted string.
	got, err := skill.Execute(context.Background(), assist.ToolCall{Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got.Text, "It is ") {
		t.Errorf("expected EN time format, got %q", got.Text)
	}
}
