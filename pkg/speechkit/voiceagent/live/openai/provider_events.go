package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// Receive translates the next server event into a live.LiveMessage. Server
// events that don't map to live.LiveMessage fields (session.created/updated,
// rate-limit telemetry, etc.) are swallowed and the loop fetches the next
// frame so callers see an aligned event stream.
func (p *Provider) Receive(ctx context.Context) (*live.LiveMessage, error) {
	conn := p.snapshotConn()
	if conn == nil {
		return nil, fmt.Errorf("openai realtime: %w", live.ErrNotConnected)
	}
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return nil, fmt.Errorf("openai realtime: ws read: %w", err)
		}
		if typ != websocket.MessageText {
			// OpenAI Realtime is JSON-only; binary frames are unexpected.
			continue
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

// parseEvent translates one OpenAI Realtime server event into the kernel's
// live.LiveMessage shape. Returns (nil, true, nil) when the event should be
// swallowed (status events, rate-limit telemetry, session lifecycle).
func (p *Provider) parseEvent(data []byte) (*live.LiveMessage, bool, error) {
	var env struct {
		Type    string `json:"type"`
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, false, fmt.Errorf("openai realtime: decode event: %w", err)
	}
	switch env.Type {
	case "session.created":
		// Server has accepted the connection. We sent session.update from
		// Connect(); the canonical "ready" trigger is session.updated below.
		return nil, true, nil
	case "session.updated":
		p.markSessionReady()
		return nil, true, nil
	case "input_audio_buffer.speech_started":
		// User started speaking — used by the kernel state machine for
		// barge-in / interruption detection.
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventInterrupted, Interrupted: true}, env.Type), false, nil
	case "input_audio_buffer.speech_stopped":
		return nil, true, nil
	case "conversation.item.input_audio_transcription.delta":
		var ev struct {
			Delta string `json:"delta"`
		}
		_ = json.Unmarshal(data, &ev)
		return live.NormalizeMessageEvents(&live.LiveMessage{
			EventType:       live.LiveEventInputPartial,
			InputTranscript: ev.Delta,
		}, env.Type), false, nil
	case "conversation.item.input_audio_transcription.completed":
		var ev struct {
			Transcript string `json:"transcript"`
		}
		_ = json.Unmarshal(data, &ev)
		return live.NormalizeMessageEvents(&live.LiveMessage{
			EventType:           live.LiveEventInputFinal,
			InputTranscript:     ev.Transcript,
			InputTranscriptDone: true,
		}, env.Type), false, nil
	case "response.audio.delta", "response.output_audio.delta":
		var ev struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			return nil, false, fmt.Errorf("openai realtime: decode audio.delta: %w", err)
		}
		audio, err := base64.StdEncoding.DecodeString(ev.Delta)
		if err != nil {
			return nil, false, fmt.Errorf("openai realtime: decode audio bytes: %w", err)
		}
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventOutputAudio, Audio: audio}, env.Type), false, nil
	case "response.audio.done", "response.output_audio.done":
		// End-of-audio for this response — the server still owes us a
		// response.done, which is where we signal turn completion to the
		// kernel. Swallow audio.done to avoid double-fire.
		return nil, true, nil
	case "response.audio_transcript.delta", "response.output_audio_transcript.delta":
		var ev struct {
			Delta string `json:"delta"`
		}
		_ = json.Unmarshal(data, &ev)
		return live.NormalizeMessageEvents(&live.LiveMessage{
			EventType:        live.LiveEventOutputText,
			OutputTranscript: ev.Delta,
		}, env.Type), false, nil
	case "response.audio_transcript.done", "response.output_audio_transcript.done":
		var ev struct {
			Transcript string `json:"transcript"`
		}
		_ = json.Unmarshal(data, &ev)
		return live.NormalizeMessageEvents(&live.LiveMessage{
			EventType:            live.LiveEventOutputText,
			OutputTranscript:     ev.Transcript,
			OutputTranscriptDone: true,
		}, env.Type), false, nil
	case "response.text.delta", "response.output_text.delta":
		var ev struct {
			Delta string `json:"delta"`
		}
		_ = json.Unmarshal(data, &ev)
		return live.NormalizeMessageEvents(&live.LiveMessage{
			EventType:        live.LiveEventOutputText,
			Text:             ev.Delta,
			OutputTranscript: ev.Delta,
		}, env.Type), false, nil
	case "response.output_text.done":
		var ev struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(data, &ev)
		return live.NormalizeMessageEvents(&live.LiveMessage{
			EventType:            live.LiveEventOutputText,
			Text:                 ev.Text,
			OutputTranscript:     ev.Text,
			OutputTranscriptDone: true,
		}, env.Type), false, nil
	case "response.function_call_arguments.done":
		var ev struct {
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			return nil, false, fmt.Errorf("openai realtime: decode function_call.done: %w", err)
		}
		args := map[string]any{}
		if strings.TrimSpace(ev.Arguments) != "" {
			if err := json.Unmarshal([]byte(ev.Arguments), &args); err != nil {
				slog.Warn("openai realtime: function call arguments not valid JSON", "name", ev.Name, "raw", ev.Arguments)
			}
		}
		return live.NormalizeMessageEvents(&live.LiveMessage{
			EventType: live.LiveEventToolCall,
			ToolCalls: []live.ToolCall{{
				ID:   ev.CallID,
				Name: ev.Name,
				Args: args,
			}},
		}, env.Type), false, nil
	case "response.done":
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventTurnEnd, Done: true}, env.Type), false, nil
	case "error":
		var ev struct {
			Error struct {
				Type    string `json:"type"`
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &ev)
		if ev.Error.Code == "response_cancel_not_active" {
			// A `response.cancel` raced a response that had already finished.
			// OpenAI keeps the session fully usable after this event, so
			// treating it as fatal would tear down the whole voice session
			// for a harmless no-op cancel. Swallow it.
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("openai realtime: server error %s/%s: %s",
			ev.Error.Type, ev.Error.Code, ev.Error.Message)
	default:
		// Many events (rate_limits.updated, response.output_item.added, etc.)
		// are observability-only. Forward-compatible swallow.
		return nil, true, nil
	}
}
