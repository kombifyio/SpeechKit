//go:build linux

package voiceagent

import (
	"log/slog"
	"strings"
	"sync/atomic"
	"time"
)

// dialogStats counts what explains how a dialog ended: how many replies the
// session produced, how many of them handed the turn back (expects_reply) and
// what the last decision was. The device decides from expects_reply whether to
// keep listening, so this is the server half of any "the dialog ended after
// one answer" diagnosis. Only the ordered output path writes it; the session
// end reads it after the pumps stopped.
type dialogStats struct {
	turns       atomic.Int64
	followUps   atomic.Int64
	interrupted atomic.Int64
	// lastExpects is 0 before any decided reply, then 1 (false) or 2 (true).
	lastExpects atomic.Int32
	lastRule    atomic.Value // string
	endLogged   atomic.Bool
	// startedAtMs is the wall clock of the start frame (Unix ms); a prepared
	// session idles before it, so its creation time would inflate durations.
	startedAtMs atomic.Int64

	// The signals the session outcome is classified from (classifyOutcome).
	// startAccepted: a valid start frame left standby. providerConnected: the
	// provider leg connected. answered: replies that carried text. userTurns:
	// the caller's final transcripts. clientClosed: the client closed or
	// stopped the session. providerEnded: the provider closed its leg without
	// an error. failure: the first provider failure code of the session.
	startAccepted     atomic.Bool
	providerConnected atomic.Bool
	answered          atomic.Int64
	userTurns         atomic.Int64
	clientClosed      atomic.Bool
	providerEnded     atomic.Bool
	failure           atomic.Value // string
	// firstFinalAtMs and firstAudioAtMs (Unix ms) time the caller's first
	// final transcript and the first reply audio sent after it.
	firstFinalAtMs atomic.Int64
	firstAudioAtMs atomic.Int64
}

// Session outcomes, one per session (SessionOutcome.Outcome).
const (
	OutcomeAnswered       = "answered"
	OutcomeStandbyExpired = "standby_expired"
	OutcomeClientClosed   = "client_closed"
	OutcomeProviderIdle   = "provider_idle"
	OutcomeProviderFailed = "provider_failed"
)

// SessionOutcome is the classified end of one voice session: whether the
// person got a reply and, when not, which side ended it. It carries counts,
// durations and fixed codes only, never transcript, audio or free text.
type SessionOutcome struct {
	SessionID string
	Outcome   string
	// EndReason is the session_end wire reason, unchanged for clients.
	EndReason string
	Route     string
	Provider  string
	// FailureCode is the first provider failure code, "" without one.
	FailureCode   string
	Turns         int64
	AnsweredTurns int64
	UserTurns     int64
	Interrupted   int64
	// FirstAudioMs runs from the caller's first final transcript to the first
	// reply audio sent after it; -1 when none was sent.
	FirstAudioMs int64
	DurationMs   int64
}

// noteProviderFailure keeps the first provider failure of the session.
func (a *Adapter) noteProviderFailure(code string) {
	if code == "" {
		code = "provider_failed"
	}
	a.dialog.failure.CompareAndSwap(nil, code)
}

// noteUserFinal counts one final transcript of the caller.
func (a *Adapter) noteUserFinal() {
	a.dialog.userTurns.Add(1)
	a.dialog.firstFinalAtMs.CompareAndSwap(0, time.Now().UnixMilli())
}

// noteReplyAudio times the first reply audio after the caller's first final.
func (a *Adapter) noteReplyAudio() {
	if a.dialog.firstAudioAtMs.Load() == 0 && a.dialog.firstFinalAtMs.Load() > 0 {
		a.dialog.firstAudioAtMs.CompareAndSwap(0, time.Now().UnixMilli())
	}
}

// classifyOutcome decides the session outcome. A session that answered is
// answered whatever ended it. Otherwise: one that never left standby (or whose
// wake candidate was not confirmed) expired there; a provider failure, or a
// provider that never connected, failed; a client that closed or stopped (a
// DELETE or a dropped connection ends the request as "shutdown") closed it; a
// provider that closed its leg, or an idle or duration limit, ended idle.
// Server-side policy ends (authorization, budget, go-away) count as failed,
// with EndReason telling them apart.
func classifyOutcome(d *dialogStats, endReason string) string {
	failure, _ := d.failure.Load().(string)
	switch {
	case d.answered.Load() > 0:
		return OutcomeAnswered
	case !d.startAccepted.Load() || endReason == "wake_rejected":
		return OutcomeStandbyExpired
	case failure != "" || !d.providerConnected.Load():
		return OutcomeProviderFailed
	case d.clientClosed.Load() || endReason == "client_gone" || endReason == "client" || endReason == "shutdown":
		return OutcomeClientClosed
	case d.providerEnded.Load() || endReason == "idle" || endReason == "max_duration":
		return OutcomeProviderIdle
	default:
		return OutcomeProviderFailed
	}
}

// routeLabel names the route of this session for logs: the registered
// kombify-agent route ("agent"), an own endpoint ("direct") or the plain
// realtime Voice Agent ("fast").
func (a *Adapter) routeLabel() string {
	if a.Session == nil {
		return "unknown"
	}
	switch {
	case a.Session.VoiceAgentBinding.DirectEndpoint != nil:
		return "direct"
	case a.Session.VoiceAgentBinding.TargetAgentID != "":
		return "agent"
	default:
		return "fast"
	}
}

