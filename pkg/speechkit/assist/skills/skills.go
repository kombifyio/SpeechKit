// Package skills exposes SpeechKit's Voice-Companion skill catalog — Time,
// Date, Math, Weather, Timer, Reminder, Wikipedia, Temperature, plus a
// fail-closed Home Assistant boundary — as a public [assist.ToolMatcher] +
// [assist.ToolExecutor] pair, ready to plug into an [assist.Service].
//
// It is the catalog the SpeechKit desktop app and the self-host server run;
// an external host (e.g. the kombify-box firmware companion) wires the same
// skills instead of re-implementing a keyword router. The skill
// implementations live in the companion subpackage; the codeword phrases they
// are matched against live in the shortcuts package, and the
// [UtilityRegistry] decides which intents are routable and which
// presentation defaults apply.
//
// Wiring:
//
//	cat := skills.New(skills.Options{HomeAssistantURL: url, HomeAssistantToken: tok})
//	svc, _ := assist.NewService(assist.Options{
//	    Matcher:   cat.Matcher(),
//	    Executor:  cat.Executor(),
//	    Generator: llm,          // optional: answers turns no skill matched
//	    TTSRouter: tts, TTSEnabled: true,
//	})
//
// Matched-but-silent behavior: a skill may match an intent yet decline the
// specific payload (e.g. Math on non-math text), returning an empty
// [speechkit.AssistSurfaceSilent] result. [assist.Service] then falls through
// to the Generator when one is configured (and the behavior is not clean);
// without a Generator the silent result is returned as-is.
//
// Home Assistant is the sole smart-home authority: a recognised smart-home
// utterance is always claimed by the catalog and terminates locally with a
// localized result when no bridge is configured, when Home Assistant reports
// no match, or when the bridge fails. It is never offered to the Generator.
package skills

import (
	"context"
	"errors"
	"fmt"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist/shortcuts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist/skills/companion"
)

// ErrSkillDisabled is returned (wrapped with the intent) when the catalog
// matched an intent whose skill the host disabled through
// Options.DisabledIntents. The utterance fails closed: it reaches neither the
// network nor the Generator.
var ErrSkillDisabled = errors.New("speechkit skills: skill is disabled by host policy")

// Options configures the skill catalog.
type Options struct {
	// HomeAssistantURL and HomeAssistantToken enable Home Assistant execution
	// when BOTH are set. Recognized smart-home commands still match when either
	// value is missing, but terminate locally with a safe unavailable result;
	// they are never offered to the host Generator.
	HomeAssistantURL   string
	HomeAssistantToken string

	// OnAlarm, when set, activates in-process scheduling for the Timer and
	// Reminder skills: instead of only confirming verbally, they schedule the
	// alarm and OnAlarm fires (on a background goroutine) when it elapses, so
	// the host can ring a sound or show a notification. When nil, Timer and
	// Reminder stay verbal-only. Call [Catalog.Close] to cancel pending alarms.
	OnAlarm func(Alarm)

	// Resolver overrides the codeword resolver; nil uses
	// shortcuts.DefaultResolver. Hosts pass a resolver built from a
	// DefaultRegistry clone extended with their configured aliases.
	Resolver *shortcuts.Resolver

	// Registry overrides the utility registry that decides which intents are
	// routable and which Surface/Kind defaults apply; nil uses
	// DefaultUtilityRegistry. The catalog works on a private copy and always
	// enables the Home Assistant entry (see the package comment).
	Registry *UtilityRegistry

	// DisabledIntents removes the built-in skills for the listed intents (for
	// example Weather and Wikipedia under a restricted network scope). The
	// intents stay matched so the utterance fails closed with ErrSkillDisabled
	// instead of reaching the network or the Generator. The Home Assistant
	// boundary cannot be disabled; an unconfigured bridge already fails closed.
	DisabledIntents []shortcuts.Intent

	// Fallback executes matched intents no built-in skill owns: the host text
	// utilities copy_last, insert_last, summarize and quick_note. When nil,
	// such a match is an error.
	Fallback assist.ToolExecutor
}

// Catalog is the built skill set exposed as a matcher + executor. Construct
// with [New]; the zero value is unusable.
type Catalog struct {
	matcher   *matcher
	executor  *executor
	scheduler *scheduler
}

