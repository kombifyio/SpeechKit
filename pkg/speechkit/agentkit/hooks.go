package agentkit

import "context"

// SessionContext is passed to every lifecycle hook. It exposes the active
// session id, the bound tool registry, and the session memory store.
type SessionContext struct {
	SessionID string
	Memory    Memory
	Registry  *ToolRegistry
}

// LifecycleHooks are optional callbacks fired during the agent session.
// AuthorizeToolCall runs synchronously in the receive callback before a
// dispatch goroutine exists. The remaining hooks run synchronously from their
// respective session or dispatch goroutine. Long work should be moved
// off-thread by the caller. Any hook may be nil.
//
// OnUserMessage and OnAgentMessage are called only when the underlying
// transcript event signals done=true. The accumulated text since the last
// done is delivered as the message.
type LifecycleHooks struct {
	OnSessionStart func(ctx context.Context, sc SessionContext)
	OnUserMessage  func(ctx context.Context, sc SessionContext, text string)
	// AuthorizeToolCall is the synchronous host-policy boundary immediately
	// before a known tool is invoked. It must return the exact argument map the
	// tool is allowed to receive. Returning an error denies the call and the
	// tool is never invoked. OnToolCall fires only after authorization succeeds.
	//
	// This hook must be fast. It runs in the provider receive callback before
	// any tool dispatch goroutine exists and is intended for local policy
	// checks, turn binding, and argument narrowing, not network calls.
	AuthorizeToolCall func(ctx context.Context, sc SessionContext, call ToolCall) (map[string]any, error)
	OnToolCall        func(ctx context.Context, sc SessionContext, call ToolCall)
	// OnToolResult observes the normalized result of a known tool after its
	// invocation completes and before the result is sent to the live provider.
	// The original call ID is preserved so hosts can bind an authority receipt
	// to the exact turn that requested it.
	OnToolResult   func(ctx context.Context, sc SessionContext, call ToolCall, response ToolResponse)
	OnAgentMessage func(ctx context.Context, sc SessionContext, text string)
	OnSessionEnd   func(ctx context.Context, sc SessionContext)
}
