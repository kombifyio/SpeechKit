package deepgram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// Receive translates the next server frame into a live.LiveMessage. Binary frames
// are agent audio; text frames are JSON control events. Events that don't map
// to live.LiveMessage fields are swallowed and the loop fetches the next frame.
func (p *Provider) Receive(ctx context.Context) (*live.LiveMessage, error) {
	conn := p.snapshotConn()
	if conn == nil {
		return nil, fmt.Errorf("deepgram agent: %w", live.ErrNotConnected)
	}
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			if websocket.CloseStatus(err) != -1 {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("deepgram agent: ws read: %w", err)
		}
		if typ == websocket.MessageBinary {
			if len(data) == 0 {
				continue
			}
			return live.NormalizeMessageEvents(&live.LiveMessage{
				EventType: live.LiveEventOutputAudio,
				Audio:     data,
			}, "binary_audio"), nil
		}
		msg, swallow, err := p.parseEvent(data)
		if err != nil {
			return nil, err
		}
		if swallow {
			continue
		}
		return msg, nil
	}
}

// parseEvent translates one Deepgram Voice Agent server event into the kernel's
// live.LiveMessage shape. Returns (nil, true, nil) when the event should be swallowed.
func (p *Provider) parseEvent(data []byte) (*live.LiveMessage, bool, error) {
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, false, fmt.Errorf("deepgram agent: decode event: %w", err)
	}
	switch env.Type {
	case "Welcome", "SettingsApplied", "AgentThinking", "AgentStartedSpeaking",
		"PromptUpdated", "SpeakUpdated", "ThinkUpdated", "History", "InjectionRefused":
		return nil, true, nil
	case "Warning":
		var ev struct {
			Description string `json:"description"`
		}
		_ = json.Unmarshal(data, &ev)
		slog.Warn("deepgram agent: server warning", "description", ev.Description)
		return nil, true, nil
	case "UserStartedSpeaking":
		// Barge-in: the kernel state machine uses this for interruption.
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventInterrupted, Interrupted: true}, env.Type), false, nil
	case "StartOfTurn", "TurnResumed":
		// Flux turn events signal fresh user activity. Surface them as
		// interruptions so host UIs can stop playback across both Deepgram Voice
		// Agent and lower-level Flux transports without provider-specific code.
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventInterrupted, Interrupted: true}, env.Type), false, nil
	case "EagerEndOfTurn", "EndOfTurn":
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventTurnEnd}, env.Type), false, nil
	case "ConversationText":
		var ev struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		_ = json.Unmarshal(data, &ev)
		if strings.EqualFold(ev.Role, "user") {
			return live.NormalizeMessageEvents(&live.LiveMessage{
				EventType:           live.LiveEventInputFinal,
				InputTranscript:     ev.Content,
				InputTranscriptDone: true,
			}, env.Type), false, nil
		}
		return live.NormalizeMessageEvents(&live.LiveMessage{
			EventType:            live.LiveEventOutputText,
			Text:                 ev.Content,
			OutputTranscript:     ev.Content,
			OutputTranscriptDone: true,
		}, env.Type), false, nil
	case "AgentAudioDone":
		// End of the agent's spoken turn.
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventTurnEnd, Done: true}, env.Type), false, nil
	case "FunctionCallRequest":
		var ev struct {
			FunctionCallID string          `json:"function_call_id"`
			FunctionName   string          `json:"function_name"`
			Input          json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			return nil, false, fmt.Errorf("deepgram agent: decode function call: %w", err)
		}
		args := map[string]any{}
		if len(ev.Input) > 0 {
			if err := json.Unmarshal(ev.Input, &args); err != nil {
				slog.Warn("deepgram agent: function call input not an object", "name", ev.FunctionName)
			}
		}
		return live.NormalizeMessageEvents(&live.LiveMessage{
			EventType: live.LiveEventToolCall,
			ToolCalls: []live.ToolCall{{
				ID:   ev.FunctionCallID,
				Name: ev.FunctionName,
				Args: args,
			}},
		}, env.Type), false, nil
	case "Error":
		var ev struct {
			Code        string `json:"code"`
			Description string `json:"description"`
			Message     string `json:"message"`
		}
		_ = json.Unmarshal(data, &ev)
		return nil, false, fmt.Errorf("deepgram agent: server error %s: %s", ev.Code, dgFirst(ev.Description, ev.Message, "unknown"))
	default:
		// Forward-compatible swallow for observability-only events.
		return nil, true, nil
	}
}
