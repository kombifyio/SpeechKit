package deepgram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// Connect dials the Deepgram Voice Agent WebSocket and sends the initial
// Settings message describing listen/think/speak and the audio formats. The
// SettingsApplied acknowledgement is consumed asynchronously by Receive().
func (p *Provider) Connect(ctx context.Context, cfg live.LiveConfig) error {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return fmt.Errorf("deepgram agent: %w", live.ErrMissingAPIKey)
	}

	header := http.Header{}
	header.Set("Authorization", "Token "+cfg.APIKey)
	conn, dialResp, err := websocket.Dial(ctx, deepgramAgentURL, &websocket.DialOptions{HTTPHeader: header})
	if dialResp != nil && dialResp.Body != nil {
		_ = dialResp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("deepgram agent: dial: %w", err)
	}
	conn.SetReadLimit(deepgramAgentReadLimit)

	cfgCopy := cfg
	// closed/closeErr belong to closeMu — Close guards them with it. Take
	// closeMu first and mu inside it, the same order Close uses; reversing the
	// two would deadlock against a concurrent Close, and resetting them under
	// mu alone (as this did) races a reconnect against a teardown.
	p.closeMu.Lock()
	p.mu.Lock()
	p.conn = conn
	p.lastConfig = &cfgCopy
	p.mu.Unlock()
	p.closed = false
	p.closeErr = nil
	p.closeMu.Unlock()

	if err := p.sendSettings(ctx, cfgCopy); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "settings failed")
		return fmt.Errorf("deepgram agent: settings: %w", err)
	}
	// The keepalive loop is detached: it runs for the whole session (until
	// Close), so it deliberately does not inherit Connect's request context.
	p.startKeepAlive() //nolint:contextcheck // detached session-lifetime keepalive; must outlive Connect's ctx
	return nil
}

// SendAudio forwards a 16 kHz PCM16 mic chunk as a binary frame. Deepgram
// accepts the mic rate directly (declared in the Settings input config), so no
// resample is needed. Empty chunks are no-ops.
func (p *Provider) SendAudio(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("deepgram agent: %w", live.ErrNotConnected)
	}
	return conn.Write(context.Background(), websocket.MessageBinary, chunk)
}

// SendAudioStreamEnd is a no-op for Deepgram: the Voice Agent performs its own
// endpointing/turn-detection server-side and responds when the user stops
// speaking. There is no client-side commit in the protocol.
func (p *Provider) SendAudioStreamEnd() error { return nil }

// SendText injects a text user turn (e.g. an idle reminder) and lets the agent
// respond. Empty text is a no-op.
func (p *Provider) SendText(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("deepgram agent: %w", live.ErrNotConnected)
	}
	return p.sendJSON(conn, map[string]any{"type": "InjectUserMessage", "content": text})
}

// SendToolResponse returns a host-side function result to the agent.
func (p *Provider) SendToolResponse(response live.ToolResponse) error {
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("deepgram agent: %w", live.ErrNotConnected)
	}
	output := response.Response
	if output == nil {
		output = map[string]any{}
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("deepgram agent: marshal tool response: %w", err)
	}
	return p.sendJSON(conn, map[string]any{
		"type":    "FunctionCallResponse",
		"id":      response.ID,
		"name":    response.Name,
		"content": string(raw),
	})
}

// UpdateInstructions refreshes the agent's system prompt without a reconnect.
// Implements live.LiveInstructionUpdater.
func (p *Provider) UpdateInstructions(ctx context.Context, cfg live.LiveConfig) error {
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("deepgram agent: %w", live.ErrNotConnected)
	}
	prompt := composeDeepgramPrompt(cfg)
	if prompt == "" {
		return nil
	}
	// Write directly with the caller's ctx (rather than sendJSON's detached
	// background context) so an UpdatePrompt honors the request deadline.
	body, err := json.Marshal(map[string]any{"type": "UpdatePrompt", "prompt": prompt})
	if err != nil {
		return fmt.Errorf("deepgram agent: marshal UpdatePrompt: %w", err)
	}
	return conn.Write(ctx, websocket.MessageText, body)
}

// Close terminates the WebSocket and stops the keepalive loop. Idempotent.
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
	stop := p.keepAliveStop
	p.keepAliveStop = nil
	p.mu.Unlock()

	if stop != nil {
		close(stop)
	}
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
		return fmt.Errorf("deepgram agent: marshal frame: %w", err)
	}
	return conn.Write(context.Background(), websocket.MessageText, body)
}

func (p *Provider) snapshotConn() *websocket.Conn {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.conn
}

// sendSettings writes the Voice Agent Settings message: audio formats plus the
// listen/think/speak providers and the system prompt.
func (p *Provider) sendSettings(ctx context.Context, cfg live.LiveConfig) error {
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("deepgram agent: %w", live.ErrNotConnected)
	}

	writeCtx, cancel := context.WithTimeout(ctx, deepgramAgentWriteTimeout)
	defer cancel()
	body, err := json.Marshal(p.buildSettings(cfg))
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	return conn.Write(writeCtx, websocket.MessageText, body)
}

func (p *Provider) startKeepAlive() {
	stop := make(chan struct{})
	p.mu.Lock()
	p.keepAliveStop = stop
	p.mu.Unlock()

	go func() {
		ticker := time.NewTicker(deepgramAgentKeepAlive)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				conn := p.snapshotConn()
				if conn == nil {
					return
				}
				if err := p.sendJSON(conn, map[string]any{"type": "KeepAlive"}); err != nil {
					return
				}
			}
		}
	}()
}
