package assist

import (
	"context"

	"github.com/kombifyio/SpeechKit/internal/shortcuts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
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
	Utility UtilityDefinition
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
	Text       string
	SpeakText  string
	Action     string
	Locale     string
	Surface    ResultSurface
	Kind       ResultKind
	MessageID  localization.MessageID
	ReasonCode string

	// FollowupNeeded signals a multi-turn skill: the pipeline stores
	// the current Intent + FollowupState under the caller's session
	// key (see ProcessOpts.SessionKey) and the next transcript will
	// re-route to the same skill. When false, any prior follow-up
	// state for the same session is cleared. v0.38.0 (Phase 2).
	FollowupNeeded bool

	// FollowupState carries skill-private data across turns. The
	// pipeline echoes it back via ToolCall.Context on the next
	// invocation. Keep entries small — this is in-memory state, not
	// long-term persistence.
	FollowupState map[string]string
}

type ToolExecutor interface {
	Execute(context.Context, ToolCall) (ToolResult, error)
}
