// Package assemblyai adapts the AssemblyAI Voice Agent WebSocket API
// (wss://agents.assemblyai.com/v1/ws) to [live.LiveProvider]. It needs an
// AssemblyAI API key in the [live.LiveConfig]; the LLM behind the agent is
// chosen server-side, so cfg.Model is not sent.
package assemblyai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	assemblyAIAgentURL          = "wss://agents.assemblyai.com/v1/ws"
	assemblyAIAgentReadLimit    = 4 << 20
	assemblyAIAgentWriteTimeout = 15 * time.Second
	assemblyAIDefaultVoice      = "ivy"
)

// Provider implements live.LiveProvider against AssemblyAI's Voice Agent API.
// It sends session.update on connect, waits for session.ready before returning,
// and maps the provider's event vocabulary onto SpeechKit's live.LiveMessage shape.
type Provider struct {
	mu        sync.RWMutex
	conn      *websocket.Conn
	sessionID string
	lastCfg   live.LiveConfig

	closeMu  sync.Mutex
	closed   bool
	closeErr error
}

// New returns an unconnected AssemblyAI Voice Agent provider.
func New() *Provider { return &Provider{} }

// Name implements [live.LiveProvider] by identifying the provider as
// "assemblyai-agent" in logs.
func (p *Provider) Name() string { return "assemblyai-agent" }

// EndpointURL reports the WS endpoint for connect-time logging
// (live.LiveEndpointReporter). Static URL, no credentials or query parameters.
func (p *Provider) EndpointURL() string { return assemblyAIAgentURL }

// SessionCapabilities reports the profile, default model and capability
// flags of the AssemblyAI catalog descriptor.
func (p *Provider) SessionCapabilities() live.SessionCapabilities {
	return live.SessionCapabilitiesForProvider("assemblyai")
}

// Connect dials the Voice Agent WebSocket with the API key as a Bearer
// token, sends the session.update built from cfg and waits for session.ready
// before returning. It fails with [live.ErrMissingAPIKey] when the key is
// empty and with the server's session.error when the session is rejected.
func (p *Provider) Connect(ctx context.Context, cfg live.LiveConfig) error {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return fmt.Errorf("assemblyai agent: %w", live.ErrMissingAPIKey)
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	conn, resp, err := websocket.Dial(ctx, assemblyAIAgentURL, &websocket.DialOptions{HTTPHeader: header})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("assemblyai agent: dial: %w", err)
	}
	conn.SetReadLimit(assemblyAIAgentReadLimit)

	// closed/closeErr belong to closeMu — Close guards them with it. Take
	// closeMu first and mu inside it, the same order Close uses; reversing the
	// two would deadlock against a concurrent Close, and resetting them under
	// mu alone (as this did) races a reconnect against a teardown.
	p.closeMu.Lock()
	p.mu.Lock()
	p.conn = conn
	p.sessionID = ""
	p.lastCfg = cfg
	p.mu.Unlock()
	p.closed = false
	p.closeErr = nil
	p.closeMu.Unlock()

	if err := p.sendSessionUpdate(ctx, cfg); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "session.update failed")
		return err
	}
	if err := p.waitReady(ctx, conn); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "session.ready failed")
		return err
	}
	return nil
}

// SendAudio upsamples a 16 kHz PCM16 mono mic chunk to 24 kHz and sends it
// base64-encoded as input.audio. Empty chunks are no-ops; it fails with
// [live.ErrNotConnected] before Connect and [live.ErrSessionNotReady] until
// session.ready arrived.
func (p *Provider) SendAudio(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("assemblyai agent: %w", live.ErrNotConnected)
	}
	if p.snapshotSessionID() == "" {
		return fmt.Errorf("assemblyai agent: %w", live.ErrSessionNotReady)
	}
	encoded := base64.StdEncoding.EncodeToString(live.UpsampleMicPCM16Mono(chunk))
	return p.sendJSON(context.Background(), conn, map[string]any{
		"type":  "input.audio",
		"audio": encoded,
	})
}

// SendAudioStreamEnd is a no-op: the Voice Agent detects turn ends
// server-side and the protocol has no client-side commit.
func (p *Provider) SendAudioStreamEnd() error { return nil }

// SendText asks the agent to reply following text as instructions
// (reply.create); this is how host prompts such as idle reminders are
// delivered. Blank text is a no-op.
func (p *Provider) SendText(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("assemblyai agent: %w", live.ErrNotConnected)
	}
	return p.sendJSON(context.Background(), conn, map[string]any{
		"type":         "reply.create",
		"instructions": text,
	})
}

