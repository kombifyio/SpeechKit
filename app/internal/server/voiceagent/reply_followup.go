//go:build linux

package voiceagent

import (
	"strings"
	"unicode"
)

// replyFollowUpInvitations are final-sentence openings that invite an answer
// even when a provider's text drops the question mark. Lower-case; matched as
// a prefix of the reply's last sentence.
var replyFollowUpInvitations = []string{
	"wie kann ich", "was kann ich", "womit kann ich", "möchtest du", "willst du", "soll ich",
	"brauchst du", "hast du", "kannst du mir sagen", "magst du",
	"how can i", "what can i", "would you like", "do you want", "shall i", "should i",
	"is there anything", "anything else", "can you tell me", "let me know which", "let me know what",
}

// replyFollowUpImperatives ask the user to answer now ("Sag mir, welche
// Farbe", "Tell me which room") unless the sentence is a polite closer.
var replyFollowUpImperatives = []string{"sag mir", "bitte sag mir", "nenn mir", "verrat mir", "tell me", "please tell me"}

// replyIsPoliteCloser reports a conditional hand-off ("Sag mir Bescheid,
// wenn …", "Tell me if …") that leaves the next turn to the user's initiative.
func replyIsPoliteCloser(sentence string) bool {
	padded := " " + strings.NewReplacer(",", " ", ";", " ").Replace(sentence) + " "
	for _, word := range []string{" bescheid ", " wenn ", " falls ", " einfach ", " gerne ", " gern ", " if ", " when ", " whenever "} {
		if strings.Contains(padded, word) {
			return true
		}
	}
	return false
}

// replyExpectsAnswer reports whether an assistant reply hands the turn back
// for one more user turn: its last sentence is a question ("?", "？") or opens
// with an invitation to answer. Rules only, so it adds no latency or cost to
// the turn_end that carries it; statements, confirmations and ordinary
// answers return false.
func replyExpectsAnswer(text string) bool {
	text = strings.TrimRightFunc(strings.TrimSpace(text), func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(`"'”“’‘»«)]`, r) ||
			unicode.In(r, unicode.So, unicode.Sk, unicode.Mn, unicode.Cf)
	})
	last := strings.TrimRightFunc(text, func(r rune) bool { return strings.ContainsRune(".!…", r) })
	if last == "" {
		return false
	}
	if strings.HasSuffix(last, "?") || strings.HasSuffix(last, "？") {
		return true
	}
	if i := strings.LastIndexAny(last, ".!?…\n"); i >= 0 {
		last = last[i+1:]
	}
	last = strings.ToLower(strings.TrimSpace(last))
	for _, imperative := range replyFollowUpImperatives {
		if strings.HasPrefix(last, imperative+" ") || strings.HasPrefix(last, imperative+",") {
			return !replyIsPoliteCloser(last)
		}
	}
	for _, invitation := range replyFollowUpInvitations {
		if strings.HasPrefix(last, invitation) {
			return true
		}
	}
	return false
}