// New builds the Voice-Companion catalog. The deterministic skills are always
// present unless disabled; the Home Assistant boundary is always present and
// executes commands only when Options provides a URL and token. When
// Options.OnAlarm is set, Timer and Reminder are backed by an in-process
// scheduler.
func New(opts Options) *Catalog {
	timer := companion.NewTimerSkill()
	reminder := companion.NewReminderSkill()
	var sched *scheduler
	if opts.OnAlarm != nil {
		sched = newScheduler(opts.OnAlarm)
		timer = timer.WithSink(timerSink{sched})
		reminder = reminder.WithSink(reminderSink{sched})
	}

	disabled := make(map[shortcuts.Intent]bool, len(opts.DisabledIntents))
	for _, intent := range opts.DisabledIntents {
		if intent != shortcuts.IntentNone && intent != shortcuts.IntentHomeAssistant {
			disabled[intent] = true
		}
	}

	all := []companion.Skill{
		companion.NewTimeSkill(),
		companion.NewDateSkill(),
		companion.NewMathSkill(),
		companion.NewWeatherSkill(),
		timer,
		reminder,
		companion.NewWikipediaSkill(),
		companion.NewTemperatureSkill(),
	}
	installed := make([]companion.Skill, 0, len(all)+1)
	for _, skill := range all {
		if !disabled[skill.Intent()] {
			installed = append(installed, skill)
		}
	}
	// Always install the Home Assistant boundary. Its unconfigured state is a
	// terminal denial, not permission to reinterpret a smart-home command with
	// a general-purpose generator.
	installed = append(installed, companion.NewHomeAssistantSkill(opts.HomeAssistantURL, opts.HomeAssistantToken))

	registry := opts.Registry.clone()
	if opts.Registry == nil {
		registry = DefaultUtilityRegistry()
	}
	// The matcher must always claim the Home Assistant intent so the skill's
	// fail-closed result is what the user hears when no bridge is configured.
	registry.Register(UtilityDefinition{
		ID:             UtilityHomeAssistant,
		Intent:         shortcuts.IntentHomeAssistant,
		Label:          "Send command to Home Assistant",
		Input:          UtilityInputUtterance,
		DefaultSurface: speechkit.AssistSurfaceActionAck,
		DefaultKind:    assist.KindUtilityAction,
		Enabled:        true,
	})

	resolver := opts.Resolver
	if resolver == nil {
		resolver = shortcuts.DefaultResolver()
	}

	return &Catalog{
		matcher: &matcher{resolver: resolver, registry: registry},
		executor: &executor{
			registry: registry,
			inner:    companion.NewCompositeExecutor(installed, opts.Fallback),
			disabled: disabled,
		},
		scheduler: sched,
	}
}

// Matcher returns the transcript→intent matcher for the catalog.
func (c *Catalog) Matcher() assist.ToolMatcher { return c.matcher }

// Executor returns the intent→result executor for the catalog.
func (c *Catalog) Executor() assist.ToolExecutor { return c.executor }

// Registry returns the effective utility registry: the host's registry (or
// the default) with the Home Assistant entry enabled. Hosts read it to report
// which utilities are routable.
func (c *Catalog) Registry() *UtilityRegistry {
	if c == nil || c.matcher == nil {
		return nil
	}
	return c.matcher.registry
}

// CanHandleLocally reports whether req maps to an enabled utility that does
// not need the Generator (UtilityDefinition.RequiresModel is false). Hosts
// without an Assist model use it to decide between running the request and
// explaining that no model is available.
func (c *Catalog) CanHandleLocally(req speechkit.AssistRequest) bool {
	if c == nil || c.matcher == nil {
		return false
	}
	dec := c.matcher.decide(req.Text, req.Locale)
	return dec.matched && !dec.utility.RequiresModel
}

// Close cancels any pending Timer/Reminder alarms scheduled by the built-in
// scheduler. It is a no-op when Options.OnAlarm was not set. Safe to call more
// than once; call it when the host shuts the catalog down.
func (c *Catalog) Close() {
	if c != nil && c.scheduler != nil {
		c.scheduler.Close()
	}
}

// executor dispatches to the companion skills (or the host fallback) and
// applies the registry's presentation defaults to every result.
type executor struct {
	registry *UtilityRegistry
	inner    *companion.CompositeExecutor
	disabled map[shortcuts.Intent]bool
}

// ExecuteTool implements assist.ToolExecutor.
func (e *executor) ExecuteTool(ctx context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	intent := shortcuts.Intent(call.Intent)
	if e.disabled[intent] {
		return assist.ToolResult{}, fmt.Errorf("%w: %s", ErrSkillDisabled, call.Intent)
	}
	if call.Transcript == "" {
		call.Transcript = call.Payload
	}
	result, err := e.inner.ExecuteTool(ctx, call)
	if err != nil {
		return assist.ToolResult{}, err
	}
	return e.applyDefaults(intent, result), nil
}

// applyDefaults fills Surface and Kind from the utility definition when the
// skill or fallback left them empty, so host fallbacks that only return text
// still present as the registry declares (action_ack + utility_action for
// copy/insert, panel + work_product for summarize).
func (e *executor) applyDefaults(intent shortcuts.Intent, result assist.ToolResult) assist.ToolResult {
	def, _ := e.registry.Definition(intent)
	if result.Surface == "" {
		result.Surface = def.DefaultSurface
		if result.Surface == "" {
			result.Surface = speechkit.AssistSurfaceActionAck
		}
	}
	if result.Kind == "" {
		result.Kind = def.DefaultKind
		if result.Kind == "" {
			result.Kind = assist.KindUtilityAction
		}
	}
	return result
}
