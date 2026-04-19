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

type ResultSurface string

const (
	ResultSurfacePanel     ResultSurface = "panel"
	ResultSurfaceActionAck ResultSurface = "action_ack"
	ResultSurfaceSilent    ResultSurface = "silent"
)

type ResultKind string

const (
	ResultKindAnswer        ResultKind = "answer"
	ResultKindWorkProduct   ResultKind = "work_product"
	ResultKindUtilityAction ResultKind = "utility_action"
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
	Target     any
}

type ToolResult struct {
	Text      string
	SpeakText string
	Action    string
	Locale    string
	Surface   ResultSurface
	Kind      ResultKind
}

type ToolExecutor interface {
	Execute(context.Context, ToolCall) (ToolResult, error)
}
