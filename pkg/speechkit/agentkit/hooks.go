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
// Each hook is invoked synchronously from the dispatch goroutine; long
// work should be moved off-thread by the caller. Any hook may be nil.
//
// OnUserMessage and OnAgentMessage are called only when the underlying
// transcript event signals done=true. The accumulated text since the last
// done is delivered as the message.
type LifecycleHooks struct {
	OnSessionStart func(ctx context.Context, sc SessionContext)
	OnUserMessage  func(ctx context.Context, sc SessionContext, text string)
	OnToolCall     func(ctx context.Context, sc SessionContext, call ToolCall)
	OnAgentMessage func(ctx context.Context, sc SessionContext, text string)
	OnSessionEnd   func(ctx context.Context, sc SessionContext)
}
