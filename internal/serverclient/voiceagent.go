package serverclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/internal/voiceagent"
)

// VoiceAgentProvider implements voiceagent.LiveProvider against the remote
// SpeechKit server's POST /v1/voiceagent/sessions + WebSocket protocol.
// The kernel-side voiceagent.Session orchestrates Connect/Send*/Receive
// without knowing whether the LiveProvider is in-process Gemini Live or
// this network-hop adapter.
type VoiceAgentProvider struct {
	client *Client

	// Optional Persona/Role/Sequence IDs propagated as fields on the
	// initial Start frame. The kernel-side Session only knows about
	// LiveConfig fields (Voice, Locale, â€¦); applications that want richer
	// catalog selection set these at construction time.
	personaID  string
	roleID     string
	sequenceID string

	mu       sync.RWMutex
	conn     *websocket.Conn
	sessID   string
	closeMu  sync.Mutex
	closeErr error
}

// VoiceAgentOption configures a VoiceAgentProvider.
type VoiceAgentOption func(*VoiceAgentProvider)

// WithPersona presets the Start-frame persona_id. Pass an empty string to
// fall back to the server's default persona (typically "default").
func WithPersona(id string) VoiceAgentOption {
	return func(p *VoiceAgentProvider) { p.personaID = strings.TrimSpace(id) }
}

// WithRole presets the Start-frame role_id.
func WithRole(id string) VoiceAgentOption {
	return func(p *VoiceAgentProvider) { p.roleID = strings.TrimSpace(id) }
}

// WithSequence presets the Start-frame sequence_id.
func WithSequence(id string) VoiceAgentOption {
	return func(p *VoiceAgentProvider) { p.sequenceID = strings.TrimSpace(id) }
}

// NewVoiceAgent constructs a VoiceAgentProvider.
func NewVoiceAgent(client *Client, opts ...VoiceAgentOption) (*VoiceAgentProvider, error) {
	if client == nil {
		return nil, errors.New("serverclient: NewVoiceAgent requires a non-nil Client")
	}
	p := &VoiceAgentProvider{client: client}
	for _, o := range opts {
		o(p)
	}
	return p, nil
}

// Name identifies this provider in voiceagent logs.
func (p *VoiceAgentProvider) Name() string { return "server-voiceagent" }

// createSessionResponse mirrors the server's wire shape; field names must
// stay aligned with internal/server/voiceagent.createSessionResponse.
type createSessionResponse struct {
	SessionID string `json:"session_id"`
	WSURL     string `json:"ws_url"`
	Ticket    string `json:"ticket"`
	ExpiresAt string `json:"expires_at"`
}

// startFrame is the payload of the mandatory first WS frame. The shape
// mirrors internal/server/voiceagent.StartFrame; we keep our own type
// here so the device-target doesn't depend on the //go:build linux
// server package.
type startFrame struct {
	Type                 string `json:"type"`
	PersonaID            string `json:"persona_id,omitempty"`
	RoleID               string `json:"role_id,omitempty"`
	SequenceID           string `json:"sequence_id,omitempty"`
	Voice                string `json:"voice,omitempty"`
	Locale               string `json:"locale,omitempty"`
	Model                string `json:"model,omitempty"`
	SystemPromptOverride string `json:"system_prompt_override,omitempty"`
}

