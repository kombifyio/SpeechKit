// Voice Agent WebSocket helpers.
//
// CreateVoiceAgentSession + DialVoiceAgent let an embedder open a duplex
// realtime session against a running speechkit-server without writing the
// WebSocket plumbing or frame definitions by hand. The shapes mirror the
// server's wire protocol under internal/server/voiceagent/protocol.go.

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	ws "github.com/coder/websocket"
)

// VoiceAgentTicket is the minted session envelope returned by
// POST /v1/voiceagent/sessions. DialVoiceAgent prefers WSSubprotocol so the
// one-time ticket does not have to ride in the URL.
type VoiceAgentTicket struct {
	SessionID     string    `json:"session_id"`
	WSURL         string    `json:"ws_url"`
	WSSubprotocol string    `json:"ws_subprotocol,omitempty"`
	Ticket        string    `json:"ticket"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// VoiceAgentStartFrame opens a session and binds it to a persona/role/sequence.
// PersonaID is required if the deployment has any personas; the server's
// resolver falls back to the configured default persona when empty.
type VoiceAgentStartFrame struct {
	PersonaID            string `json:"persona_id,omitempty"`
	RoleID               string `json:"role_id,omitempty"`
	SequenceID           string `json:"sequence_id,omitempty"`
	Provider             string `json:"provider,omitempty"`
	MediaTransport       string `json:"media_transport,omitempty"` // "websocket" (default) or "livekit"
	Voice                string `json:"voice,omitempty"`
	Locale               string `json:"locale,omitempty"`
	Model                string `json:"model,omitempty"`
	Thinking             string `json:"thinking,omitempty"`
	SystemPromptOverride string `json:"system_prompt_override,omitempty"`
}

// VoiceAgentFrame is the parsed shape of any inbound text frame. Binary
// frames carry audio and are returned via VoiceAgentMessage.Audio instead.
type VoiceAgentFrame struct {
	Type       string   `json:"type"`
	EventType  string   `json:"event_type,omitempty"`
	EventTypes []string `json:"event_types,omitempty"`
	State      string   `json:"state,omitempty"`
	Text       string   `json:"text,omitempty"`
	Done       bool     `json:"done,omitempty"`
	ID         string   `json:"id,omitempty"`
	Name       string   `json:"name,omitempty"`
	Code       string   `json:"code,omitempty"`
	Message    string   `json:"message,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	SequenceID string   `json:"sequence_id,omitempty"`
	StepID     string   `json:"step_id,omitempty"`
	StepIndex  int      `json:"step_index,omitempty"`
	Status     string   `json:"status,omitempty"`
}

// VoiceAgentMessage is a single inbound event. Exactly one of Audio or Frame
// is set per call to ReadMessage; audio chunks are raw PCM S16LE at 24 kHz
// mono (Gemini Live native output rate).
type VoiceAgentMessage struct {
	Audio []byte
	Frame *VoiceAgentFrame
}

// VoiceAgentSession is a duplex WebSocket session. Use SendStart first, then
// SendText / SendAudio / SendAudioEnd to drive the conversation, and
// ReadMessage in a loop to consume server frames until SessionEnd arrives.
//
// The zero value is unusable — always go through DialVoiceAgent.
type VoiceAgentSession struct {
	conn      *ws.Conn
	sessionID string
}

// SessionID returns the manager-assigned session identifier.
func (s *VoiceAgentSession) SessionID() string {
	if s == nil {
		return ""
	}
	return s.sessionID
}

// CreateVoiceAgentSession mints a session + ticket. Pair the result with
// DialVoiceAgent to upgrade to the WebSocket.
func (c *Client) CreateVoiceAgentSession(ctx context.Context) (*VoiceAgentTicket, error) {
	return c.CreateVoiceAgentSessionWithOptions(ctx, VoiceAgentSessionOptions{})
}

type VoiceAgentSessionOptions struct {
	AISessionID   string `json:"ai_session_id,omitempty"`
	Provider      string `json:"provider,omitempty"`
	TargetAgentID string `json:"target_agent_id,omitempty"`
}

// CreateVoiceAgentSessionWithOptions mints a native/BYO session or selects a
// hosted registered agent when the caller also presents its capability lease.
func (c *Client) CreateVoiceAgentSessionWithOptions(ctx context.Context, options VoiceAgentSessionOptions) (*VoiceAgentTicket, error) {
	var raw struct {
		SessionID     string `json:"session_id"`
		WSURL         string `json:"ws_url"`
		WSSubprotocol string `json:"ws_subprotocol"`
		Ticket        string `json:"ticket"`
		ExpiresAt     string `json:"expires_at"`
	}
	if err := c.DoJSON(ctx, http.MethodPost, "/v1/voiceagent/sessions", options, &raw); err != nil {
		return nil, err
	}
	if raw.SessionID == "" || raw.WSURL == "" || raw.WSSubprotocol == "" || raw.Ticket == "" {
		return nil, errors.New("speechkit: voice agent create response missing fields")
	}
	t := &VoiceAgentTicket{
		SessionID:     raw.SessionID,
		WSURL:         raw.WSURL,
		WSSubprotocol: raw.WSSubprotocol,
		Ticket:        raw.Ticket,
	}
	if raw.ExpiresAt != "" {
		if parsed, err := time.Parse(time.RFC3339, raw.ExpiresAt); err == nil {
			t.ExpiresAt = parsed
		}
	}
	return t, nil
}

