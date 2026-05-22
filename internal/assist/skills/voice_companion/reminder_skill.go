package voice_companion

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

// ReminderSkill answers "remind me to <text> at <time>" / "erinnere
// mich an <text> um <zeit>" by scheduling a future notification.
// Phase 1 handles the simple cases:
//   - "in 30 minutes" / "in 30 Minuten" — relative duration
//   - "at 17:30" / "um 17 Uhr 30" — absolute clock time today (or
//     tomorrow if the time already passed today)
//   - "tomorrow at 09:00" / "morgen um 9 Uhr" — explicit tomorrow
//
// Anything more complex (recurring rules, multi-day windows, natural
// dates) falls through to the LLM via Action="silent" — the LLM can
// either answer conversationally or call back into the reminder skill
// with a structured payload once tool-calling is wired.
//
// Persistence is host-controlled via ReminderSink. Without a sink the
// skill still confirms verbally so the user sees that the request
// landed, but the reminder never fires.
type ReminderSkill struct {
	sink ReminderSink
	now  func() time.Time
}

// ReminderSink is the host-provided integration point for persistent
// reminders. Implementations decide where the entries live (SQLite via
// internal/store, Render Postgres, file-based JSON).
type ReminderSink interface {
	Schedule(ctx context.Context, fireAt time.Time, label, locale string) (string, error)
}

// NewReminderSkill returns a ReminderSkill backed by time.Now and no
// sink. Use WithSink + WithClock to override.
func NewReminderSkill() *ReminderSkill {
	return &ReminderSkill{now: time.Now}
}

// WithSink returns a copy of the skill that delegates to the sink.
func (s *ReminderSkill) WithSink(sink ReminderSink) *ReminderSkill {
	out := *s
	out.sink = sink
	return &out
}

// WithClock overrides the time source for tests.
func (s *ReminderSkill) WithClock(now func() time.Time) *ReminderSkill {
	out := *s
	if now != nil {
		out.now = now
	}
	return &out
}

// Intent reports the shortcuts.Intent this skill handles.
func (s *ReminderSkill) Intent() shortcuts.Intent { return shortcuts.IntentReminder }

// Execute runs the skill against a single ToolCall.
func (s *ReminderSkill) Execute(ctx context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	label, fireAt, ok := parseReminderPayload(call.Payload, s.now(), call.Locale)
	if !ok {
		return silentResult(call.Locale), nil
	}

	if s.sink != nil {
		if _, err := s.sink.Schedule(ctx, fireAt, label, call.Locale); err != nil {
			return assist.ToolResult{}, fmt.Errorf("reminder: schedule: %w", err)
		}
	}

	text := formatReminderConfirmation(label, fireAt, call.Locale)
	return assist.ToolResult{
		Text:      text,
		SpeakText: text,
		Action:    "execute",
		Locale:    call.Locale,
		Surface:   assist.ResultSurfaceActionAck,
		Kind:      assist.ResultKindUtilityAction,
	}, nil
}

var (
	// reminderInRegex captures "...in 30 minuten" or "...in 2 hours".
	reminderInRegex = regexp.MustCompile(`(?i)^(.+?)\s+in\s+(\d+(?:[.,]\d+)?)\s*(sekunde|sekunden|sek|second|seconds|sec|minute|minuten|min|minutes|stunde|stunden|hour|hours)\s*$`)

	// reminderAtRegex captures "...um 17 Uhr 30" / "...at 17:30" /
	// "...um 9 uhr". Matches both DE Uhr-form and EN/DE colon-form.
	reminderAtRegex = regexp.MustCompile(`(?i)^(.+?)\s+(?:um|at)\s+(\d{1,2})(?:\s*(?:uhr|:)\s*(\d{1,2})?(?:\s*uhr)?)?$`)

	// reminderTomorrowRegex same as reminderAtRegex but with an
	// explicit "morgen"/"tomorrow" marker that pins fireAt to the next
	// calendar day.
	reminderTomorrowRegex = regexp.MustCompile(`(?i)^(.+?)\s+(?:morgen|tomorrow)\s+(?:um|at)\s+(\d{1,2})(?:\s*(?:uhr|:)\s*(\d{1,2})?(?:\s*uhr)?)?$`)
)

