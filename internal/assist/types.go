package assist

import (
	"context"

	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

type Route string

const (
	RouteDirectReply Route = "direct_reply"
	RouteToolIntent  Route = "tool_intent"
	RouteClarify     Route = "clarify"
)

type Decision struct {
	Route   Route
	Intent  shortcuts.Intent
	Payload string
	Locale  string
}

type ToolCall struct {
	Intent     shortcuts.Intent
	Payload    string
	Transcript string
	Locale     string
	Selection  string
	Context    string
}

type ToolResult struct {
	Text      string
	SpeakText string
	Action    string
	Locale    string
}

type ToolExecutor interface {
	Execute(context.Context, ToolCall) (ToolResult, error)
}
