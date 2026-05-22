package voice_companion

import (
	"context"
	"fmt"
	"time"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

// TimeSkill answers "what time is it" / "wie spaet ist es" with the
// current local clock, formatted per locale (de = 24h, en = 12h AM/PM).
//
// The skill is deterministic and runs entirely in-process; the only
// external input is the system clock, which can be replaced via
// WithClock for tests.
type TimeSkill struct {
	now func() time.Time
}

// NewTimeSkill returns a TimeSkill backed by time.Now.
func NewTimeSkill() *TimeSkill {
	return &TimeSkill{now: time.Now}
}

// WithClock returns a copy of the skill that reads the current time
// from the supplied function. Use in tests; the production constructor
// already wires time.Now.
func (s *TimeSkill) WithClock(now func() time.Time) *TimeSkill {
	if now == nil {
		now = time.Now
	}
	return &TimeSkill{now: now}
}

// Intent reports the shortcuts.Intent this skill handles.
func (s *TimeSkill) Intent() shortcuts.Intent { return shortcuts.IntentTime }

// Execute runs the skill against a single ToolCall.
func (s *TimeSkill) Execute(_ context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	now := s.now().Local()
	text := formatTime(now, call.Locale)
	return assist.ToolResult{
		Text:      text,
		SpeakText: text,
		Action:    "respond",
		Locale:    call.Locale,
		Surface:   assist.ResultSurfacePanel,
		Kind:      assist.ResultKindAnswer,
	}, nil
}

// DateSkill answers "what day is it" / "welcher tag ist heute" with the
// current weekday + calendar date in the caller's locale.
type DateSkill struct {
	now func() time.Time
}

// NewDateSkill returns a DateSkill backed by time.Now.
func NewDateSkill() *DateSkill { return &DateSkill{now: time.Now} }

// WithClock returns a copy of the skill that reads the current time
// from the supplied function. Use in tests.
func (s *DateSkill) WithClock(now func() time.Time) *DateSkill {
	if now == nil {
		now = time.Now
	}
	return &DateSkill{now: now}
}

// Intent reports the shortcuts.Intent this skill handles.
func (s *DateSkill) Intent() shortcuts.Intent { return shortcuts.IntentDate }

// Execute runs the skill against a single ToolCall.
func (s *DateSkill) Execute(_ context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	now := s.now().Local()
	text := formatDate(now, call.Locale)
	return assist.ToolResult{
		Text:      text,
		SpeakText: text,
		Action:    "respond",
		Locale:    call.Locale,
		Surface:   assist.ResultSurfacePanel,
		Kind:      assist.ResultKindAnswer,
	}, nil
}

// formatTime renders the clock in the locale's idiomatic style. EN uses
// 12-hour with AM/PM; DE uses 24-hour with "Uhr" between hour and
// minute. Minutes are zero-padded so "9 Uhr 05" reads naturally.
func formatTime(now time.Time, locale string) string {
	h := now.Hour()
	m := now.Minute()
	switch normalizeLocale(locale) {
	case "de":
		return fmt.Sprintf("Es ist %d Uhr %02d.", h, m)
	default:
		h12 := h % 12
		if h12 == 0 {
			h12 = 12
		}
		suffix := "AM"
		if h >= 12 {
			suffix = "PM"
		}
		return fmt.Sprintf("It is %d:%02d %s.", h12, m, suffix)
	}
}

// formatDate renders the weekday + date in the locale's idiomatic style.
// DE: "Heute ist Montag, der 21. Mai 2026."
// EN: "Today is Thursday, May 21, 2026."
func formatDate(now time.Time, locale string) string {
	switch normalizeLocale(locale) {
	case "de":
		return fmt.Sprintf("Heute ist %s, der %d. %s %d.",
			germanWeekday(now.Weekday()),
			now.Day(),
			germanMonth(now.Month()),
			now.Year(),
		)
	default:
		return fmt.Sprintf("Today is %s, %s %d, %d.",
			now.Weekday().String(),
			now.Month().String(),
			now.Day(),
			now.Year(),
		)
	}
}

func germanWeekday(d time.Weekday) string {
	switch d {
	case time.Monday:
		return "Montag"
	case time.Tuesday:
		return "Dienstag"
	case time.Wednesday:
		return "Mittwoch"
	case time.Thursday:
		return "Donnerstag"
	case time.Friday:
		return "Freitag"
	case time.Saturday:
		return "Samstag"
	case time.Sunday:
		return "Sonntag"
	}
	return d.String()
}

func germanMonth(m time.Month) string {
	switch m {
	case time.January:
		return "Januar"
	case time.February:
		return "Februar"
	case time.March:
		return "Maerz"
	case time.April:
		return "April"
	case time.May:
		return "Mai"
	case time.June:
		return "Juni"
	case time.July:
		return "Juli"
	case time.August:
		return "August"
	case time.September:
		return "September"
	case time.October:
		return "Oktober"
	case time.November:
		return "November"
	case time.December:
		return "Dezember"
	}
	return m.String()
}
