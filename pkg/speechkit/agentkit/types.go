package agentkit

import "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"

// Re-exported core realtime types from the SpeechKit voice-agent runtime.
// Callers of agentkit do not need to import any internal package directly;
// these aliases point at the public type contract in
// pkg/speechkit/voiceagent/live and are interchangeable with it.
type (
	// ToolDefinition describes a host-side tool to the realtime model.
	// Built from a Tool by ToolRegistry.Definitions.
	ToolDefinition = live.ToolDefinition

	// ToolCall is emitted by the model when it wants to invoke a host-side
	// tool. The agentkit ToolRegistry resolves Name to a registered Tool
	// and dispatches Args to its Invoke method.
	ToolCall = live.ToolCall

	// ToolResponse carries the result of a host-side tool invocation back
	// to the model.
	ToolResponse = live.ToolResponse

	// LiveConfig configures a realtime audio session (model, voice,
	// prompts, locale, tools, policies).
	LiveConfig = live.LiveConfig

	// IdleConfig configures the per-session idle reminder + auto-deactivate
	// timer.
	IdleConfig = live.IdleConfig

	// Callbacks are the realtime session event handlers (audio out, text,
	// transcripts, tool calls, errors, session end).
	Callbacks = live.Callbacks

	// LiveProvider is the WebSocket-backed realtime model adapter (Gemini
	// Live, OpenAI Realtime, ...). Pass it to NewAgentSession.
	LiveProvider = live.LiveProvider

	// ToolBehavior controls whether the model waits for the tool result.
	ToolBehavior = live.ToolBehavior
)
