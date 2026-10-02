//go:build linux

package voiceagent

import (
	"context"
	"log/slog"
	"runtime/debug"
	"strings"
)

// mergeBridgeTools asks the session tool router for definitions (bounded by
// bridgeDefinitionsTimeout) and merges them into cfg.Tools. Failure or an
// empty manifest degrades to tool-less and surfaces an informational event
// frame; the session always proceeds.
func (a *Adapter) mergeBridgeTools(ctx context.Context, cfg *LiveConfigFrame) {
	if a.ToolRouter == nil {
		return
	}
	defsCtx, cancel := context.WithTimeout(ctx, bridgeDefinitionsTimeout)
	defer cancel()
	defs := a.ToolRouter.Definitions(defsCtx, a.Session, *cfg)
	if len(defs) == 0 {
		fields := a.eventFrameFields(nil, EventToolBridgeUnavailable)
		fields.ProviderMetadata = map[string]any{"reason": "no_tool_definitions"}
		a.sendJSON(ctx, EventFrame{Type: MsgEvent, EventFrameFields: fields})
		return
	}
	a.bridgeTools = make(map[string]ToolDefinitionFrame, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		def.Name = name
		a.bridgeTools[name] = def
		cfg.Tools = append(cfg.Tools, def)
	}
}

// dispatchBridgeToolCall handles one provider tool call claimed by the tool
// router: emit the transparency tool_call frame, execute asynchronously under
// the per-session concurrency bound, feed the result back to the provider via
// SendToolResponse, and acknowledge to the client with a tool_result_ack
// event frame.
func (a *Adapter) dispatchBridgeToolCall(ctx context.Context, msg *LiveMessage, call ToolCall) {
	fields := a.eventFrameFields(msg, EventToolCall)
	metadata := make(map[string]any, len(fields.ProviderMetadata)+1)
	for key, value := range fields.ProviderMetadata {
		metadata[key] = value
	}
	metadata["execution"] = "server"
	fields.ProviderMetadata = metadata
	a.sendJSON(ctx, ToolCallFrame{
		Type:             MsgToolCall,
		EventFrameFields: fields,
		ID:               call.ID,
		Name:             call.Name,
		Args:             call.Args,
	})

	select {
	case a.toolSem <- struct{}{}:
	default:
		// Concurrency bound reached: answer immediately with a structured
		// error so the model recovers verbally instead of stalling the turn.
		a.sendBridgeToolResponse(ctx, call, map[string]any{
			"error": map[string]any{
				"code":    "tool_concurrency_exceeded",
				"message": "too many concurrent server-side tool calls in this session; try again",
			},
		}, "error")
		return
	}
	a.toolWG.Add(1)
	go func() {
		defer a.toolWG.Done()
		defer func() { <-a.toolSem }()
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("voiceagent: bridge tool execution panic recovered",
					"session_id", a.Session.ID,
					"tool", call.Name,
					"err", rec,
					"stack", string(debug.Stack()),
				)
			}
		}()
		response, handled := a.ToolRouter.Execute(ctx, a.Session, call)
		status := "ok"
		if !handled || response == nil {
			status = "error"
			response = map[string]any{
				"error": map[string]any{
					"code":    "tool_bridge_unavailable",
					"message": "server-side tool execution failed",
				},
			}
		} else if _, hasError := response["error"]; hasError {
			status = "error"
		}
		a.sendBridgeToolResponse(ctx, call, response, status)
	}()
}

// sendBridgeToolResponse feeds a server-side tool result back to the provider
// and emits the client-facing tool_result_ack event frame carrying id/status.
func (a *Adapter) sendBridgeToolResponse(ctx context.Context, call ToolCall, response map[string]any, status string) {
	responder, ok := a.Provider.(LiveToolResponder)
	if !ok {
		slog.Warn("voiceagent: provider does not accept tool responses; dropping bridge result",
			"session_id", a.Session.ID, "tool", call.Name)
		status = "error"
	} else if err := responder.SendToolResponse(ToolResponseFrame{
		Type:     MsgToolResponse,
		ID:       call.ID,
		Name:     call.Name,
		Response: response,
	}); err != nil {
		slog.Warn("voiceagent: bridge tool response upstream failed",
			"session_id", a.Session.ID, "tool", call.Name, "err", err)
		status = "error"
	}
	fields := a.eventFrameFields(nil, EventToolResultAck)
	fields.ProviderMetadata = map[string]any{
		"id":        call.ID,
		"name":      call.Name,
		"status":    status,
		"execution": "server",
	}
	a.sendJSON(ctx, EventFrame{Type: MsgEvent, EventFrameFields: fields})
}
