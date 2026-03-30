package shortcuts

import (
	"strings"
)

type rule struct {
	intent Intent
	alias  string
}

var rules = []rule{
	// --- Copy Last: EN ---
	{intent: IntentCopyLast, alias: "copy last transcription"},
	{intent: IntentCopyLast, alias: "copy last"},
	{intent: IntentCopyLast, alias: "copy that"},
	{intent: IntentCopyLast, alias: "copy it"},
	// --- Copy Last: DE ---
	{intent: IntentCopyLast, alias: "kopiere das letzte"},
	{intent: IntentCopyLast, alias: "letzte kopieren"},
	{intent: IntentCopyLast, alias: "kopier das"},
	{intent: IntentCopyLast, alias: "in die zwischenablage"},
	{intent: IntentCopyLast, alias: "kopieren"},
	// --- Copy Last: FR ---
	{intent: IntentCopyLast, alias: "copier le dernier"},
	{intent: IntentCopyLast, alias: "copie ca"},
	{intent: IntentCopyLast, alias: "copier"},
	// --- Copy Last: ES ---
	{intent: IntentCopyLast, alias: "copiar lo ultimo"},
	{intent: IntentCopyLast, alias: "copiar eso"},
	{intent: IntentCopyLast, alias: "copiar"},

	// --- Insert Last: EN ---
	{intent: IntentInsertLast, alias: "insert last transcription"},
	{intent: IntentInsertLast, alias: "insert last"},
	{intent: IntentInsertLast, alias: "paste that"},
	{intent: IntentInsertLast, alias: "insert that"},
	{intent: IntentInsertLast, alias: "paste it"},
	// --- Insert Last: DE ---
	{intent: IntentInsertLast, alias: "fuege das letzte ein"},
	{intent: IntentInsertLast, alias: "letztes einfuegen"},
	{intent: IntentInsertLast, alias: "einfuegen"},
	{intent: IntentInsertLast, alias: "fueg das ein"},
	// --- Insert Last: FR ---
	{intent: IntentInsertLast, alias: "inserer le dernier"},
	{intent: IntentInsertLast, alias: "coller ca"},
	{intent: IntentInsertLast, alias: "inserer"},
	// --- Insert Last: ES ---
	{intent: IntentInsertLast, alias: "insertar lo ultimo"},
	{intent: IntentInsertLast, alias: "pegar eso"},
	{intent: IntentInsertLast, alias: "insertar"},

	// --- Summarize: EN ---
	{intent: IntentSummarize, alias: "summarize this"},
	{intent: IntentSummarize, alias: "summarise this"},
	{intent: IntentSummarize, alias: "summarize"},
	{intent: IntentSummarize, alias: "summarise"},
	{intent: IntentSummarize, alias: "summary"},
	{intent: IntentSummarize, alias: "give me a summary"},
	{intent: IntentSummarize, alias: "sum it up"},
	// --- Summarize: DE ---
	{intent: IntentSummarize, alias: "fassen wir dies zusammen"},
	{intent: IntentSummarize, alias: "zusammenfassung"},
	{intent: IntentSummarize, alias: "zusammenfassen"},
	{intent: IntentSummarize, alias: "fass zusammen"},
	{intent: IntentSummarize, alias: "fass das zusammen"},
	{intent: IntentSummarize, alias: "kurz zusammenfassen"},
	{intent: IntentSummarize, alias: "mach eine zusammenfassung"},
	// --- Summarize: FR ---
	{intent: IntentSummarize, alias: "resume ca"},
	{intent: IntentSummarize, alias: "resumer"},
	{intent: IntentSummarize, alias: "fais un resume"},
	{intent: IntentSummarize, alias: "resume"},
	// --- Summarize: ES ---
	{intent: IntentSummarize, alias: "resumir esto"},
	{intent: IntentSummarize, alias: "resumir"},
	{intent: IntentSummarize, alias: "haz un resumen"},
	{intent: IntentSummarize, alias: "resumen"},

	// --- Quick Note: EN ---
	{intent: IntentQuickNote, alias: "quick note"},
	{intent: IntentQuickNote, alias: "note"},
	{intent: IntentQuickNote, alias: "take a note"},
	{intent: IntentQuickNote, alias: "save note"},
	// --- Quick Note: DE ---
	{intent: IntentQuickNote, alias: "notiz"},
	{intent: IntentQuickNote, alias: "schnelle notiz"},
	{intent: IntentQuickNote, alias: "merke dir"},
	{intent: IntentQuickNote, alias: "notiere"},
	// --- Quick Note: FR ---
	{intent: IntentQuickNote, alias: "note rapide"},
	{intent: IntentQuickNote, alias: "prends une note"},
	{intent: IntentQuickNote, alias: "noter"},
	// --- Quick Note: ES ---
	{intent: IntentQuickNote, alias: "nota rapida"},
	{intent: IntentQuickNote, alias: "toma nota"},
	{intent: IntentQuickNote, alias: "anotar"},
}

var leadingFillers = []string{
	// EN
	"please",
	"could you please",
	"could you",
	"would you please",
	"would you",
	"can you please",
	"can you",
	// DE
	"bitte",
	"kannst du bitte",
	"kannst du",
	"koenntest du bitte",
	"koenntest du",
	"wuerdest du bitte",
	// FR
	"s'il te plait",
	"s'il vous plait",
	"est-ce que tu peux",
	"peux-tu",
	// ES
	"por favor",
	"puedes",
	"podrias",
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
