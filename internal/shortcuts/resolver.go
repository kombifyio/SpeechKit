package shortcuts

import (
	"strings"
)

type rule struct {
	intent Intent
	alias  string
}

var rules = []rule{
	{intent: IntentCopyLast, alias: "copy last transcription"},
	{intent: IntentCopyLast, alias: "copy last"},
	{intent: IntentInsertLast, alias: "insert last transcription"},
	{intent: IntentInsertLast, alias: "insert last"},
	{intent: IntentSummarize, alias: "fassen wir dies zusammen"},
	{intent: IntentSummarize, alias: "bitte fassen wir dies zusammen"},
	{intent: IntentSummarize, alias: "zusammenfassung"},
	{intent: IntentSummarize, alias: "zusammenfassen"},
	{intent: IntentSummarize, alias: "bitte zusammenfassen"},
	{intent: IntentSummarize, alias: "fass zusammen"},
	{intent: IntentSummarize, alias: "summary"},
	{intent: IntentSummarize, alias: "summarize this"},
	{intent: IntentSummarize, alias: "summarise this"},
	{intent: IntentSummarize, alias: "summarize"},
	{intent: IntentSummarize, alias: "summarise"},
}

var leadingFillers = []string{
	"please",
	"bitte",
	"kannst du bitte",
	"kannst du",
	"could you please",
	"could you",
	"would you please",
	"would you",
}

func Resolve(text string) Resolution {
	normalized := normalize(text)
	normalized = stripLeadingFillers(normalized)
	if normalized == "" {
		return Resolution{}
	}

	for _, rule := range rules {
		if alias, payload, ok := matchPrefix(normalized, rule.alias); ok {
			return Resolution{
				Intent:  rule.intent,
				Alias:   alias,
				Payload: payload,
			}
		}
	}

	return Resolution{}
}

func normalize(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	fields := strings.Fields(text)
	return strings.Join(fields, " ")
}

func stripLeadingFillers(text string) string {
	for _, filler := range leadingFillers {
		if text == filler {
			return ""
		}
		if strings.HasPrefix(text, filler+" ") {
			return strings.TrimSpace(strings.TrimPrefix(text, filler))
		}
	}
	return text
}

func matchPrefix(text, alias string) (string, string, bool) {
	if text == alias {
		return alias, "", true
	}

	if strings.HasPrefix(text, alias+" ") {
		return alias, strings.TrimSpace(strings.TrimPrefix(text, alias)), true
	}

	if strings.HasPrefix(text, alias+",") || strings.HasPrefix(text, alias+":") || strings.HasPrefix(text, alias+".") || strings.HasPrefix(text, alias+"!") || strings.HasPrefix(text, alias+"?") {
		payload := strings.TrimLeft(text[len(alias):], " ,:.!?")
		return alias, payload, true
	}

	return "", "", false
}
