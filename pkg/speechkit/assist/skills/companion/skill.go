// Package companion provides the Voice-Companion skill implementations
// behind pkg/speechkit/assist/skills. Each skill answers one
// shortcuts.Intent value (e.g. IntentTime, IntentMath) and returns an
// assist.ToolResult ready to be spoken back to the user.
//
// Skills are pure-Go and free of host dependencies: they consume an
// assist.ToolCall (transcript + payload + locale) and produce a ToolResult.
// They never touch a UI, the system clipboard, or any platform-specific
// API. That keeps them embeddable in every SpeechKit deployment target
// (Device, Server, Local-Library) and in any host that imports the SDK.
//
// The CompositeExecutor dispatches an incoming ToolCall to the matching
// Skill or falls back to a host-supplied executor for the text-utility
// intents (copy_last, insert_last, summarize, ...). The skills catalog in
// the parent package wires it together with the utility registry; hosts
// normally use skills.New rather than this package directly.
//
// See docs/voice-companion.md for the Voice-Companion design overview.
package companion

import (
	"context"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist/shortcuts"
)

// Skill is the per-intent executor contract. Implementations are
// stateless beyond optional clock/HTTP-client injection for tests.
type Skill interface {
	// Intent returns the single shortcuts.Intent this skill handles.
	Intent() shortcuts.Intent

	// Execute runs the skill for a single ToolCall. Returning an empty
	// result with Surface silent (and Action "silent") signals "I matched
	// the intent but cannot answer this specific payload" — the Assist
	// service then falls through to its Generator. Returning an error
	// signals an unrecoverable failure (network down, parse error in an
	// internal table); the service surfaces it to the caller.
	Execute(ctx context.Context, call assist.ToolCall) (assist.ToolResult, error)
}

// CompositeExecutor implements assist.ToolExecutor by routing each call
// to the registered skill matching call.Intent, falling back to a
// host-provided executor when no skill matches. Use NewCompositeExecutor
// to construct.
type CompositeExecutor struct {
	skills   map[shortcuts.Intent]Skill
	fallback assist.ToolExecutor
}

// NewCompositeExecutor wires the given skills behind a single
// ToolExecutor. The fallback is consulted when no skill matches the
// incoming Intent; pass nil to make unmatched intents an error.
//
// Skill order is preserved for deterministic registration tracing but
// has no functional effect — each Intent maps to exactly one skill, the
// last one registered for that Intent wins.
func NewCompositeExecutor(skills []Skill, fallback assist.ToolExecutor) *CompositeExecutor {
	m := make(map[shortcuts.Intent]Skill, len(skills))
	for _, s := range skills {
		if s == nil {
			continue
		}
		intent := s.Intent()
		if intent == shortcuts.IntentNone {
			continue
		}
		m[intent] = s
	}
	return &CompositeExecutor{skills: m, fallback: fallback}
}

// ExecuteTool is the assist.ToolExecutor entry-point. It dispatches by
// call.Intent to a registered skill, falling back to the host executor
// when no skill matches.
func (c *CompositeExecutor) ExecuteTool(ctx context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	if c == nil {
		return assist.ToolResult{}, fmt.Errorf("companion: nil CompositeExecutor")
	}
	if skill, ok := c.skills[shortcuts.Intent(call.Intent)]; ok && skill != nil {
		return skill.Execute(ctx, call)
	}
	if c.fallback != nil {
		return c.fallback.ExecuteTool(ctx, call)
	}
	return assist.ToolResult{}, fmt.Errorf("companion: no skill registered for intent %q and no fallback configured", call.Intent)
}

// Intents reports the set of shortcuts.Intent values this executor can
// handle directly (excluding fallback). Useful for diagnostics and for
// flipping a utility's Enabled flag on a per-host basis.
func (c *CompositeExecutor) Intents() []shortcuts.Intent {
	if c == nil {
		return nil
	}
	out := make([]shortcuts.Intent, 0, len(c.skills))
	for intent := range c.skills {
		out = append(out, intent)
	}
	return out
}

// DefaultSkills returns the non-smart-home skill catalog (Time, Date,
// Math, Weather, Timer, Reminder, Wikipedia, Temperature). Hosts that route
// the standard smart-home intent must use AllSkills so the fail-closed Home
// Assistant semantic boundary is present even before credentials are
// configured.
//
// Hosts that want a subset can build their own CompositeExecutor by
// passing only the skills they need. Timer and Reminder run in
// "verbal-only" mode unless the host injects a TimerSink / ReminderSink
// via the skill's With* method before calling NewCompositeExecutor.
func DefaultSkills() []Skill {
	return []Skill{
		NewTimeSkill(),
		NewDateSkill(),
		NewMathSkill(),
		NewWeatherSkill(),
		NewTimerSkill(),
		NewReminderSkill(),
		NewWikipediaSkill(),
		NewTemperatureSkill(),
	}
}

// AllSkills returns DefaultSkills plus the Home Assistant semantic boundary.
// The boundary is present even when configuration is missing so recognized
// smart-home commands fail closed instead of falling through to a host LLM.
func AllSkills(haBaseURL, haToken string) []Skill {
	skills := DefaultSkills()
	return append(skills, NewHomeAssistantSkill(haBaseURL, haToken))
}

// normalizeLocale collapses "de"/"de-DE"/"DE_de" to "de" and everything
// else to "en". The built-in phrasings cover DE+EN; additional locales are
// added together with the corresponding catalog entries in
// pkg/speechkit/assist/shortcuts.
func normalizeLocale(locale string) string {
	l := strings.ToLower(strings.TrimSpace(locale))
	if l == "" {
		return "en"
	}
	if strings.HasPrefix(l, "de") {
		return "de"
	}
	return "en"
}
