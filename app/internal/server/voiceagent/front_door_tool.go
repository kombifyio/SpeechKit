//go:build linux

package voiceagent

import (
	"context"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// handoffToolName is the model-facing control tool of the front door. The
// model calls it when the person asks for something it cannot do itself and
// the rules did not catch ("restart my NAS", "send the invoice"). It costs the
// tool-call round trip of the model's own turn and nothing else; the
// receipt it produces is the same route_switch the spoken rules produce.
const handoffToolName = "kombify_handoff_companion"

func handoffToolDefinition() ToolDefinitionFrame {
	return ToolDefinitionFrame{
		Name: handoffToolName,
		Description: "Hand the conversation to the Companion app. Use it when the user asks for something you cannot do yourself: " +
			"an action in their account or homelab (create, change, deploy, restart, delete, send, schedule), delegating work to other agents, " +
			"or anything that needs their private data, memory or tools that you do not have. " +
			"Say one short sentence that you are handing over, then call this tool with the request in the user's own words. " +
			"Never use it for questions you can answer yourself or with your other tools.",
		ParametersJSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"request": map[string]any{
					"type":        "string",
					"description": "What the user wants the Companion to do, in their own words.",
				},
			},
			"required":             []string{"request"},
			"additionalProperties": false,
		},
		Behavior:  string(live.ToolBehaviorBlocking),
		TimeoutMs: 3000,
	}
}

// withFrontDoorTools appends the handoff tool for a session that opted into
// route_switch. It never replaces or shadows a bridge tool of the same name.
func (a *Adapter) withFrontDoorTools(defs []ToolDefinitionFrame) []ToolDefinitionFrame {
	if a.front == nil || !a.front.routes[routeAgent] {
		return defs
	}
	for _, def := range defs {
		if strings.TrimSpace(def.Name) == handoffToolName {
			return defs
		}
	}
	a.front.mu.Lock()
	a.front.localHandoff = true
	a.front.mu.Unlock()
	return append(defs, handoffToolDefinition())
}

// toolsExcludingHandoff counts the tools of a session without the front
// door's own control tool, so the prompt and the unavailable event reflect
// the bridge tools only.
func toolsExcludingHandoff(tools []ToolDefinitionFrame) int {
	n := 0
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) != handoffToolName {
			n++
		}
	}
	return n
}

func (f *frontDoor) isLocalHandoff() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.localHandoff
}

// executeBridgeTool runs a claimed tool call: the front-door handoff locally,
// everything else through the session tool router.
func (a *Adapter) executeBridgeTool(ctx context.Context, call ToolCall) (map[string]any, bool) {
	if call.Name == handoffToolName && a.front != nil && a.front.isLocalHandoff() {
		request := ""
		if value, ok := call.Args["request"].(string); ok {
			request = strings.TrimSpace(value)
		}
		if request == "" {
			return map[string]any{"error": map[string]any{
				"code":    "handoff_request_missing",
				"message": "The request for the Companion must not be empty.",
			}}, true
		}
		if !a.sendRouteSwitch(ctx, a.front, "model_handoff", request) {
			return map[string]any{"error": map[string]any{
				"code":    "handoff_unavailable",
				"message": "The hand-over to the Companion did not go out; answer the request yourself as far as you can.",
			}}, true
		}
		return map[string]any{"status": "handed_over", "message": "The Companion takes over now. Do not say anything more."}, true
	}
	return a.ToolRouter.Execute(ctx, a.Session, call)
}
