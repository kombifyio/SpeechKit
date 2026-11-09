//go:build linux

package voiceagent

// Server tools are the adapter's own control tools: the front door's
// Companion hand-over (front_door_tool.go) and the Companion's conversation
// control (conversation.go). A session's model sees them next to its own
// tools; a call to one of them is executed here and answered through the
// provider's SendToolResponse. The client still receives the tool_call frame
// (tagged provider_metadata.execution="server") plus a tool_result_ack event,
// but is never asked to answer it. Unclaimed tool names keep the client
// pass-through wire contract.
//
// SpeechKit runs no other server-side tools: actions on a person's account
// and devices belong to the registered agent they talk to, which brings its
// own governed tools.

// ToolDefinitionFrame is the transport-neutral tool declaration the adapter
// merges into the live provider config. It mirrors the kernel's
// live.ToolDefinition without importing kernel types so test doubles stay
// dependency-free.
type ToolDefinitionFrame struct {
	Name                 string         `json:"name"`
	Description          string         `json:"description,omitempty"`
	ParametersJSONSchema map[string]any `json:"parameters,omitempty"`
	// Behavior is "blocking", "non_blocking", or "" (provider default).
	// Server tools are declared "blocking": Gemini 3.1 flash live rejects
	// non_blocking function declarations.
	Behavior  string `json:"behavior,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

// maxConcurrentServerToolCalls bounds per-session async server-side tool
// execution. Calls beyond the bound receive an immediate structured error
// tool response instead of queueing unboundedly.
const maxConcurrentServerToolCalls = 4