// SendToolResponse returns a host-side tool result as tool.result with the
// Response map JSON-encoded into the result string; a nil Response sends an
// empty object.
func (p *Provider) SendToolResponse(response live.ToolResponse) error {
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("assemblyai agent: %w", live.ErrNotConnected)
	}
	result := response.Response
	if result == nil {
		result = map[string]any{}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("assemblyai agent: marshal tool result: %w", err)
	}
	return p.sendJSON(context.Background(), conn, map[string]any{
		"type":    "tool.result",
		"call_id": response.ID,
		"result":  string(raw),
	})
}

// UpdateInstructions implements [live.LiveInstructionUpdater] by sending a
// new session.update built from cfg on the open connection, so prompt, tools
// and voice change without a reconnect.
func (p *Provider) UpdateInstructions(ctx context.Context, cfg live.LiveConfig) error {
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("assemblyai agent: %w", live.ErrNotConnected)
	}
	body, err := json.Marshal(assemblyAISessionUpdate(cfg))
	if err != nil {
		return fmt.Errorf("assemblyai agent: marshal session.update: %w", err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, assemblyAIAgentWriteTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, body)
}

// Reconnect implements [live.LiveReconnector]: it dials a new connection
// with the last config, resumes the previous session id with
// session.resume, waits for session.ready and then closes the old
// connection. It fails with [live.ErrNoResumableSession] when no session id
// is known.
func (p *Provider) Reconnect(ctx context.Context) error {
	p.mu.RLock()
	cfg := p.lastCfg
	sessionID := strings.TrimSpace(p.sessionID)
	oldConn := p.conn
	p.mu.RUnlock()
	if sessionID == "" {
		return fmt.Errorf("assemblyai agent: %w", live.ErrNoResumableSession)
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return fmt.Errorf("assemblyai agent: %w", live.ErrMissingAPIKey)
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	conn, resp, err := websocket.Dial(ctx, assemblyAIAgentURL, &websocket.DialOptions{HTTPHeader: header})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("assemblyai agent: resume dial: %w", err)
	}
	conn.SetReadLimit(assemblyAIAgentReadLimit)
	if err := p.sendJSON(ctx, conn, map[string]any{"type": "session.resume", "session_id": sessionID}); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "session.resume failed")
		return fmt.Errorf("assemblyai agent: session.resume: %w", err)
	}
	if err := p.waitReady(ctx, conn); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "session.resume ready failed")
		return err
	}
	// Same closeMu -> mu ordering as Connect and Close; see Connect.
	p.closeMu.Lock()
	p.mu.Lock()
	p.conn = conn
	p.mu.Unlock()
	p.closed = false
	p.closeErr = nil
	p.closeMu.Unlock()
	if oldConn != nil && oldConn != conn {
		_ = oldConn.Close(websocket.StatusNormalClosure, "client reconnect")
	}
	return nil
}

// Receive reads server events until one maps to a [live.LiveMessage]: user
// and agent transcripts, reply audio, reply completion (with its
// interruption status), tool calls, and session.ended, which is returned
// with GoAway set. Status-only events are swallowed; session.error and
// decoding failures are returned as errors, and a closed socket yields
// io.EOF.
func (p *Provider) Receive(ctx context.Context) (*live.LiveMessage, error) {
	conn := p.snapshotConn()
	if conn == nil {
		return nil, fmt.Errorf("assemblyai agent: %w", live.ErrNotConnected)
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
			return nil, fmt.Errorf("assemblyai agent: ws read: %w", err)
		}
		if typ != websocket.MessageText {
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

// Close sends session.end and closes the WebSocket. It is idempotent and
// repeats the first close error on later calls.
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
	p.sessionID = ""
	p.mu.Unlock()

	if conn == nil {
		return nil
	}
	_ = p.sendJSON(context.Background(), conn, map[string]any{"type": "session.end"})
	err := conn.Close(websocket.StatusNormalClosure, "client close")
	p.closeErr = err
	return err
}

func (p *Provider) sendSessionUpdate(ctx context.Context, cfg live.LiveConfig) error {
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("assemblyai agent: %w", live.ErrNotConnected)
	}
	body, err := json.Marshal(assemblyAISessionUpdate(cfg))
	if err != nil {
		return fmt.Errorf("assemblyai agent: marshal session.update: %w", err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, assemblyAIAgentWriteTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, body)
}

func (p *Provider) waitReady(ctx context.Context, conn *websocket.Conn) error {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("assemblyai agent: wait ready: %w", err)
		}
		if typ != websocket.MessageText {
			continue
		}
		var env struct {
			Type      string `json:"type"`
			SessionID string `json:"session_id"`
			Code      string `json:"code"`
			Message   string `json:"message"`
		}
		if err := json.Unmarshal(data, &env); err != nil {
			return fmt.Errorf("assemblyai agent: decode ready event: %w", err)
		}
		switch env.Type {
		case "session.ready":
			p.mu.Lock()
			p.sessionID = strings.TrimSpace(env.SessionID)
			p.mu.Unlock()
			return nil
		case "session.error":
			return fmt.Errorf("assemblyai agent: session error %s: %s", env.Code, env.Message)
		default:
			continue
		}
	}
}