// parseReminderPayload extracts the label + fire-at time from a payload
// such as "an arzttermin morgen um 9 uhr". Returns ok=false when none
// of the supported patterns match.
func parseReminderPayload(payload string, now time.Time, locale string) (string, time.Time, bool) {
	expr := strings.TrimSpace(payload)
	expr = stripLeadingAnObjectMarker(expr)
	if expr == "" {
		return "", time.Time{}, false
	}

	if m := reminderTomorrowRegex.FindStringSubmatch(expr); m != nil {
		label := strings.TrimSpace(m[1])
		h, _ := strconv.Atoi(m[2])
		mins := 0
		if m[3] != "" {
			mins, _ = strconv.Atoi(m[3])
		}
		fireAt := dateAt(now.AddDate(0, 0, 1), h, mins)
		return label, fireAt, true
	}

	if m := reminderAtRegex.FindStringSubmatch(expr); m != nil {
		label := strings.TrimSpace(m[1])
		h, _ := strconv.Atoi(m[2])
		mins := 0
		if m[3] != "" {
			mins, _ = strconv.Atoi(m[3])
		}
		fireAt := dateAt(now, h, mins)
		// If the time is already past today, roll to tomorrow.
		if !fireAt.After(now) {
			fireAt = fireAt.Add(24 * time.Hour)
		}
		return label, fireAt, true
	}

	if m := reminderInRegex.FindStringSubmatch(expr); m != nil {
		label := strings.TrimSpace(m[1])
		valueStr := strings.ReplaceAll(strings.TrimSpace(m[2]), ",", ".")
		unit := strings.ToLower(strings.TrimSpace(m[3]))
		value, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			return "", time.Time{}, false
		}
		mult, ok := durationUnitMultiplier(unit)
		if !ok {
			return "", time.Time{}, false
		}
		dur := time.Duration(value * float64(mult))
		if dur <= 0 {
			return "", time.Time{}, false
		}
		fireAt := now.Add(dur)
		return label, fireAt, true
	}

	_ = locale // reserved for future locale-specific phrasing
	return "", time.Time{}, false
}

// stripLeadingAnObjectMarker removes the "an "/"to " connector that
// follows the wake-prefix "erinnere mich"/"remind me".
func stripLeadingAnObjectMarker(expr string) string {
	lower := strings.ToLower(expr)
	for _, prefix := range []string{"an ", "to "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(expr[len(prefix):])
		}
	}
	return expr
}

// dateAt clones the date portion of d and sets the clock to h:m:00.
func dateAt(d time.Time, h, m int) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, d.Location())
}

// formatReminderConfirmation renders the confirmation reply.
func formatReminderConfirmation(label string, fireAt time.Time, locale string) string {
	when := formatClock(fireAt, locale)
	switch normalizeLocale(locale) {
	case "de":
		return fmt.Sprintf("Erinnerung an %s um %s gesetzt.", label, when)
	default:
		return fmt.Sprintf("Reminder for %s at %s set.", label, when)
	}
}

// formatClock renders fireAt as a short clock+date phrase suitable for
// inclusion in a confirmation sentence. Same-day reminders read
// "17:30"; tomorrow reads "morgen 09:00" / "tomorrow 09:00".
func formatClock(fireAt time.Time, locale string) string {
	now := time.Now()
	sameDay := fireAt.Year() == now.Year() && fireAt.YearDay() == now.YearDay()
	clock := fmt.Sprintf("%02d:%02d", fireAt.Hour(), fireAt.Minute())
	if sameDay {
		return clock
	}
	switch normalizeLocale(locale) {
	case "de":
		return "morgen " + clock
	default:
		return "tomorrow " + clock
	}
}
