package openailive

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// SendAudio upsamples a 16 kHz kernel mic chunk to the session's 24 kHz and
// sends it base64-encoded in session.input_audio.append
// (types/live/input_audio_append_event.py). Appends are not acknowledged.
func (p *Provider) SendAudio(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	conn, err := p.connected()
	if err != nil {
		return err
	}
	return writeJSON(context.Background(), conn, map[string]any{
		"type":  "session.input_audio.append",
		"audio": base64.StdEncoding.EncodeToString(live.UpsampleMicPCM16Mono(chunk)),
	})
}

// SendAudioStreamEnd is a no-op: GPT-Live is full duplex and has no buffer
// commit or turn-end client event (types/live/client_event.py). The model
// decides when to speak from the continuous audio stream.
func (p *Provider) SendAudioStreamEnd() error {
	_, err := p.connected()
	return err
}

// SendText injects trusted host text (idle reminders, agent progress) with
// session.instructions.append and a null delegation_id, the documented
// channel for application instructions that change behavior or speech
// (types/live/instructions_append_event.py, how-to/gpt-live.md).
func (p *Provider) SendText(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	conn, err := p.connected()
	if err != nil {
		return err
	}
	return writeJSON(context.Background(), conn, map[string]any{
		"type":          "session.instructions.append",
		"content":       text,
		"delegation_id": nil,
	})
}

// SendToolResponse submits a function result to the Responses backend as a
// function_call_output item (response.item.create) and, once every pending
// call of the same delegation has a result, continues the backend with
// response.create (how-to/gpt-live-delegation.md "Complete a
// client-actionable function call"). A result for a call this session did not
// see is continued immediately.
func (p *Provider) SendToolResponse(response live.ToolResponse) error {
	conn, err := p.connected()
	if err != nil {
		return err
	}
	output := response.Response
	if output == nil {
		output = map[string]any{}
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("openai live: marshal tool response: %w", err)
	}
	if err := writeJSON(context.Background(), conn, map[string]any{
		"type": "response.item.create",
		"item": map[string]any{
			"type":    "function_call_output",
			"call_id": response.ID,
			"output":  string(raw),
		},
	}); err != nil {
		return err
	}
	if !p.resolveToolCall(response.ID) {
		return nil
	}
	return writeJSON(context.Background(), conn, map[string]any{"type": "response.create"})
}

// resolveToolCall marks callID answered and reports whether its delegation
// has no other pending call.
func (p *Provider) resolveToolCall(callID string) bool {
	p.toolMu.Lock()
	defer p.toolMu.Unlock()
	delegation, ok := p.pending[callID]
	if !ok {
		return true
	}
	delete(p.pending, callID)
	for _, other := range p.pending {
		if other == delegation {
			return false
		}
	}
	return true
}

func (p *Provider) trackToolCall(callID, delegation string) {
	p.toolMu.Lock()
	defer p.toolMu.Unlock()
	if p.pending == nil {
		p.pending = map[string]string{}
	}
	p.pending[callID] = delegation
}

// Close asks the server to finalize the session (session.close) and closes
// the socket without waiting for session.closed. Idempotent.
func (p *Provider) Close() error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	p.mu.Lock()
	conn := p.conn
	p.conn = nil
	p.mu.Unlock()
	if conn == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), closeWriteTimeout)
	defer cancel()
	_ = writeJSON(ctx, conn, map[string]any{"type": "session.close"})
	return conn.Close(websocket.StatusNormalClosure, "client close")
}

// Receive translates the next server event into a live.LiveMessage and skips
// events the kernel has no field for (acknowledgements, usage, info).
func (p *Provider) Receive(ctx context.Context) (*live.LiveMessage, error) {
	conn, err := p.connected()
	if err != nil {
		return nil, err
	}
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return nil, fmt.Errorf("openai live: ws read: %w", err)
		}
		if typ != websocket.MessageText {
			continue // Live WebSocket events are JSON text (resources/live/live.py: recv).
		}
		msg, err := p.parseEvent(data)
		if err != nil {
			return nil, err
		}
		if msg != nil {
			return msg, nil
		}
	}
}

