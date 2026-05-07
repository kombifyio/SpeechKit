package agentkit

import "errors"

var (
	// ErrDuplicateTool is returned by ToolRegistry.Register when a tool with
	// the same name is already registered.
	ErrDuplicateTool = errors.New("agentkit: tool already registered")

	// ErrUnknownTool is returned when a ToolCall references a name that is
	// not present in the registry. Callers normally see this surfaced as a
	// JSON {"error": "..."} response sent back to the model.
	ErrUnknownTool = errors.New("agentkit: unknown tool")

	// ErrNilTool is returned by ToolRegistry.Register when the supplied
	// Tool is nil or has an empty Name.
	ErrNilTool = errors.New("agentkit: tool is nil or has empty name")
)
