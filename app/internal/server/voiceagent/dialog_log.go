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
	slog.Info("voiceagent: session end",
		"session_id", a.logSessionID(),
		"route", a.routeLabel(),
		"provider", provider,
		"reason", reason,
		"turns", a.dialog.turns.Load(),
		"follow_ups", a.dialog.followUps.Load(),
		"interrupted", a.dialog.interrupted.Load(),
		"last_expects_reply", lastExpects,
		"last_rule", lastRule,
		"duration_ms", durationMs,
	)
}

// sessionBusyMessage is the sentence a device may show or speak when the
// account's running-conversation cap is reached. Short and complete without a
// status code; locale is a BCP-47 tag or empty.
func sessionBusyMessage(locale string) string {
	tag := strings.ToLower(strings.TrimSpace(locale))
	switch {
	case strings.HasPrefix(tag, "de"):
		return "Gerade läuft schon ein anderes Gespräch auf deinem Konto. Beende es oder versuche es gleich noch einmal."
	case strings.HasPrefix(tag, "es"):
		return "Ya hay otra conversación en curso en tu cuenta. Termínala o inténtalo de nuevo en un momento."
	default:
		return "Another voice conversation is already running on your account. End it or try again in a moment."
	}
}