// Connect creates the server-side session, dials the WebSocket using the
// returned ticket, and sends the mandatory Start frame. Errors at any of
// the three stages are wrapped with phase context so logs show where the
// failure occurred (POST vs dial vs first-frame).
func (p *VoiceAgentProvider) Connect(ctx context.Context, cfg voiceagent.LiveConfig) error {
	// 1. POST /v1/voiceagent/sessions to mint session + ticket.
	createReq, err := p.client.newRequest(ctx, http.MethodPost, "/v1/voiceagent/sessions",
		bytes.NewReader([]byte("{}")), "application/json")
	if err != nil {
		return err
	}
	resp, err := p.client.httpClient.Do(createReq)
	if err != nil {
		return fmt.Errorf("serverclient: POST /v1/voiceagent/sessions: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return decodeErrorEnvelope(resp, 16<<10)
	}
	var created createSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return fmt.Errorf("serverclient: decode session response: %w", err)
	}
	if created.WSURL == "" || created.SessionID == "" {
		return errors.New("serverclient: server returned empty ws_url or session_id")
	}

	// 2. Dial the WebSocket. The server's POST handler builds an absolute
	// URL using the request scheme/host; we honour it but normalise scheme
	// in case the server is behind a reverse proxy that didn't forward
	// X-Forwarded-Proto.
	wsURL, err := normaliseWSURL(p.client.BaseURL(), created.WSURL)
	if err != nil {
		return fmt.Errorf("serverclient: normalise WS URL: %w", err)
	}

	dialOpts := &websocket.DialOptions{
		HTTPClient: p.client.HTTPClient(),
		HTTPHeader: http.Header{},
	}
	if tok := p.client.BearerToken(); tok != "" {
		dialOpts.HTTPHeader.Set("Authorization", "Bearer "+tok)
	}
	conn, dialResp, err := websocket.Dial(ctx, wsURL, dialOpts)
	if err != nil {
		return fmt.Errorf("serverclient: WS dial %s: %w", wsURL, err)
	}
	// Drain + close the upgrade response body so the underlying TCP
	// connection can be reused. coder/websocket consumes the upgrade
	// response internally on success but documents that callers should
	// still close Body to be safe.
	if dialResp != nil && dialResp.Body != nil {
		_ = dialResp.Body.Close()
	}

	// 3. Send the mandatory Start frame so the server transitions out of
	// the "awaiting start" state.
	frame := startFrame{
		Type:                 "start",
		PersonaID:            p.personaID,
		RoleID:               p.roleID,
		SequenceID:           p.sequenceID,
		Voice:                cfg.Voice,
		Locale:               cfg.Locale,
		Model:                cfg.Model,
		SystemPromptOverride: cfg.FrameworkPrompt,
	}
	body, err := json.Marshal(frame)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "marshal start frame")
		return fmt.Errorf("serverclient: marshal start frame: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, body); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "write start frame")
		return fmt.Errorf("serverclient: write start frame: %w", err)
	}

	p.mu.Lock()
	p.conn = conn
	p.sessID = created.SessionID
	p.mu.Unlock()
	return nil
}

// SendAudio writes a binary PCM 16kHz S16 mono chunk to the server. The
// server forwards the bytes verbatim to the underlying provider after
// minor framing; chunk sizes between 20 ms and 100 ms are recommended.
func (p *VoiceAgentProvider) SendAudio(chunk []byte) error {
	conn := p.snapshotConn()
	if conn == nil {
		return errors.New("serverclient: not connected")
	}
	if len(chunk) == 0 {
		return nil
	}
	return conn.Write(context.Background(), websocket.MessageBinary, chunk)
}

// SendAudioStreamEnd signals end-of-turn to the server. The server-side
// adapter forwards it to the kernel which translates it to the
// provider-specific stream-end signal.
func (p *VoiceAgentProvider) SendAudioStreamEnd() error {
	return p.sendJSON(map[string]string{"type": "audio_end"})
}

// SendText injects a text-only turn (used by the kernel for idle reminders
// or when the orchestration layer wants to fake user input).
func (p *VoiceAgentProvider) SendText(text string) error {
	return p.sendJSON(map[string]string{"type": "text", "text": text})
}

// SendToolResponse forwards the result of a host-side tool execution.
func (p *VoiceAgentProvider) SendToolResponse(response voiceagent.ToolResponse) error {
	frame := map[string]any{
		"type":     "tool_response",
		"id":       response.ID,
		"name":     response.Name,
		"response": response.Response,
	}
	return p.sendJSON(frame)
}

// Close terminates the WebSocket session. Idempotent â€” a second Close
// call returns nil.
func (p *VoiceAgentProvider) Close() error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()

	p.mu.Lock()
	conn := p.conn
	p.conn = nil
	p.mu.Unlock()

	if conn == nil {
		return p.closeErr
	}
	// Best-effort polite Stop frame so the server can advance state cleanly
	// before we tear down the connection.
	_ = writeJSON(conn, map[string]string{"type": "stop"})
	err := conn.Close(websocket.StatusNormalClosure, "client close")
	p.closeErr = err
	return err
}

