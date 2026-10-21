//go:build linux

package voiceagent

import (
	"strings"
	"time"
	_ "time/tzdata" // The server image may carry no zoneinfo; session clocks must still resolve.
)

const defaultSessionTimezone = "Europe/Berlin"

// sessionTimezone resolves the client's IANA zone, falling back to the
// product default when it is absent or unknown.
func sessionTimezone(name string) *time.Location {
	if name = strings.TrimSpace(name); name != "" && len(name) <= 64 {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	loc, err := time.LoadLocation(defaultSessionTimezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// appendSessionContext adds what a server-prompted session cannot know on
// its own: the current local date and time, and, when the session has no
// tools, that it must not announce checks or actions it cannot perform.
// Sessions that bring their own agent prompt (registered agents, own
// endpoints, providers running their own tools) are left untouched.
func appendSessionContext(prompt string, now time.Time, timezone, locale string, hasTools bool) string {
	loc := sessionTimezone(timezone)
	local := now.In(loc)
	var b strings.Builder
	b.WriteString(strings.TrimSpace(prompt))
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString("Date and time at the start of this session: ")
	b.WriteString(local.Format("Monday, 2 January 2006, 15:04"))
	b.WriteString(" (")
	b.WriteString(loc.String())
	b.WriteString("). Use it for questions about the time, date or day.")
	if locale = strings.TrimSpace(locale); locale != "" && len(locale) <= 35 && strings.Trim(locale, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") == "" {
		b.WriteString(" The session language is " + locale + ".")
	}
	b.WriteString(" Always reply in the language the user actually speaks, whatever this clock's region.")
	if !hasTools {
		b.WriteString("\nYou have no tools in this conversation: you cannot look anything up, check devices or take actions. " +
			"Never say that you will check, look up, do or come back to something. Answer directly from your own knowledge, " +
			"or say briefly and honestly that you cannot do that yet. End with a short question only when you need the user's answer or can " +
			"offer a concrete next step; never add a generic closer such as asking whether you can help with anything else.")
	}
	return b.String()
}