func (p *Provider) parseEvent(data []byte) (*live.LiveMessage, bool, error) {
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, false, fmt.Errorf("assemblyai agent: decode event: %w", err)
	}
	switch env.Type {
	case "session.ready":
		var ev struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(data, &ev)
		p.mu.Lock()
		p.sessionID = strings.TrimSpace(ev.SessionID)
		p.mu.Unlock()
		return nil, true, nil
	case "session.updated", "input.speech.started", "input.speech.stopped", "reply.started":
		return nil, true, nil
	case "session.ended":
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventSessionEnd, Done: true, GoAway: true}, env.Type), false, nil
	case "transcript.user.delta":
		var ev struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(data, &ev)
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventInputPartial, InputTranscript: ev.Text}, env.Type), false, nil
	case "transcript.user":
		var ev struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(data, &ev)
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventInputFinal, InputTranscript: ev.Text, InputTranscriptDone: true}, env.Type), false, nil
	case "reply.audio":
		var ev struct {
			Data string `json:"data"`
		}
		_ = json.Unmarshal(data, &ev)
		audio, err := base64.StdEncoding.DecodeString(ev.Data)
		if err != nil {
			return nil, false, fmt.Errorf("assemblyai agent: decode reply.audio: %w", err)
		}
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventOutputAudio, Audio: audio}, env.Type), false, nil
	case "transcript.agent":
		var ev struct {
			Text        string `json:"text"`
			Interrupted bool   `json:"interrupted"`
		}
		_ = json.Unmarshal(data, &ev)
		return live.NormalizeMessageEvents(&live.LiveMessage{
			EventType:            live.LiveEventOutputText,
			Text:                 ev.Text,
			OutputTranscript:     ev.Text,
			OutputTranscriptDone: true,
			Interrupted:          ev.Interrupted,
		}, env.Type), false, nil
	case "reply.done":
		var ev struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(data, &ev)
		return live.NormalizeMessageEvents(&live.LiveMessage{
			EventType:   live.LiveEventTurnEnd,
			Done:        true,
			Interrupted: strings.EqualFold(ev.Status, "interrupted"),
		}, env.Type), false, nil
	case "tool.call":
		var ev struct {
			CallID    string         `json:"call_id"`
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			return nil, false, fmt.Errorf("assemblyai agent: decode tool.call: %w", err)
		}
		return live.NormalizeMessageEvents(&live.LiveMessage{
			EventType: live.LiveEventToolCall,
			ToolCalls: []live.ToolCall{{ID: ev.CallID, Name: ev.Name, Args: ev.Arguments}},
		}, env.Type), false, nil
	case "session.error":
		var ev struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Param   string `json:"param"`
		}
		_ = json.Unmarshal(data, &ev)
		return nil, false, fmt.Errorf("assemblyai agent: session error %s: %s %s", ev.Code, ev.Message, ev.Param)
	default:
		return nil, true, nil
	}
}

func (p *Provider) sendJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("assemblyai agent: marshal frame: %w", err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, assemblyAIAgentWriteTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, body)
}

func (p *Provider) snapshotConn() *websocket.Conn {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.conn
}

func (p *Provider) snapshotSessionID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.sessionID
}

