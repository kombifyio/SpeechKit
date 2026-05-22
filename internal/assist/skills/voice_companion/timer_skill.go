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

// TimerSkill handles "set a timer for X minutes" / "stell einen timer
// auf X minuten" requests. Phase 1 of the Voice-Companion roadmap
// targets the simple case: parse a duration from the payload, return a
// spoken confirmation, and (when a TimerSink is wired) schedule a
// fire-and-forget callback that the host plays back as a sound or
// re-uses for desktop notifications.
//
// The skill stays useful even without a TimerSink — it confirms the
// duration verbally so the user has audio feedback that the request
// landed. The host upgrades it to a "real" timer by passing a sink
// implementation.
type TimerSkill struct {
	sink TimerSink
}

// TimerSink is the host-provided integration point for scheduled
// timers. Implementations decide where the alarm lives (in-memory
// goroutine on the desktop, Postgres-backed worker on the server,
// system-clock service on Linux). When sink is nil, the skill still
// answers verbally — the user gets confirmation, just no actual
// alarm.
type TimerSink interface {
	// Schedule arranges for OnFire to be invoked once the given
	// duration elapses, on a host-controlled goroutine. Returns a
	// stable timer ID for cancellation / status queries.
	Schedule(ctx context.Context, duration time.Duration, label, locale string) (string, error)
}

// NewTimerSkill returns a TimerSkill with no sink — answers verbally
// only. Use WithSink to attach a host scheduler.
func NewTimerSkill() *TimerSkill { return &TimerSkill{} }

// WithSink returns a copy of the skill that delegates to the given
// sink for scheduling. nil disables scheduling and keeps the verbal
// confirmation behaviour.
func (s *TimerSkill) WithSink(sink TimerSink) *TimerSkill {
	out := *s
	out.sink = sink
	return &out
}

// Intent reports the shortcuts.Intent this skill handles.
func (s *TimerSkill) Intent() shortcuts.Intent { return shortcuts.IntentTimer }

// Execute runs the skill against a single ToolCall.
//
// v0.38.0 multi-turn: when ParseDuration cannot find a duration in
// the payload AND the call is a fresh turn (Context does not carry
// a prior "awaiting_duration=1" marker), the skill asks "Für wie
// lange?" / "How long?" and sets FollowupNeeded=true. The pipeline
// stores the state and on the user's next utterance routes the new
// transcript back to this skill, where the payload is re-parsed.
// If the second attempt also fails, fall through to the LLM rather
// than loop forever.
func (s *TimerSkill) Execute(ctx context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	duration, label, ok := ParseDuration(call.Payload, call.Locale)
	if !ok || duration <= 0 {
		// Try again with the full transcript — for a follow-up turn
		// the payload IS the answer ("5 minuten") but the resolver
		// may have stripped the verbal prefix. Be tolerant.
		if duration, label, ok = ParseDuration(call.Transcript, call.Locale); !ok || duration <= 0 {
			// First contact — ask the follow-up question. If we're
			// already inside a follow-up turn (see Context marker
			// below), fall through to the LLM instead of asking
			// again.
			if strings.Contains(call.Context, "awaiting_duration=1") {
				return silentResult(call.Locale), nil
			}
			question := timerFollowupQuestion(call.Locale)
			return assist.ToolResult{
				Text:           question,
				SpeakText:      question,
				Action:         "respond",
				Locale:         call.Locale,
				Surface:        assist.ResultSurfacePanel,
				Kind:           assist.ResultKindAnswer,
				FollowupNeeded: true,
				FollowupState:  map[string]string{"awaiting_duration": "1"},
			}, nil
		}
	}

	if s.sink != nil {
		if _, err := s.sink.Schedule(ctx, duration, label, call.Locale); err != nil {
			return assist.ToolResult{}, fmt.Errorf("timer: schedule: %w", err)
		}
	}

	text := formatTimerConfirmation(duration, call.Locale)
	return assist.ToolResult{
		Text:      text,
		SpeakText: text,
		Action:    "execute",
		Locale:    call.Locale,
		Surface:   assist.ResultSurfaceActionAck,
		Kind:      assist.ResultKindUtilityAction,
	}, nil
}

