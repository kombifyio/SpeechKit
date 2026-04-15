package flows

import (
	"fmt"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/genkit"
)

// SummarizeToolInput is the input schema for the summarize tool (used by the agent).
type SummarizeToolInput struct {
	Text        string `json:"text" jsonschema_description:"Text to summarize"`
	Instruction string `json:"instruction,omitempty" jsonschema_description:"Optional instruction for how to summarize"`
}

// DefineSummarizeTool creates a tool that the agent can use to summarize text.
// It delegates to the summarize flow, avoiding logic duplication.
func DefineSummarizeTool(g *genkit.Genkit, summarizeFlow *core.Flow[SummarizeInput, string, struct{}]) ai.ToolRef {
	return genkit.DefineTool(g, "summarize", "Summarize or transform text according to instructions",
		func(ctx *ai.ToolContext, input SummarizeToolInput) (string, error) {
			if input.Text == "" {
				return "", fmt.Errorf("no text provided to summarize")
			}
			result, err := summarizeFlow.Run(ctx, SummarizeInput{
				Text:        input.Text,
				Instruction: input.Instruction,
				Locale:      "de", // default; agent context determines locale
			})
			if err != nil {
				return "", fmt.Errorf("summarize tool: %w", err)
			}
			return result, nil
		},
	)
}

// ClipboardReadInput is the input schema for clipboard read tool.
type ClipboardReadInput struct {
	// No input needed -- reads current clipboard.
}

// ClipboardWriteInput is the input schema for clipboard write tool.
type ClipboardWriteInput struct {
	Text string `json:"text" jsonschema_description:"Text to write to clipboard"`
}

// DefineClipboardReadTool creates a tool that reads the current clipboard content.
// The actual clipboard access is injected via the provided function to keep the tool testable.
func DefineClipboardReadTool(g *genkit.Genkit, readFn func() (string, error)) ai.ToolRef {
	return genkit.DefineTool(g, "readClipboard", "Read the current clipboard content",
		func(ctx *ai.ToolContext, input ClipboardReadInput) (string, error) {
			text, err := readFn()
			if err != nil {
				return "", fmt.Errorf("read clipboard: %w", err)
			}
			return text, nil
		},
	)
}

// DefineClipboardWriteTool creates a tool that writes text to the clipboard.
// The actual clipboard access is injected via the provided function.
func DefineClipboardWriteTool(g *genkit.Genkit, writeFn func(string) error) ai.ToolRef {
	return genkit.DefineTool(g, "writeClipboard", "Write text to the clipboard",
		func(ctx *ai.ToolContext, input ClipboardWriteInput) (string, error) {
			if input.Text == "" {
				return "", fmt.Errorf("no text to write")
			}
			if err := writeFn(input.Text); err != nil {
				return "", fmt.Errorf("write clipboard: %w", err)
			}
			return "Text written to clipboard", nil
		},
	)
}
