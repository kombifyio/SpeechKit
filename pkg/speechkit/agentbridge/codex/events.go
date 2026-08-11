package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentbridge"
)

// execEvent mirrors the documented `codex exec --json` JSONL shapes loosely.
// Unknown fields and unknown event types are ignored on purpose — the Codex
// CLI adds event kinds ahead of adapter updates.
type execEvent struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Error    *execEventError `json:"error"`
	Message  string          `json:"message"`
	Item     map[string]any  `json:"item"`
}

type execEventError struct {
	Message string `json:"message"`
}

// parseExecEvent normalizes one JSONL line into a bridge event. The second
// return is false for blank lines, non-JSON noise, and unrecognized event
// types.
func parseExecEvent(line []byte) (agentbridge.Event, bool) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return agentbridge.Event{}, false
	}
	var ev execEvent
	if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
		return agentbridge.Event{}, false
	}
	switch ev.Type {
	case "thread.started":
		return agentbridge.Event{Type: agentbridge.EventThreadStarted, ThreadID: ev.ThreadID}, true
	case "turn.started":
		return agentbridge.Event{Type: agentbridge.EventTurnStarted, ThreadID: ev.ThreadID}, true
	case "turn.completed":
		return agentbridge.Event{Type: agentbridge.EventTurnCompleted, ThreadID: ev.ThreadID}, true
	case "turn.failed":
		msg := "turn failed"
		if ev.Error != nil && ev.Error.Message != "" {
			msg = ev.Error.Message
		}
		return agentbridge.Event{Type: agentbridge.EventError, ThreadID: ev.ThreadID, Err: msg}, true
	case "item.started":
		return agentbridge.Event{Type: agentbridge.EventItemStarted, ThreadID: ev.ThreadID, Item: normalizeItem(ev.Item)}, true
	case "item.completed":
		return agentbridge.Event{Type: agentbridge.EventItemCompleted, ThreadID: ev.ThreadID, Item: normalizeItem(ev.Item)}, true
	case "error":
		msg := ev.Message
		if msg == "" && ev.Error != nil {
			msg = ev.Error.Message
		}
		if msg == "" {
			msg = "unknown error"
		}
		return agentbridge.Event{Type: agentbridge.EventError, ThreadID: ev.ThreadID, Err: msg}, true
	default:
		return agentbridge.Event{}, false
	}
}

// normalizeItem compresses a Codex item payload into the seam's Item. Kind
// naming follows the Codex item vocabulary; the summary is a short
// human-readable line safe to narrate.
func normalizeItem(raw map[string]any) *agentbridge.Item {
	if raw == nil {
		return nil
	}
	kind := stringField(raw, "item_type")
	if kind == "" {
		kind = stringField(raw, "type")
	}
	item := &agentbridge.Item{Kind: kind}
	switch kind {
	case "command_execution":
		item.Summary = stringField(raw, "command")
		if code, ok := raw["exit_code"]; ok {
			item.Summary = fmt.Sprintf("%s (exit %v)", item.Summary, code)
		}
	case "file_change":
		if changes, ok := raw["changes"].([]any); ok && len(changes) > 0 {
			item.Summary = fmt.Sprintf("%d file change(s)", len(changes))
		} else {
			item.Summary = stringField(raw, "path")
		}
	case "agent_message", "reasoning":
		item.Summary = truncate(stringField(raw, "text"), 240)
	case "mcp_tool_call":
		item.Summary = strings.TrimSpace(stringField(raw, "server") + " " + stringField(raw, "tool"))
	case "web_search":
		item.Summary = stringField(raw, "query")
	default:
		item.Summary = truncate(stringField(raw, "text"), 240)
	}
	return item
}

func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func truncate(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
