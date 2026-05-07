package agentkit

import (
	"context"
	"errors"
)

// Schema is a JSON Schema object describing a tool's input parameters.
// Use the standard JSON Schema vocabulary (type, properties, required, ...).
type Schema = map[string]any

// Tool is a host-side function the realtime voice agent may invoke.
//
// Implementations must be safe for concurrent calls; the agent runtime
// dispatches Invoke from a goroutine per ToolCall.
type Tool interface {
	// Name is the unique identifier the model sees. Must match across
	// ToolDefinition and ToolCall.Name.
	Name() string

	// Description is a short natural-language hint shown to the model so it
	// can decide when to invoke this tool.
	Description() string

	// InputSchema is the JSON Schema for the tool's arguments. Returning
	// nil is equivalent to "no parameters".
	InputSchema() Schema

	// Invoke runs the tool. The returned map is sent back to the model as
	// the tool response payload. A non-nil error is converted to
	// {"error": err.Error()} for the model.
	Invoke(ctx context.Context, args map[string]any) (map[string]any, error)
}

// FuncTool is a convenience Tool implementation backed by a closure.
// Useful for inline registration without defining a new struct.
type FuncTool struct {
	ToolName        string
	ToolDescription string
	ToolSchema      Schema
	Fn              func(ctx context.Context, args map[string]any) (map[string]any, error)
}

func (f *FuncTool) Name() string        { return f.ToolName }
func (f *FuncTool) Description() string { return f.ToolDescription }
func (f *FuncTool) InputSchema() Schema { return f.ToolSchema }

func (f *FuncTool) Invoke(ctx context.Context, args map[string]any) (map[string]any, error) {
	if f.Fn == nil {
		return nil, errors.New("agentkit: FuncTool has no Fn")
	}
	return f.Fn(ctx, args)
}
