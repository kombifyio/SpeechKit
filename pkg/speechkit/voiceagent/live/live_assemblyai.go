package live

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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

// AssemblyAILive implements LiveProvider against AssemblyAI's Voice Agent API.
// It sends session.update on connect, waits for session.ready before returning,
// and maps the provider's event vocabulary onto SpeechKit's LiveMessage shape.
//
// Deprecated: moved to pkg/speechkit/voiceagent/live/assemblyai.Provider. This name is
// removed in v0.65.0; import the provider package instead.
type AssemblyAILive struct {
	mu        sync.RWMutex
	conn      *websocket.Conn
	sessionID string
	lastCfg   LiveConfig

	closeMu  sync.Mutex
	closed   bool
	closeErr error
}

// NewAssemblyAILive returns an unconnected AssemblyAI Voice Agent provider.
//
// Deprecated: moved to pkg/speechkit/voiceagent/live/assemblyai.New. This name is
// removed in v0.65.0; import the provider package instead.
func NewAssemblyAILive() *AssemblyAILive { return &AssemblyAILive{} }

func (p *AssemblyAILive) Name() string { return "assemblyai-agent" }

// EndpointURL reports the WS endpoint for connect-time logging
// (LiveEndpointReporter). Static URL, no credentials or query parameters.
func (p *AssemblyAILive) EndpointURL() string { return assemblyAIAgentURL }

func (p *AssemblyAILive) SessionCapabilities() SessionCapabilities {
	return sessionCapabilitiesForProvider("assemblyai")
}

