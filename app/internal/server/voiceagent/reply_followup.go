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
	expects, _ := replyFollowUpDecision(text)
	return expects
}

// Rules that decide expects_reply, named for the dialog log. A question mark
// is the plain fallback; the phrase lists are the classifier proper.
const (
	followUpRuleNoText       = "no_text"
	followUpRuleQuestionMark = "question_mark"
	followUpRuleImperative   = "imperative_phrase"
	followUpRulePoliteCloser = "polite_closer"
	followUpRuleInvitation   = "invitation_phrase"
	followUpRuleQuestionThen = "question_then_short_statement"
	followUpRuleStatement    = "statement"
)

// replyAnswersItself reports a remark that answers the question before it
// ("Warum? Weil der Server neu startet."): a rhetorical question.
func replyAnswersItself(sentence string) bool {
	for _, opener := range []string{"weil ", "denn ", "because ", "since ", "porque ", "ja, ", "nein, ", "yes, ", "no, "} {
		if strings.HasPrefix(sentence, opener) {
			return true
		}
	}
	return false
}

// replyFollowUpTailWords bounds the statement that may follow a question and
// still leave the question open ("Welches Homelab meinst du? Ich sehe zwei.").
const replyFollowUpTailWords = 12

// replyFollowUpDecision is replyExpectsAnswer plus the rule that decided it.
func replyFollowUpDecision(text string) (bool, string) {
	text = strings.TrimRightFunc(strings.TrimSpace(text), func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(`"'”“’‘»«)]`, r) ||
			unicode.In(r, unicode.So, unicode.Sk, unicode.Mn, unicode.Cf)
	})
	last := strings.TrimRightFunc(text, func(r rune) bool { return strings.ContainsRune(".!…", r) })
	if last == "" {
		return false, followUpRuleNoText
	}
	if strings.HasSuffix(last, "?") || strings.HasSuffix(last, "？") {
		return true, followUpRuleQuestionMark
	}
	head := ""
	if i := strings.LastIndexAny(last, ".!?…\n"); i >= 0 {
		head = strings.TrimSpace(last[:i+1])
		last = last[i+1:]
	}
	last = strings.ToLower(strings.TrimSpace(last))
	for _, imperative := range replyFollowUpImperatives {
		if strings.HasPrefix(last, imperative+" ") || strings.HasPrefix(last, imperative+",") {
			if replyIsPoliteCloser(last) {
				return false, followUpRulePoliteCloser
			}
			return true, followUpRuleImperative
		}
	}
	for _, invitation := range replyFollowUpInvitations {
		if strings.HasPrefix(last, invitation) {
			return true, followUpRuleInvitation
		}
	}
	// An agent often asks and then adds one short remark; the question still
	// waits for the answer unless the remark hands the turn back to the user.
	if (strings.HasSuffix(head, "?") || strings.HasSuffix(head, "？")) &&
		len(strings.Fields(last)) <= replyFollowUpTailWords && !replyIsPoliteCloser(last) && !replyAnswersItself(last) {
		return true, followUpRuleQuestionThen
	}
	return false, followUpRuleStatement
}