// DeleteVoiceAgentSession force-closes a session. Idempotent: 404 is treated
// as success because the server already removed it.
func (c *Client) DeleteVoiceAgentSession(ctx context.Context, sessionID string) error {
	err := c.DoJSON(ctx, http.MethodDelete, "/v1/voiceagent/sessions/"+url.PathEscape(strings.TrimSpace(sessionID)), nil, nil)
	if err == nil {
		return nil
	}
	var httpErr HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
		return nil
	}
	return err
}

// DialVoiceAgent upgrades the ticket to a WebSocket. The caller still needs
// to send a start frame (see VoiceAgentSession.SendStart) before any other
// I/O.
func (c *Client) DialVoiceAgent(ctx context.Context, ticket *VoiceAgentTicket) (*VoiceAgentSession, error) {
	if ticket == nil || strings.TrimSpace(ticket.WSURL) == "" {
		return nil, errors.New("speechkit: voice agent ticket is empty")
	}
	opts := &ws.DialOptions{}
	if ticket.WSSubprotocol != "" {
		opts.Subprotocols = []string{ticket.WSSubprotocol}
	}
	if c.token != "" {
		opts.HTTPHeader = http.Header{}
		opts.HTTPHeader.Set("Authorization", "Bearer "+c.token)
	}
	conn, resp, err := ws.Dial(ctx, ticket.WSURL, opts)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("speechkit: voice agent dial: %w", err)
	}
	// 4 MiB read limit is generous for control frames; audio chunks are
	// typically 4-8 KiB.
	conn.SetReadLimit(4 << 20)
	return &VoiceAgentSession{conn: conn, sessionID: ticket.SessionID}, nil
}

// SendStart sends the mandatory first control frame. MediaTransport defaults
// to "websocket" when empty.
func (s *VoiceAgentSession) SendStart(ctx context.Context, frame VoiceAgentStartFrame) error {
	if strings.TrimSpace(frame.MediaTransport) == "" {
		frame.MediaTransport = "websocket"
	}
	payload := map[string]any{
		"type":            "start",
		"media_transport": frame.MediaTransport,
	}
	if frame.PersonaID != "" {
		payload["persona_id"] = frame.PersonaID
	}
	if frame.RoleID != "" {
		payload["role_id"] = frame.RoleID
	}
	if frame.SequenceID != "" {
		payload["sequence_id"] = frame.SequenceID
	}
	if frame.Provider != "" {
		payload["provider"] = frame.Provider
	}
	if frame.Voice != "" {
		payload["voice"] = frame.Voice
	}
	if frame.Locale != "" {
		payload["locale"] = frame.Locale
	}
	if frame.Model != "" {
		payload["model"] = frame.Model
	}
	if frame.Thinking != "" {
		payload["thinking"] = frame.Thinking
	}
	if frame.SystemPromptOverride != "" {
		payload["system_prompt_override"] = frame.SystemPromptOverride
	}
	return s.writeJSON(ctx, payload)
}

// SendText injects a text turn (the agent will reply via audio + transcript).
func (s *VoiceAgentSession) SendText(ctx context.Context, text string) error {
	return s.writeJSON(ctx, map[string]any{"type": "text", "text": text})
}

// SendAudio forwards a PCM chunk (16 kHz, signed 16-bit little-endian, mono).
func (s *VoiceAgentSession) SendAudio(ctx context.Context, chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	return s.conn.Write(ctx, ws.MessageBinary, chunk)
}

// SendAudioEnd marks the end of the current microphone turn. Only needed
// when automatic activity detection is disabled.
func (s *VoiceAgentSession) SendAudioEnd(ctx context.Context) error {
	return s.writeJSON(ctx, map[string]any{"type": "audio_end"})
}

// AdvanceStep advances the active sequence step. Reason is optional and
// surfaces in the resulting sequence_step frame.
func (s *VoiceAgentSession) AdvanceStep(ctx context.Context, reason string) error {
	payload := map[string]any{"type": "advance_step"}
	if reason != "" {
		payload["reason"] = reason
	}
	return s.writeJSON(ctx, payload)
}

// SendStop asks the server to gracefully end the session.
func (s *VoiceAgentSession) SendStop(ctx context.Context) error {
	return s.writeJSON(ctx, map[string]any{"type": "stop"})
}

// ReadMessage blocks until the next inbound WebSocket message arrives.
// Binary frames are returned as Audio; text frames are decoded into Frame.
// Returns io.EOF or a websocket close error when the peer goes away.
func (s *VoiceAgentSession) ReadMessage(ctx context.Context) (VoiceAgentMessage, error) {
	typ, data, err := s.conn.Read(ctx)
	if err != nil {
		return VoiceAgentMessage{}, err
	}
	if typ == ws.MessageBinary {
		return VoiceAgentMessage{Audio: data}, nil
	}
	var frame VoiceAgentFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return VoiceAgentMessage{}, fmt.Errorf("speechkit: decode voice agent frame: %w (raw=%s)", err, truncate(data, 240))
	}
	return VoiceAgentMessage{Frame: &frame}, nil
}

// Close releases the WebSocket. Safe to call multiple times.
func (s *VoiceAgentSession) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	_ = s.conn.Close(ws.StatusNormalClosure, "client closing")
	return s.conn.CloseNow()
}

func (s *VoiceAgentSession) writeJSON(ctx context.Context, v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("speechkit: marshal frame: %w", err)
	}
	return s.conn.Write(ctx, ws.MessageText, buf)
}

func truncate(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