// timerFollowupQuestion returns the locale-appropriate question that
// asks the user how long the timer should run. Kept short — the user
// is waiting for the answer prompt before speaking the duration.
func timerFollowupQuestion(locale string) string {
	if normalizeLocale(locale) == "de" {
		return "Für wie lange?"
	}
	return "How long?"
}

// ParseDuration interprets a spoken duration phrase such as "auf 5
// minuten" / "for 10 seconds" / "1 stunde 30 minuten" / "for 1 hour"
// and returns the total time.Duration plus a clean label suitable for
// notifications. Exported so future skills (reminder, alarm) can reuse
// the same parser without duplicating the regex.
func ParseDuration(payload, locale string) (time.Duration, string, bool) {
	expr := strings.ToLower(strings.TrimSpace(payload))
	for _, prefix := range []string{"auf ", "fuer ", "für ", "for ", "of "} {
		if strings.HasPrefix(expr, prefix) {
			expr = strings.TrimSpace(expr[len(prefix):])
			break
		}
	}
	if expr == "" {
		return 0, "", false
	}

	matches := durationPartRegex.FindAllStringSubmatch(expr, -1)
	if len(matches) == 0 {
		return 0, "", false
	}

	var total time.Duration
	for _, m := range matches {
		valueStr := strings.ReplaceAll(strings.TrimSpace(m[1]), ",", ".")
		unitStr := strings.TrimSpace(m[2])
		value, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			return 0, "", false
		}
		unitMult, ok := durationUnitMultiplier(unitStr)
		if !ok {
			return 0, "", false
		}
		total += time.Duration(value * float64(unitMult))
	}
	if total <= 0 {
		return 0, "", false
	}

	label := formatTimerLabel(total, locale)
	return total, label, true
}

// durationPartRegex matches one "<number> <unit>" pair. Numbers can be
// integer or decimal (decimal comma is accepted because we lowercase
// before matching but do NOT swap comma→dot — instead the matcher
// accepts both, the ParseFloat below would fail on comma so we
// pre-normalise via ReplaceAll there).
var durationPartRegex = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*([a-zäöü]+)`)

// durationUnitMultiplier maps a spoken unit to time.Duration multiplier
// in nanoseconds (the time.Duration base unit). DE+EN coverage.
func durationUnitMultiplier(unit string) (time.Duration, bool) {
	switch unit {
	case "sekunde", "sekunden", "sek", "second", "seconds", "sec", "s":
		return time.Second, true
	case "minute", "minuten", "min", "minutes", "m":
		return time.Minute, true
	case "stunde", "stunden", "h", "hour", "hours", "hr":
		return time.Hour, true
	}
	return 0, false
}

// formatTimerConfirmation renders the "Timer set to <duration>" reply.
func formatTimerConfirmation(d time.Duration, locale string) string {
	label := formatTimerLabel(d, locale)
	switch normalizeLocale(locale) {
	case "de":
		return fmt.Sprintf("Timer auf %s gesetzt.", label)
	default:
		return fmt.Sprintf("Timer set for %s.", label)
	}
}

// formatTimerLabel renders the duration as "5 minutes" / "1 hour 30
// minutes" depending on locale. Used both inside the confirmation and
// as the sink label for notification text.
func formatTimerLabel(d time.Duration, locale string) string {
	h := int64(d / time.Hour)
	m := int64((d % time.Hour) / time.Minute)
	s := int64((d % time.Minute) / time.Second)
	parts := []string{}

	de := normalizeLocale(locale) == "de"
	add := func(value int64, deSingular, dePlural, enSingular, enPlural string) {
		if value <= 0 {
			return
		}
		if de {
			unit := dePlural
			if value == 1 {
				unit = deSingular
			}
			parts = append(parts, fmt.Sprintf("%d %s", value, unit))
		} else {
			unit := enPlural
			if value == 1 {
				unit = enSingular
			}
			parts = append(parts, fmt.Sprintf("%d %s", value, unit))
		}
	}
	add(h, "Stunde", "Stunden", "hour", "hours")
	add(m, "Minute", "Minuten", "minute", "minutes")
	add(s, "Sekunde", "Sekunden", "second", "seconds")

	if len(parts) == 0 {
		if de {
			return "0 Sekunden"
		}
		return "0 seconds"
	}
	if de {
		return strings.Join(parts, " und ")
	}
	return strings.Join(parts, " and ")
}