// parseEvent maps one server event (types/live/server_event.py). It returns
// nil for events that carry nothing for the kernel.
func (p *Provider) parseEvent(data []byte) (*live.LiveMessage, error) {
	var ev struct {
		Type         string          `json:"type"`
		Delta        string          `json:"delta"`
		StartMs      int64           `json:"start_ms"`
		EndMs        int64           `json:"end_ms"`
		Reason       string          `json:"reason"`
		DelegationID string          `json:"delegation_id"`
		Event        json.RawMessage `json:"event"`
	}
	if err := decode(data, &ev); err != nil {
		return nil, err
	}
	switch ev.Type {
	case "session.output_audio.delta":
		// Base64 raw audio in the configured 24 kHz PCM16 format
		// (types/live/output_audio_delta_event.py).
		audio, err := base64.StdEncoding.DecodeString(ev.Delta)
		if err != nil {
			return nil, fmt.Errorf("openai live: decode output audio: %w", err)
		}
		return normalize(&live.LiveMessage{EventType: live.LiveEventOutputAudio, Audio: audio}, ev.Type), nil
	case "session.input_transcript.delta":
		// Fragments follow audio cadence; the API has no transcript-done or
		// turn-completed event (types/live/input_transcript_delta_event.py).
		if ev.Delta == "" {
			return nil, nil
		}
		msg := &live.LiveMessage{EventType: live.LiveEventInputPartial, InputTranscript: ev.Delta}
		if p.userSpokeOverOutput(ev.StartMs) {
			// The API has no barge-in event, so the session timeline stands
			// in: user speech that starts before the assistant's audio ends
			// is a barge-in, and local playback must flush like it does for
			// the other providers' interruption events.
			msg.Interrupted = true
		}
		return normalize(msg, ev.Type), nil
	case "session.output_transcript.delta":
		if ev.Delta == "" {
			return nil, nil
		}
		p.noteOutputSpan(ev.StartMs, ev.EndMs)
		return normalize(&live.LiveMessage{EventType: live.LiveEventOutputText, OutputTranscript: ev.Delta}, ev.Type), nil
	case "response.event":
		return p.parseResponseEvent(ev.DelegationID, ev.Event)
	case "session.closed":
		// Terminal event with the close reason
		// (types/live/session_closed_event.py).
		msg := normalize(&live.LiveMessage{EventType: live.LiveEventSessionEnd, GoAway: true}, ev.Type)
		msg.ProviderMetadata["reason"] = ev.Reason
		return msg, nil
	case "error":
		return nil, serverError(data)
	default:
		// session.started/updated, *.appended, input_audio.muted/unmuted,
		// session.delegation.created, session.usage.updated, info.
		return nil, nil
	}
}

// parseResponseEvent reads the nested Responses stream event of a
// response.event envelope (types/live/response_event.py). A finished
// function_call output item becomes a kernel tool call
// (how-to/gpt-live-delegation.md); delegated text is injected into the live
// conversation server-side and reaches the kernel as transcript and audio.
func (p *Provider) parseResponseEvent(delegationID string, raw json.RawMessage) (*live.LiveMessage, error) {
	var nested struct {
		Type string `json:"type"`
		Item struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"item"`
	}
	if len(raw) == 0 {
		return nil, nil
	}
	if err := decode(raw, &nested); err != nil {
		return nil, err
	}
	if nested.Type != "response.output_item.done" || nested.Item.Type != "function_call" {
		return nil, nil
	}
	args := map[string]any{}
	if strings.TrimSpace(nested.Item.Arguments) != "" {
		if err := json.Unmarshal([]byte(nested.Item.Arguments), &args); err != nil {
			p.log().Warn("openai live: function call arguments are not valid JSON", "name", nested.Item.Name, "raw_len", len(nested.Item.Arguments))
		}
	}
	p.trackToolCall(nested.Item.CallID, delegationID)
	msg := normalize(&live.LiveMessage{
		EventType: live.LiveEventToolCall,
		ToolCalls: []live.ToolCall{{ID: nested.Item.CallID, Name: nested.Item.Name, Args: args}},
	}, "response.event")
	msg.ProviderMetadata["nested_event"] = nested.Type
	msg.ProviderMetadata["delegation_id"] = delegationID
	return msg, nil
}

func normalize(msg *live.LiveMessage, providerEvent string) *live.LiveMessage {
	return live.NormalizeMessageEvents(msg, providerEvent)
}

// serverError renders the Live error envelope (types/live/error.py).
func serverError(data []byte) error {
	var ev struct {
		Error struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &ev)
	return fmt.Errorf("openai live: server error %s/%s: %s", ev.Error.Type, ev.Error.Code, ev.Error.Message)
}

func (p *Provider) connected() (*websocket.Conn, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.conn == nil {
		return nil, fmt.Errorf("openai live: %w", live.ErrNotConnected)
	}
	return p.conn, nil
}

func writeJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("openai live: marshal frame: %w", err)
	}
	return conn.Write(ctx, websocket.MessageText, body)
}

func decode(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("openai live: decode event: %w", err)
	}
	return nil
}

// noteOutputSpan records how far the assistant's audio reaches on the session
// timeline (output transcript fragments carry start_ms/end_ms, output audio
// on the primary WebSocket does not). Output that starts after a barge-in
// opens a new run that can be interrupted again.
func (p *Provider) noteOutputSpan(startMs, endMs int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bargedIn && startMs >= p.bargeInAtMs {
		p.bargedIn = false
	}
	if endMs > p.outputEndMs {
		p.outputEndMs = endMs
	}
}

// userSpokeOverOutput reports the first user speech that starts before the
// current assistant output run ends on the session timeline.
func (p *Provider) userSpokeOverOutput(startMs int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bargedIn || p.outputEndMs == 0 || startMs >= p.outputEndMs {
		return false
	}
	p.bargedIn, p.bargeInAtMs = true, startMs
	return true
}