func (p *AssemblyAILive) Connect(ctx context.Context, cfg LiveConfig) error {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return errors.New("assemblyai agent: APIKey is required")
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

	p.mu.Lock()
	p.conn = conn
	p.sessionID = ""
	p.lastCfg = cfg
	p.closed = false
	p.closeErr = nil
	p.mu.Unlock()

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

func (p *AssemblyAILive) SendAudio(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	conn := p.snapshotConn()
	if conn == nil {
		return errors.New("assemblyai agent: not connected")
	}
	if p.snapshotSessionID() == "" {
		return errors.New("assemblyai agent: session is not ready")
	}
	encoded := base64.StdEncoding.EncodeToString(upsampleMicPCM16Mono(chunk))
	return p.sendJSON(context.Background(), conn, map[string]any{
		"type":  "input.audio",
		"audio": encoded,
	})
}

func (p *AssemblyAILive) SendAudioStreamEnd() error { return nil }

func (p *AssemblyAILive) SendText(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	conn := p.snapshotConn()
	if conn == nil {
		return errors.New("assemblyai agent: not connected")
	}
	return p.sendJSON(context.Background(), conn, map[string]any{
		"type":         "reply.create",
		"instructions": text,
	})
}

func (p *AssemblyAILive) SendToolResponse(response ToolResponse) error {
	conn := p.snapshotConn()
	if conn == nil {
		return errors.New("assemblyai agent: not connected")
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

func (p *AssemblyAILive) UpdateInstructions(ctx context.Context, cfg LiveConfig) error {
	conn := p.snapshotConn()
	if conn == nil {
		return errors.New("assemblyai agent: not connected")
	}
	body, err := json.Marshal(assemblyAISessionUpdate(cfg))
	if err != nil {
		return fmt.Errorf("assemblyai agent: marshal session.update: %w", err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, assemblyAIAgentWriteTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, body)
}

func (p *AssemblyAILive) Reconnect(ctx context.Context) error {
	p.mu.RLock()
	cfg := p.lastCfg
	sessionID := strings.TrimSpace(p.sessionID)
	oldConn := p.conn
	p.mu.RUnlock()
	if sessionID == "" {
		return errors.New("assemblyai agent: no resumable session id")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return errors.New("assemblyai agent: APIKey is required")
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
	p.mu.Lock()
	p.conn = conn
	p.closed = false
	p.closeErr = nil
	p.mu.Unlock()
	if oldConn != nil && oldConn != conn {
		_ = oldConn.Close(websocket.StatusNormalClosure, "client reconnect")
	}
	return nil
}

func (p *AssemblyAILive) Receive(ctx context.Context) (*LiveMessage, error) {
	conn := p.snapshotConn()
	if conn == nil {
		return nil, errors.New("assemblyai agent: not connected")
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

func (p *AssemblyAILive) Close() error {
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

func (p *AssemblyAILive) sendSessionUpdate(ctx context.Context, cfg LiveConfig) error {
	conn := p.snapshotConn()
	if conn == nil {
		return errors.New("assemblyai agent: not connected")
	}
	body, err := json.Marshal(assemblyAISessionUpdate(cfg))
	if err != nil {
		return fmt.Errorf("assemblyai agent: marshal session.update: %w", err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, assemblyAIAgentWriteTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, body)
}

func (p *AssemblyAILive) waitReady(ctx context.Context, conn *websocket.Conn) error {
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

func (p *AssemblyAILive) parseEvent(data []byte) (*LiveMessage, bool, error) {
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
		return normalizeLiveMessageEvents(&LiveMessage{EventType: LiveEventSessionEnd, Done: true, GoAway: true}, env.Type), false, nil
	case "transcript.user.delta":
		var ev struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(data, &ev)
		return normalizeLiveMessageEvents(&LiveMessage{EventType: LiveEventInputPartial, InputTranscript: ev.Text}, env.Type), false, nil
	case "transcript.user":
		var ev struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(data, &ev)
		return normalizeLiveMessageEvents(&LiveMessage{EventType: LiveEventInputFinal, InputTranscript: ev.Text, InputTranscriptDone: true}, env.Type), false, nil
	case "reply.audio":
		var ev struct {
			Data string `json:"data"`
		}
		_ = json.Unmarshal(data, &ev)
		audio, err := base64.StdEncoding.DecodeString(ev.Data)
		if err != nil {
			return nil, false, fmt.Errorf("assemblyai agent: decode reply.audio: %w", err)
		}
		return normalizeLiveMessageEvents(&LiveMessage{EventType: LiveEventOutputAudio, Audio: audio}, env.Type), false, nil
	case "transcript.agent":
		var ev struct {
			Text        string `json:"text"`
			Interrupted bool   `json:"interrupted"`
		}
		_ = json.Unmarshal(data, &ev)
		return normalizeLiveMessageEvents(&LiveMessage{
			EventType:            LiveEventOutputText,
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
		return normalizeLiveMessageEvents(&LiveMessage{
			EventType:   LiveEventTurnEnd,
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
		return normalizeLiveMessageEvents(&LiveMessage{
			EventType: LiveEventToolCall,
			ToolCalls: []ToolCall{{ID: ev.CallID, Name: ev.Name, Args: ev.Arguments}},
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

func (p *AssemblyAILive) sendJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("assemblyai agent: marshal frame: %w", err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, assemblyAIAgentWriteTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, body)
}

func (p *AssemblyAILive) snapshotConn() *websocket.Conn {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.conn
}

func (p *AssemblyAILive) snapshotSessionID() string {
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
func assemblyAISessionUpdate(cfg LiveConfig) map[string]any {
	resolved := ResolveLiveOptions("assemblyai", "realtime.assemblyai.voice-agent", cfg, nil, nil)
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
	if prompt := appendContextPrompt(composeAssemblyAIPrompt(cfg), resolved.ContextPrompt); prompt != "" {
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

func composeAssemblyAIPrompt(cfg LiveConfig) string {
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

func assemblyAITurnDetection(policy ActivityDetectionPolicy) map[string]any {
	out := map[string]any{}
	if policy.SilenceDurationMs > 0 {
		out["min_silence"] = int(policy.SilenceDurationMs)
	}
	if policy.ActivityHandling == ActivityHandlingNoInterrupt {
		out["interrupt_response"] = false
	}
	return out
}

func assemblyAITools(defs []ToolDefinition) []map[string]any {
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
	_ LiveProvider           = (*AssemblyAILive)(nil)
	_ LiveInstructionUpdater = (*AssemblyAILive)(nil)
)
