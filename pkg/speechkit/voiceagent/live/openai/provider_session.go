package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// UpdateInstructions sends a fresh session.update with new instructions/tools.
// Implements live.LiveInstructionUpdater so the kernel can refresh persona prompts
// without forcing a reconnect.
func (p *Provider) UpdateInstructions(ctx context.Context, cfg live.LiveConfig) error {
	p.mu.Lock()
	if p.conn == nil {
		p.mu.Unlock()
		return fmt.Errorf("openai realtime: %w", live.ErrNotConnected)
	}
	cfgCopy := cfg
	if cfgCopy.Model == "" && p.lastConfig != nil {
		cfgCopy.Model = p.lastConfig.Model
	}
	p.lastConfig = &cfgCopy
	p.mu.Unlock()
	return p.sendSessionUpdate(ctx, cfgCopy)
}

// SendAudio resamples a 16 kHz mic chunk to 24 kHz and forwards it as a
// base64-encoded input_audio_buffer.append event. Empty chunks are no-ops.
func (p *Provider) SendAudio(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("openai realtime: %w", live.ErrNotConnected)
	}
	upsampled := live.UpsampleMicPCM16Mono(chunk)
	encoded := base64.StdEncoding.EncodeToString(upsampled)
	return p.sendJSON(conn, map[string]any{
		"type":  "input_audio_buffer.append",
		"audio": encoded,
	})
}

// SendAudioStreamEnd flushes the input audio buffer and triggers a model
// response. With server VAD enabled OpenAI commits the buffer and creates the
// response automatically at end-of-speech (turn_detection.create_response),
// so an explicit commit here races that automatic commit and, once the buffer
// has already been drained, fails the whole session with
// input_audio_buffer_commit_empty. That is exactly what a server-VAD client
// that also signals audio_end (e.g. kombify-Box toggle-talk) triggered. Only
// push-to-talk (server VAD disabled) needs the manual commit + response.create.
func (p *Provider) SendAudioStreamEnd() error {
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("openai realtime: %w", live.ErrNotConnected)
	}
	if p.serverVADEnabled() {
		return nil
	}
	if err := p.sendJSON(conn, map[string]any{"type": "input_audio_buffer.commit"}); err != nil {
		return err
	}
	return p.sendJSON(conn, map[string]any{"type": "response.create"})
}

// serverVADEnabled reports whether the active session runs OpenAI server-side
// VAD (turn_detection). It mirrors the turn-detection decision made in
// buildOpenAISession from the last connected config, so SendAudioStreamEnd can
// avoid an explicit commit that server VAD already performs.
func (p *Provider) serverVADEnabled() bool {
	p.mu.RLock()
	cfg := p.lastConfig
	p.mu.RUnlock()
	if cfg == nil {
		return false
	}
	resolved := live.ResolveLiveOptions("openai", "realtime.openai.gpt-realtime-2", *cfg, nil, nil)
	activity := ResolveActivityDetection(cfg.Policies.ActivityDetection, resolved)
	return buildOpenAITurnDetection(activity) != nil
}

// CancelResponse cancels the in-progress model response with the Realtime
// `response.cancel` client event (tap-to-interrupt). The server truncates
// generation and answers with `response.done`, which Receive maps to a turn
// end. A cancel that races an already-finished response yields the non-fatal
// `response_cancel_not_active` error event, which parseEvent swallows.
func (p *Provider) CancelResponse() error {
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("openai realtime: %w", live.ErrNotConnected)
	}
	return p.sendJSON(conn, map[string]any{"type": "response.cancel"})
}

// SendText injects a text-only user turn and triggers a response.
func (p *Provider) SendText(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("openai realtime: %w", live.ErrNotConnected)
	}
	if err := p.sendJSON(conn, map[string]any{
		"type": "conversation.item.create",
		"item": map[string]any{
			"type": "message",
			"role": "user",
			"content": []map[string]any{
				{"type": "input_text", "text": text},
			},
		},
	}); err != nil {
		return err
	}
	return p.sendJSON(conn, map[string]any{"type": "response.create"})
}

// SendToolResponse delivers the host-side tool result back to the model and
// triggers a follow-up response.
func (p *Provider) SendToolResponse(response live.ToolResponse) error {
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("openai realtime: %w", live.ErrNotConnected)
	}
	output := response.Response
	if output == nil {
		output = map[string]any{}
	}
	rawOutput, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("openai realtime: marshal tool response: %w", err)
	}
	item := map[string]any{
		"type":    "function_call_output",
		"call_id": response.ID,
		"output":  string(rawOutput),
	}
	if err := p.sendJSON(conn, map[string]any{
		"type": "conversation.item.create",
		"item": item,
	}); err != nil {
		return err
	}
	return p.sendJSON(conn, map[string]any{"type": "response.create"})
}

// Close terminates the WebSocket. Idempotent.
func (p *Provider) Close() error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	if p.closed {
		return p.closeErr
	}
	p.closed = true

	p.mu.Lock()
	conn := p.conn
	p.conn = nil
	p.mu.Unlock()
	if conn == nil {
		return nil
	}
	err := conn.Close(websocket.StatusNormalClosure, "client close")
	p.closeErr = err
	return err
}

// ── internal helpers ────────────────────────────────────────────────────────

func (p *Provider) sendJSON(conn *websocket.Conn, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("openai realtime: marshal frame: %w", err)
	}
	return conn.Write(context.Background(), websocket.MessageText, body)
}

func (p *Provider) snapshotConn() *websocket.Conn {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.conn
}

func (p *Provider) resetSessionReady() {
	p.sessionReadyMu.Lock()
	defer p.sessionReadyMu.Unlock()
	p.sessionReady = make(chan struct{})
}

func (p *Provider) markSessionReady() {
	p.sessionReadyMu.Lock()
	ch := p.sessionReady
	p.sessionReadyMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case <-ch:
		// already closed
	default:
		close(ch)
	}
}

// sendSessionUpdate writes the configured instructions/voice/tools/turn-detection
// to the live session. It does NOT block on the session.updated ack — that
// arrives asynchronously through Receive() and the kernel session loop is the
// canonical reader.
func (p *Provider) sendSessionUpdate(ctx context.Context, cfg live.LiveConfig) error {
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("openai realtime: %w", live.ErrNotConnected)
	}

	resolved := live.ResolveLiveOptions("openai", "realtime.openai.gpt-realtime-2", cfg, nil, nil)
	instructions := strings.TrimSpace(cfg.FrameworkPrompt)
	if refinement := strings.TrimSpace(cfg.RefinementPrompt); refinement != "" {
		if instructions == "" {
			instructions = refinement
		} else {
			instructions = instructions + "\n\n" + refinement
		}
	}
	instructions = live.AppendContextPrompt(instructions, resolved.ContextPrompt)

	session := p.buildSession(cfg, resolveOpenAIRealtimeModel(cfg.Model), instructions)

	frame := map[string]any{
		"type":    "session.update",
		"session": session,
	}

	// Use a bounded write context so a stuck server doesn't pin Connect forever.
	writeCtx, cancel := context.WithTimeout(ctx, openaiSessionUpdateTimeout)
	defer cancel()
	body, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("marshal session.update: %w", err)
	}
	if err := conn.Write(writeCtx, websocket.MessageText, body); err != nil {
		return fmt.Errorf("write session.update: %w", err)
	}
	return nil
}