func (a *Adapter) logSessionID() string {
	if a.Session == nil {
		return ""
	}
	return a.Session.ID
}

// logDialogTurn records one finished reply: whether it expects an answer and
// which rule decided. No transcript text, only its length.
func (a *Adapter) logDialogTurn(expects *bool, rule string, replyChars int) {
	turn := a.dialog.turns.Add(1)
	if replyChars > 0 {
		a.dialog.answered.Add(1)
	}
	label := "none"
	if expects != nil {
		if *expects {
			label = "true"
			a.dialog.followUps.Add(1)
			a.dialog.lastExpects.Store(2)
		} else {
			label = "false"
			a.dialog.lastExpects.Store(1)
		}
	}
	if expects != nil {
		a.dialog.lastRule.Store(rule)
	}
	slog.Info("voiceagent: dialog turn",
		"session_id", a.logSessionID(),
		"route", a.routeLabel(),
		"turn", turn,
		"expects_reply", label,
		"rule", rule,
		"follow_ups", a.dialog.followUps.Load(),
		"reply_chars", replyChars,
	)
}

// logSessionEnd records why the session ended together with the dialog
// counters, once per session.
func (a *Adapter) logSessionEnd(reason string) {
	if !a.dialog.endLogged.CompareAndSwap(false, true) {
		return
	}
	lastExpects := "none"
	switch a.dialog.lastExpects.Load() {
	case 1:
		lastExpects = "false"
	case 2:
		lastExpects = "true"
	}
	lastRule, _ := a.dialog.lastRule.Load().(string)
	var durationMs int64
	if started := a.dialog.startedAtMs.Load(); started > 0 {
		durationMs = time.Now().UnixMilli() - started
	}
	provider := ""
	if a.Provider != nil {
		provider = normalizeProviderName(a.Provider.Name())
	}
	outcome := SessionOutcome{
		SessionID: a.logSessionID(), Outcome: classifyOutcome(&a.dialog, reason), EndReason: reason,
		Route: a.routeLabel(), Provider: provider, Turns: a.dialog.turns.Load(), AnsweredTurns: a.dialog.answered.Load(),
		UserTurns: a.dialog.userTurns.Load(), Interrupted: a.dialog.interrupted.Load(), FirstAudioMs: -1, DurationMs: durationMs,
	}
	outcome.FailureCode, _ = a.dialog.failure.Load().(string)
	if final, audio := a.dialog.firstFinalAtMs.Load(), a.dialog.firstAudioAtMs.Load(); final > 0 && audio >= final {
		outcome.FirstAudioMs = audio - final
	}
	if a.OnOutcome != nil {
		a.OnOutcome(outcome)
	}
	slog.Info("voiceagent: session end",
		"session_id", a.logSessionID(),
		"route", a.routeLabel(),
		"provider", provider,
		"reason", reason,
		"outcome", outcome.Outcome,
		"failure", outcome.FailureCode,
		"answered", outcome.AnsweredTurns,
		"user_turns", outcome.UserTurns,
		"first_audio_ms", outcome.FirstAudioMs,
		"turns", a.dialog.turns.Load(),
		"follow_ups", a.dialog.followUps.Load(),
		"interrupted", a.dialog.interrupted.Load(),
		"last_expects_reply", lastExpects,
		"last_rule", lastRule,
		"duration_ms", durationMs,
	)
}

// sessionBusyMessage is the sentence a device may show or speak when its own
// running-conversation cap is reached. Other devices of the account never
// count (D-2026-10-26-14). Short and complete without a status code; locale
// is a BCP-47 tag or empty.
func sessionBusyMessage(locale string) string {
	tag := strings.ToLower(strings.TrimSpace(locale))
	switch {
	case strings.HasPrefix(tag, "de"):
		return "Auf diesem Gerät laufen schon zu viele Gespräche. Beende eines oder versuche es gleich noch einmal."
	case strings.HasPrefix(tag, "es"):
		return "Este dispositivo ya tiene demasiadas conversaciones en curso. Termina una o inténtalo de nuevo en un momento."
	default:
		return "This device already has too many voice conversations running. End one or try again in a moment."
	}
}

// globalLimitRetryAfter is the Retry-After (seconds) of a refusal at the
// global abuse ceiling (platform/standards/API-FIRST-STANDARD.md §18.4).
const globalLimitRetryAfter = "30"

// serviceCapacityMessage is the sentence for a refusal at the global abuse
// ceiling: an overall service limit, never a missing right of the account.
func serviceCapacityMessage(locale string) string {
	tag := strings.ToLower(strings.TrimSpace(locale))
	switch {
	case strings.HasPrefix(tag, "de"):
		return "Der Sprachdienst ist gerade insgesamt ausgelastet. Versuche es gleich noch einmal."
	case strings.HasPrefix(tag, "es"):
		return "El servicio de voz está al límite de su capacidad. Inténtalo de nuevo en un momento."
	default:
		return "The voice service is at its overall capacity right now. Try again in a moment."
	}
}
