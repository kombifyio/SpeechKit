package assist

import "strings"

// ContextParts are the host-supplied fragments ComposeContext folds into one
// AssistRequest.Context block. Adapters populate them from the OS
// (Device-Target) or the request body (Server-Target); the Service and the
// skills never need to know the prompt format.
type ContextParts struct {
	// ActiveApp names the foreground application the user is in when they
	// trigger Assist, e.g. "Visual Studio Code" or "chrome".
	ActiveApp string
	// WindowTitle is the foreground window's title bar text.
	WindowTitle string
	// Context is free-form context (speaker labels, host hints, ...).
	Context string
	// VocabularyHint carries the user's resolved Words/Replacements
	// customization as a short guidance sentence ("Prefer these names and
	// product terms ...: Kombify, SpeechKit."). It is the Assist-LLM-prompt leg
	// of the customization story; empty means no customization terms.
	VocabularyHint string
}

// ComposeContext folds the structured active-window fields, the free-form
// context, and the vocabulary hint into a single labelled block for the
// ToolCall and the Generator. Order: active-window block, free-form context,
// then the vocabulary guidance last. Whitespace-only fields are dropped and
// the result is "" when nothing is set.
func ComposeContext(parts ContextParts) string {
	var lines []string
	if app := strings.TrimSpace(parts.ActiveApp); app != "" {
		lines = append(lines, "Active application: "+app)
	}
	if title := strings.TrimSpace(parts.WindowTitle); title != "" {
		lines = append(lines, "Active window: "+title)
	}
	if free := strings.TrimSpace(parts.Context); free != "" {
		lines = append(lines, free)
	}
	if hint := strings.TrimSpace(parts.VocabularyHint); hint != "" {
		lines = append(lines, hint)
	}
	return strings.Join(lines, "\n")
}
