package agentkit

import "github.com/kombifyio/SpeechKit/internal/voiceagent"

// Re-exported core realtime types from the SpeechKit voice-agent runtime.
// Callers of agentkit do not need to import the internal package directly;
// these aliases are the same underlying types and are interchangeable.
type (
	// ToolDefinition describes a host-side tool to the realtime model.
	// Built from a Tool by ToolRegistry.Definitions.
	ToolDefinition = voiceagent.ToolDefinition

	// ToolCall is emitted by the model when it wants to invoke a host-side
	// tool. The agentkit ToolRegistry resolves Name to a registered Tool
	// and dispatches Args to its Invoke method.
	ToolCall = voiceagent.ToolCall

	// ToolResponse carries the result of a host-side tool invocation back
	// to the model.
	ToolResponse = voiceagent.ToolResponse

	// LiveConfig configures a realtime audio session (model, voice,
	// prompts, locale, tools, policies).
	LiveConfig = voiceagent.LiveConfig

	// IdleConfig configures the per-session idle reminder + auto-deactivate
	// timer.
	IdleConfig = voiceagent.IdleConfig

	// Callbacks are the realtime session event handlers (audio out, text,
	// transcripts, tool calls, errors, session end).
	Callbacks = voiceagent.Callbacks

	// LiveProvider is the WebSocket-backed realtime model adapter (Gemini
	// Live, OpenAI Realtime, ...). Pass it to NewAgentSession.
	LiveProvider = voiceagent.LiveProvider

	// ToolBehavior controls whether the model waits for the tool result.
	ToolBehavior = voiceagent.ToolBehavior
)