// Receive reads the next server-emitted frame and translates it to a
// voiceagent.LiveMessage. Frames the kernel doesn't care about (state,
// pong, sequence_step) are swallowed and the function loops to the next
// frame so callers don't see them.
func (p *VoiceAgentProvider) Receive(ctx context.Context) (*voiceagent.LiveMessage, error) {
	conn := p.snapshotConn()
	if conn == nil {
		return nil, errors.New("serverclient: not connected")
	}
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return nil, fmt.Errorf("serverclient: WS read: %w", err)
		}
		switch typ {
		case websocket.MessageBinary:
			// Server-side audio output is 24kHz S16 mono PCM (Gemini Live
			// native rate). The kernel's LiveMessage already documents this.
			return &voiceagent.LiveMessage{Audio: append([]byte(nil), data...)}, nil
		case websocket.MessageText:
			msg, swallow, err := translateTextFrame(data)
			if err != nil {
				return nil, err
			}
			if swallow {
				continue
			}
			return msg, nil
		default:
			// Unknown WS frame types â€” defensive loop.
			continue
		}
	}
}

// translateTextFrame parses a JSON server frame and converts it into a
// LiveMessage. Returns (nil, true, nil) when the frame should be swallowed
// internally and Receive() should fetch the next one.
func translateTextFrame(data []byte) (*voiceagent.LiveMessage, bool, error) {
	var env struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Done    bool   `json:"done"`
		Code    string `json:"code"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
		ID      string `json:"id"`
		Name    string `json:"name"`
		Args    map[string]any
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, false, fmt.Errorf("serverclient: decode frame: %w", err)
	}
	switch env.Type {
	case "input_transcript":
		return &voiceagent.LiveMessage{
			InputTranscript:     env.Text,
			InputTranscriptDone: env.Done,
		}, false, nil
	case "output_transcript":
		return &voiceagent.LiveMessage{
			OutputTranscript:     env.Text,
			OutputTranscriptDone: env.Done,
		}, false, nil
	case "interrupted":
		return &voiceagent.LiveMessage{Interrupted: true}, false, nil
	case "session_end":
		// Reason "go_away" means upstream signalled migrate; the kernel's
		// reconnect hook (if registered) will handle it. Other reasons are
		// terminal â€” propagate as GoAway too so Session emits a clean
		// session_end to the UI.
		_ = env.Reason
		return &voiceagent.LiveMessage{GoAway: true}, false, nil
	case "tool_call":
		// The minimal envelope above doesn't unmarshal Args (it's a free-
		// form map). Re-decode the relevant subset so the kernel sees the
		// tool call shape it expects.
		var rich struct {
			ID   string         `json:"id"`
			Name string         `json:"name"`
			Args map[string]any `json:"args"`
		}
		if err := json.Unmarshal(data, &rich); err != nil {
			return nil, false, fmt.Errorf("serverclient: decode tool_call: %w", err)
		}
		return &voiceagent.LiveMessage{
			ToolCalls: []voiceagent.ToolCall{{
				ID:   rich.ID,
				Name: rich.Name,
				Args: rich.Args,
			}},
		}, false, nil
	case "error":
		return nil, false, &ServerError{
			Status:  0, // not an HTTP status â€” provider-level error frame
			Code:    env.Code,
			Message: env.Message,
		}
	case "state", "pong", "sequence_step":
		// Status-only frames the kernel doesn't model; swallow.
		return nil, true, nil
	default:
		// Unknown frame types â€” be forward-compatible and swallow rather
		// than fail. Server may grow new control messages.
		return nil, true, nil
	}
}

func (p *VoiceAgentProvider) sendJSON(v any) error {
	conn := p.snapshotConn()
	if conn == nil {
		return errors.New("serverclient: not connected")
	}
	return writeJSON(conn, v)
}

func writeJSON(conn *websocket.Conn, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("serverclient: marshal frame: %w", err)
	}
	return conn.Write(context.Background(), websocket.MessageText, body)
}

func (p *VoiceAgentProvider) snapshotConn() *websocket.Conn {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.conn
}

// normaliseWSURL takes the server-supplied ws_url plus the configured
// BaseURL and produces a final ws/wss URL. Some deployments terminate TLS
// at a reverse proxy that doesn't forward X-Forwarded-Proto, so the
// server may emit "ws://" even though the device should hit "wss://".
// We force the scheme to match the device's BaseURL scheme.
func normaliseWSURL(base *url.URL, raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if base != nil {
		switch base.Scheme {
		case "https":
			parsed.Scheme = "wss"
		case "http":
			parsed.Scheme = "ws"
		}
		if parsed.Host == "" {
			parsed.Host = base.Host
		}
	}
	return parsed.String(), nil
}

// Compile-time assertion against the kernel interface.
var _ voiceagent.LiveProvider = (*VoiceAgentProvider)(nil)
