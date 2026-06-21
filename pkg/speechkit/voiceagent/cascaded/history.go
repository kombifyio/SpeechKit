package cascaded

import "strings"

type conversationTurn struct {
	User      string
	Assistant string
}

func renderHistory(history []conversationTurn) string {
	if len(history) == 0 {
		return ""
	}
	var b strings.Builder
	for _, t := range history {
		b.WriteString("User: ")
		b.WriteString(t.User)
		b.WriteString("\nAssistant: ")
		b.WriteString(t.Assistant)
		b.WriteString("\n")
	}
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func renderSystemPrompt(systemPrompt, refinement string) string {
	systemPrompt = strings.TrimSpace(systemPrompt)
	refinement = strings.TrimSpace(refinement)
	if refinement == "" {
		return systemPrompt
	}
	if systemPrompt == "" {
		return refinement
	}
	return systemPrompt + "\n\nPersonal refinement:\n" + refinement
}
