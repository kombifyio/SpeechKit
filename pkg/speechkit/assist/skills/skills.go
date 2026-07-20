// Package skills exposes SpeechKit's Voice-Companion skill catalog — Time,
// Date, Math, Weather, Timer, Reminder, Wikipedia, plus a fail-closed Home
// Assistant boundary — as a public [assist.ToolMatcher] + [assist.ToolExecutor]
// pair, ready to plug into an [assist.Service].
//
// It lets an external host (e.g. the kombify-box firmware companion) wire the
// REAL framework skills into its Assist flow instead of re-implementing a
// keyword router. The skill implementations are the same pure-Go ones the
// Device- and Server-Targets run; this package adapts them from the internal
// tool vocabulary to the public [assist] types.
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
// specific payload (e.g. Math on non-math text), returning an
// [speechkit.AssistSurfaceSilent] result. [assist.Service] returns that silent
// result as-is; it does NOT fall through to the Generator on a matched skill.
// Hosts that want an LLM answer in that case should treat a silent/empty result
// as "ask the model" in their own flow.
package skills

import (
	"context"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/assist/skills/voice_companion"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	pubassist "github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
)

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
}

// Catalog is the built skill set exposed as a matcher + executor. Construct
// with [New]; the zero value is unusable.
type Catalog struct {
	matcher   *matcher
	executor  *executor
	scheduler *scheduler
}

// New builds the Voice-Companion catalog. The seven deterministic skills are
// always present; the Home Assistant bridge is added when Options provides a
// URL and token. When Options.OnAlarm is set, Timer and Reminder are backed by
// an in-process scheduler.
func New(opts Options) *Catalog {
	timer := voice_companion.NewTimerSkill()
	reminder := voice_companion.NewReminderSkill()
	var sched *scheduler
	if opts.OnAlarm != nil {
		sched = newScheduler(opts.OnAlarm)
		timer = timer.WithSink(timerSink{sched})
		reminder = reminder.WithSink(reminderSink{sched})
	}

	skills := []voice_companion.Skill{
		voice_companion.NewTimeSkill(),
		voice_companion.NewDateSkill(),
		voice_companion.NewMathSkill(),
		voice_companion.NewWeatherSkill(),
		timer,
		reminder,
		voice_companion.NewWikipediaSkill(),
		voice_companion.NewTemperatureSkill(),
	}
	// Always install the Home Assistant boundary. Its unconfigured state is a
	// terminal denial, not permission to reinterpret a smart-home command with
	// a general-purpose generator.
	skills = append(skills, voice_companion.NewHomeAssistantSkill(opts.HomeAssistantURL, opts.HomeAssistantToken))
	inner := voice_companion.NewCompositeExecutor(skills, nil)

	reg := assist.DefaultUtilityRegistry()
	// The public Service does not use internal/assist.Pipeline's secondary
	// fail-closed guard, so its matcher must always claim the HA intent.
	reg.Register(assist.UtilityDefinition{
		ID:             assist.UtilityHomeAssistant,
		Intent:         shortcuts.IntentHomeAssistant,
		Label:          "Send command to Home Assistant",
		Input:          assist.UtilityInputUtterance,
		DefaultSurface: assist.ResultSurfaceActionAck,
		DefaultKind:    assist.ResultKindUtilityAction,
		Enabled:        true,
	})

	return &Catalog{
		matcher:   &matcher{router: assist.NewRouter(assist.WithUtilityRegistry(reg))},
		executor:  &executor{inner: inner},
		scheduler: sched,
	}
}

// Matcher returns the transcript→intent matcher for the catalog.
func (c *Catalog) Matcher() pubassist.ToolMatcher { return c.matcher }

// Executor returns the intent→result executor for the catalog.
func (c *Catalog) Executor() pubassist.ToolExecutor { return c.executor }

// Close cancels any pending Timer/Reminder alarms scheduled by the built-in
// scheduler. It is a no-op when Options.OnAlarm was not set. Safe to call more
// than once; call it when the host shuts the catalog down.
func (c *Catalog) Close() {
	if c != nil && c.scheduler != nil {
		c.scheduler.Close()
	}
}

// transcriptCarrier threads the full utterance from this package's matcher to
// its executor. The public [assist.ToolCall] has no Transcript field, but some
// skills (notably Home Assistant) need the whole command, not just the payload
// after the trigger phrase. It rides in ToolCall.Target — impl-specific by the
// [assist] contract — and is only ever set and read within this package.
type transcriptCarrier struct{ transcript string }

// matcher adapts the internal keyword router to the public ToolMatcher.
type matcher struct {
	router *assist.Router
}

func (m *matcher) MatchTool(_ context.Context, req speechkit.AssistRequest) (pubassist.ToolCall, bool, error) {
	dec := m.router.Decide(req.Text, assist.ProcessOpts{
		Locale:    req.Locale,
		Selection: req.Selection,
		Context:   req.Context,
	})
	if dec.Route != assist.RouteToolIntent || dec.Intent == shortcuts.IntentNone {
		return pubassist.ToolCall{}, false, nil
	}
	locale := dec.Locale
	if locale == "" {
		locale = req.Locale
	}
	return pubassist.ToolCall{
		Intent:    string(dec.Intent),
		Payload:   dec.Payload,
		Locale:    locale,
		Selection: req.Selection,
		Target:    transcriptCarrier{transcript: req.Text},
	}, true, nil
}

// executor adapts the internal CompositeExecutor to the public ToolExecutor.
type executor struct {
	inner *voice_companion.CompositeExecutor
}

func (e *executor) ExecuteTool(ctx context.Context, call pubassist.ToolCall) (pubassist.ToolResult, error) {
	// Recover the full transcript the matcher stashed; on the Service's
	// multi-turn follow-up path there is no carrier and Payload already holds
	// the full text.
	transcript := call.Payload
	target := call.Target
	if tc, ok := call.Target.(transcriptCarrier); ok {
		transcript = tc.transcript
		target = nil
	}

	res, err := e.inner.Execute(ctx, assist.ToolCall{
		Intent:     shortcuts.Intent(call.Intent),
		Payload:    call.Payload,
		Transcript: transcript,
		Locale:     call.Locale,
		Selection:  call.Selection,
		Context:    call.Context,
		Target:     target,
	})
	if err != nil {
		return pubassist.ToolResult{}, err
	}
	return toPublicResult(res), nil
}

// toPublicResult maps an internal ToolResult to the public shape. The surface
// enums share string values ("panel"/"action_ack"/"silent"), and Kind is a
// plain string in the public API.
func toPublicResult(r assist.ToolResult) pubassist.ToolResult {
	return pubassist.ToolResult{
		Text:           r.Text,
		SpeakText:      r.SpeakText,
		Action:         r.Action,
		Kind:           string(r.Kind),
		Surface:        speechkit.AssistSurfaceDecision(r.Surface),
		Locale:         r.Locale,
		MessageID:      r.MessageID,
		ReasonCode:     r.ReasonCode,
		FollowupNeeded: r.FollowupNeeded,
		FollowupState:  pubassist.NewFollowupState(r.FollowupState),
	}
}
