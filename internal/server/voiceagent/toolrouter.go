//go:build linux

package voiceagent

import (
	"context"
	"time"
)

// SessionToolRouter supplies server-executed tools for a Voice Agent session.
//
// The adapter calls Definitions once per session (after persona resolution,
// before the provider connect) and merges the returned frames into the live
// provider config. Every tool name returned by Definitions is "claimed": when
// the provider emits a tool call for a claimed name, the adapter executes it
// server-side via Execute and feeds the result back through the provider's
// SendToolResponse — the client still sees the tool_call frame (tagged
// provider_metadata.execution="server") plus a tool_result_ack event, but is
// never asked to answer it. Unclaimed tool names keep the existing client
// pass-through wire contract unchanged.
//
// Implementations must be safe for concurrent use across sessions and must
// fail closed: a nil/empty Definitions result means the session runs
// tool-less; an Execute failure returns a model-facing error payload rather
// than blocking the conversation.
type SessionToolRouter interface {
	// Definitions returns the tool frames to advertise for this session.
	// Returning nil (e.g. missing bridge credential, manifest fetch failure,
	// timeout) degrades the session to tool-less — never an error to the user.
	Definitions(ctx context.Context, session *ManagedSession, cfg LiveConfigFrame) []ToolDefinitionFrame
	// Execute runs one claimed tool call and returns the tool response
	// payload for the provider. ok=false means the router could not produce
	// any model-facing payload; the adapter then substitutes a structured
	// error response so the model can recover verbally.
	Execute(ctx context.Context, session *ManagedSession, call ToolCall) (map[string]any, bool)
}

// ToolDefinitionFrame is the transport-neutral tool declaration the router
// hands the adapter. It mirrors the kernel's live.ToolDefinition without
// importing kernel types so test doubles stay dependency-free.
type ToolDefinitionFrame struct {
	Name                 string         `json:"name"`
	Description          string         `json:"description,omitempty"`
	ParametersJSONSchema map[string]any `json:"parameters,omitempty"`
	// Behavior is "blocking", "non_blocking", or "" (provider default).
	// Bridge-sourced tools are always declared "blocking": Gemini 3.1 flash
	// live rejects non_blocking function declarations.
	Behavior  string `json:"behavior,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

// EventToolBridgeUnavailable is emitted as an event frame when a configured
// tool router yields no definitions (bridge unreachable, manifest empty, or
// no session credential); the session proceeds tool-less.
const EventToolBridgeUnavailable = "tool_bridge_unavailable"

const (
	// bridgeDefinitionsTimeout bounds the pre-connect manifest merge so a
	// slow bridge cannot delay session start; on expiry the session simply
	// starts tool-less.
	bridgeDefinitionsTimeout = 2 * time.Second
	// maxConcurrentBridgeToolCalls bounds per-session async server-side tool
	// execution. Calls beyond the bound receive an immediate structured error
	// tool response instead of queueing unboundedly.
	maxConcurrentBridgeToolCalls = 4
)