// assemblyAISessionUpdate builds the session.update payload.
//
// Note on LLM/model selection: the AssemblyAI Voice Agents WS API
// (agents.assemblyai.com) does NOT accept an inline model/LLM field in the
// session config — verified against the events reference
// (https://www.assemblyai.com/docs/voice-agents/voice-agent-api/events-reference,
// 2026-08-28: session supports agent_id, system_prompt, greeting, input.*,
// output.*, tools only; the LLM behind the agent is chosen server-side or via
// a stored agent referenced by agent_id). cfg.Model is therefore intentionally
// not sent here. The [providers.assemblyai].llm_gateway_* models select LLMs
// on AssemblyAI's separate OpenAI-compatible LLM Gateway used by the Genkit
// flows (assist/summary/agent), not by this realtime session.
func assemblyAISessionUpdate(cfg live.LiveConfig) map[string]any {
	resolved := live.ResolveLiveOptions("assemblyai", "realtime.assemblyai.voice-agent", cfg, nil, nil)
	input := map[string]any{
		"format": map[string]any{"encoding": "audio/pcm"},
	}
	session := map[string]any{
		"input": input,
		"output": map[string]any{
			"voice":  assemblyAIVoice(aaFirst(resolved.Voice, cfg.Voice)),
			"format": map[string]any{"encoding": "audio/pcm"},
		},
	}
	if prompt := live.AppendContextPrompt(composeAssemblyAIPrompt(cfg), resolved.ContextPrompt); prompt != "" {
		session["system_prompt"] = prompt
	}
	if keyterms := resolved.Keyterms; len(keyterms) > 0 {
		input["keyterms"] = keyterms
	}
	if turnDetection := assemblyAITurnDetection(cfg.Policies.ActivityDetection); len(turnDetection) > 0 {
		input["turn_detection"] = turnDetection
	}
	if tools := assemblyAITools(cfg.Tools); len(tools) > 0 {
		session["tools"] = tools
	}
	return map[string]any{
		"type":    "session.update",
		"session": session,
	}
}

func composeAssemblyAIPrompt(cfg live.LiveConfig) string {
	prompt := strings.TrimSpace(cfg.FrameworkPrompt)
	if refinement := strings.TrimSpace(cfg.RefinementPrompt); refinement != "" {
		if prompt == "" {
			prompt = refinement
		} else {
			prompt += "\n\n" + refinement
		}
	}
	if prompt == "" {
		return strings.TrimSpace(cfg.VocabularyHint)
	}
	return prompt
}

func assemblyAITurnDetection(policy live.ActivityDetectionPolicy) map[string]any {
	out := map[string]any{}
	if policy.SilenceDurationMs > 0 {
		out["min_silence"] = int(policy.SilenceDurationMs)
	}
	if policy.ActivityHandling == live.ActivityHandlingNoInterrupt {
		out["interrupt_response"] = false
	}
	return out
}

func assemblyAITools(defs []live.ToolDefinition) []map[string]any {
	if len(defs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(defs))
	for _, def := range defs {
		item := map[string]any{
			"type":        "function",
			"name":        def.Name,
			"description": def.Description,
		}
		if def.ParametersJSONSchema != nil {
			item["parameters"] = def.ParametersJSONSchema
		}
		out = append(out, item)
	}
	return out
}

func aaFirst(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func assemblyAIVoice(value string) string {
	voice := strings.ToLower(strings.TrimSpace(value))
	if _, ok := assemblyAIKnownVoices[voice]; ok {
		return voice
	}
	return assemblyAIDefaultVoice
}

var assemblyAIKnownVoices = map[string]struct{}{
	"arjun":   {},
	"bella":   {},
	"david":   {},
	"diego":   {},
	"dmitri":  {},
	"eleanor": {},
	"emma":    {},
	"ethan":   {},
	"giulia":  {},
	"hana":    {},
	"helen":   {},
	"ivy":     {},
	"jack":    {},
	"james":   {},
	"joon":    {},
	"kyle":    {},
	"lena":    {},
	"luca":    {},
	"lucia":   {},
	"lukas":   {},
	"martha":  {},
	"mateo":   {},
	"mei":     {},
	"mia":     {},
	"mina":    {},
	"oliver":  {},
	"pierre":  {},
	"ren":     {},
	"river":   {},
	"sam":     {},
	"sophie":  {},
	"tyler":   {},
	"victor":  {},
	"winter":  {},
}

var (
	_ live.LiveProvider           = (*Provider)(nil)
	_ live.LiveInstructionUpdater = (*Provider)(nil)
)
