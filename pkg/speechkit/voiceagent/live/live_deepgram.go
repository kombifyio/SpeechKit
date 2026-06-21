package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Deepgram Voice Agent API constants. The Agent WebSocket carries the full
// audio-to-audio loop (Nova-3 listen → configurable think LLM → Aura-2 speak)
// over a single connection. SpeechKit's mic emits 16 kHz PCM16, which Deepgram
// accepts directly on the input — no upsample needed. Output is requested at
// 24 kHz to match the kernel's LiveMessage.Audio contract.
const (
	deepgramAgentURL = "wss://agent.deepgram.com/v1/agent/converse"

	deepgramListenModelDefault   = "nova-3"
	deepgramSpeakModelDefaultEN  = "aura-2-thalia-en"
	deepgramSpeakModelDefaultDE  = "aura-2-viktoria-de"
	deepgramThinkProviderDefault = "open_ai"
	deepgramThinkModelDefault    = "gpt-4o-mini"

	deepgramAgentInputSampleRate  = 16000 // SpeechKit mic capture rate; sent as-is.
	deepgramAgentOutputSampleRate = 24000 // matches LiveMessage.Audio 24 kHz contract.

	deepgramAgentKeepAlive    = 8 * time.Second
	deepgramAgentReadLimit    = 4 << 20
	deepgramAgentWriteTimeout = 15 * time.Second
)

// DeepgramLive implements LiveProvider against the Deepgram Voice Agent API
// (WebSocket). It mirrors GeminiLive/OpenAILive's surface so callers don't need
// to know which backend is active.
//
// The think (LLM) leg is configurable: Deepgram drives the LLM server-side, so
// ThinkProvider/ThinkModel select which model reasons over the transcript.
// Defaults target a widely-available option; the wiring layer overrides them
// from deployment config. Listen is always Deepgram Nova-3 and speak is always
// a Deepgram Aura-2 voice — that is the point of routing a session here.
type DeepgramLive struct {
	// Optional overrides; zero values fall back to the package defaults.
	ListenModel   string
	SpeakModel    string
	ThinkProvider string
	ThinkModel    string

	// ThinkEndpointURL + ThinkAPIKey switch the think leg to a bring-your-own
	// LLM deployment. When ThinkEndpointURL is set, the Settings message carries
	// an agent.think.endpoint block so Deepgram calls the operator's own LLM
	// instead of a Deepgram-managed model; ThinkAPIKey (when set) is sent as an
	// "Authorization: Bearer <key>" header on that endpoint. Leave both empty to
	// use Deepgram's managed LLM for ThinkProvider/ThinkModel (no key needed).
	ThinkEndpointURL string
	ThinkAPIKey      string

	mu         sync.RWMutex
	conn       *websocket.Conn
	lastConfig *LiveConfig

	closeMu  sync.Mutex
	closed   bool
	closeErr error

	keepAliveStop chan struct{}
}

// NewDeepgramLive returns a fresh Deepgram Voice Agent provider.
func NewDeepgramLive() *DeepgramLive { return &DeepgramLive{} }

// ConfigureThink applies the deployment's think-LLM selection to the provider.
// Non-empty provider/model override the package defaults; empty values keep the
// Deepgram-managed default. endpointURL/apiKey select a bring-your-own think LLM
// (see ThinkEndpointURL/ThinkAPIKey) and are cleared when empty. Both Targets
// call this from their Voice Agent wiring with values resolved from config.
func (p *DeepgramLive) ConfigureThink(provider, model, endpointURL, apiKey string) {
	if v := strings.TrimSpace(provider); v != "" {
		p.ThinkProvider = v
	}
	if v := strings.TrimSpace(model); v != "" {
		p.ThinkModel = v
	}
	p.ThinkEndpointURL = strings.TrimSpace(endpointURL)
	p.ThinkAPIKey = strings.TrimSpace(apiKey)
}

// Name identifies the provider in Voice Agent logs.
func (p *DeepgramLive) Name() string { return "deepgram-agent" }

// Connect dials the Deepgram Voice Agent WebSocket and sends the initial
// Settings message describing listen/think/speak and the audio formats. The
// SettingsApplied acknowledgement is consumed asynchronously by Receive().
func (p *DeepgramLive) Connect(ctx context.Context, cfg LiveConfig) error {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return errors.New("deepgram agent: APIKey is required")
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
	p.mu.Lock()
	p.conn = conn
	p.lastConfig = &cfgCopy
	p.closed = false
	p.closeErr = nil
	p.mu.Unlock()

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
func (p *DeepgramLive) SendAudio(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	conn := p.snapshotConn()
	if conn == nil {
		return errors.New("deepgram agent: not connected")
	}
	return conn.Write(context.Background(), websocket.MessageBinary, chunk)
}

// SendAudioStreamEnd is a no-op for Deepgram: the Voice Agent performs its own
// endpointing/turn-detection server-side and responds when the user stops
// speaking. There is no client-side commit in the protocol.
func (p *DeepgramLive) SendAudioStreamEnd() error { return nil }

// SendText injects a text user turn (e.g. an idle reminder) and lets the agent
// respond. Empty text is a no-op.
func (p *DeepgramLive) SendText(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	conn := p.snapshotConn()
	if conn == nil {
		return errors.New("deepgram agent: not connected")
	}
	return p.sendJSON(conn, map[string]any{"type": "InjectUserMessage", "content": text})
}

// SendToolResponse returns a host-side function result to the agent.
func (p *DeepgramLive) SendToolResponse(response ToolResponse) error {
	conn := p.snapshotConn()
	if conn == nil {
		return errors.New("deepgram agent: not connected")
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
// Implements LiveInstructionUpdater.
func (p *DeepgramLive) UpdateInstructions(ctx context.Context, cfg LiveConfig) error {
	conn := p.snapshotConn()
	if conn == nil {
		return errors.New("deepgram agent: not connected")
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
func (p *DeepgramLive) Close() error {
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

// Receive translates the next server frame into a LiveMessage. Binary frames
// are agent audio; text frames are JSON control events. Events that don't map
// to LiveMessage fields are swallowed and the loop fetches the next frame.
func (p *DeepgramLive) Receive(ctx context.Context) (*LiveMessage, error) {
	conn := p.snapshotConn()
	if conn == nil {
		return nil, errors.New("deepgram agent: not connected")
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
			return &LiveMessage{Audio: data}, nil
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

// ── internal helpers ────────────────────────────────────────────────────────

func (p *DeepgramLive) sendJSON(conn *websocket.Conn, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("deepgram agent: marshal frame: %w", err)
	}
	return conn.Write(context.Background(), websocket.MessageText, body)
}

func (p *DeepgramLive) snapshotConn() *websocket.Conn {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.conn
}

// sendSettings writes the Voice Agent Settings message: audio formats plus the
// listen/think/speak providers and the system prompt.
func (p *DeepgramLive) sendSettings(ctx context.Context, cfg LiveConfig) error {
	conn := p.snapshotConn()
	if conn == nil {
		return errors.New("deepgram agent: not connected")
	}

	writeCtx, cancel := context.WithTimeout(ctx, deepgramAgentWriteTimeout)
	defer cancel()
	body, err := json.Marshal(p.buildSettings(cfg))
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	return conn.Write(writeCtx, websocket.MessageText, body)
}

// buildSettings assembles the Voice Agent Settings message: audio formats plus
// the listen/think/speak providers, the system prompt, and any tool functions.
// It is pure (no I/O) so the listen/think/speak wiring can be unit-tested.
func (p *DeepgramLive) buildSettings(cfg LiveConfig) map[string]any {
	resolved := ResolveLiveOptions("deepgram", "realtime.deepgram.voice-agent", cfg, nil, nil)
	think := map[string]any{
		"provider": map[string]any{
			"type":  dgFirst(p.ThinkProvider, deepgramThinkProviderDefault),
			"model": dgFirst(p.ThinkModel, deepgramThinkModelDefault),
		},
	}
	if endpoint := p.thinkEndpoint(); endpoint != nil {
		think["endpoint"] = endpoint
	}
	if prompt := composeDeepgramPrompt(cfg); prompt != "" {
		think["prompt"] = prompt
	}
	if funcs := buildDeepgramFunctions(cfg.Tools); len(funcs) > 0 {
		think["functions"] = funcs
	}

	listenProvider := map[string]any{
		"type":  "deepgram",
		"model": dgFirst(p.ListenModel, deepgramListenModelDefault),
	}
	// VocabularyHint boosts recognition of domain terms — Nova-3 exposes this
	// as keyterms on the listen provider.
	if keyterms := resolved.Keyterms; len(keyterms) > 0 {
		listenProvider["keyterms"] = keyterms
	}

	agent := map[string]any{
		"listen": map[string]any{"provider": listenProvider},
		"think":  think,
		"speak": map[string]any{
			"provider": map[string]any{
				"type":  "deepgram",
				"model": p.resolveSpeakModel(LiveConfig{Locale: resolved.Locale, Voice: resolved.Voice}),
			},
		},
	}
	if lang := deepgramAgentLanguage(resolved.Locale); lang != "" {
		agent["language"] = lang
	}

	return map[string]any{
		"type": "Settings",
		"audio": map[string]any{
			"input": map[string]any{
				"encoding":    "linear16",
				"sample_rate": deepgramAgentInputSampleRate,
			},
			"output": map[string]any{
				"encoding":    "linear16",
				"sample_rate": deepgramAgentOutputSampleRate,
				"container":   "none",
			},
		},
		"agent": agent,
	}
}

// thinkEndpoint returns the agent.think.endpoint block for a bring-your-own
// think LLM, or nil to use Deepgram's managed model. Per the Deepgram Voice
// Agent Settings schema, a BYO credential travels as an Authorization header on
// the endpoint — the provider object itself carries no key.
func (p *DeepgramLive) thinkEndpoint() map[string]any {
	url := strings.TrimSpace(p.ThinkEndpointURL)
	if url == "" {
		return nil
	}
	endpoint := map[string]any{"url": url}
	if key := strings.TrimSpace(p.ThinkAPIKey); key != "" {
		endpoint["headers"] = map[string]any{"authorization": "Bearer " + key}
	}
	return endpoint
}

func (p *DeepgramLive) resolveSpeakModel(cfg LiveConfig) string {
	if v := strings.TrimSpace(cfg.Voice); strings.HasPrefix(strings.ToLower(v), "aura") {
		return v
	}
	if p.SpeakModel != "" {
		return p.SpeakModel
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(cfg.Locale)), "de") {
		return deepgramSpeakModelDefaultDE
	}
	return deepgramSpeakModelDefaultEN
}

func (p *DeepgramLive) startKeepAlive() {
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

// parseEvent translates one Deepgram Voice Agent server event into the kernel's
// LiveMessage shape. Returns (nil, true, nil) when the event should be swallowed.
func (p *DeepgramLive) parseEvent(data []byte) (*LiveMessage, bool, error) {
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
		return &LiveMessage{Interrupted: true}, false, nil
	case "ConversationText":
		var ev struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		_ = json.Unmarshal(data, &ev)
		if strings.EqualFold(ev.Role, "user") {
			return &LiveMessage{InputTranscript: ev.Content, InputTranscriptDone: true}, false, nil
		}
		return &LiveMessage{Text: ev.Content, OutputTranscript: ev.Content, OutputTranscriptDone: true}, false, nil
	case "AgentAudioDone":
		// End of the agent's spoken turn.
		return &LiveMessage{Done: true}, false, nil
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
		return &LiveMessage{ToolCalls: []ToolCall{{
			ID:   ev.FunctionCallID,
			Name: ev.FunctionName,
			Args: args,
		}}}, false, nil
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

// composeDeepgramPrompt merges the framework + refinement prompts into the
// single system prompt Deepgram's think leg accepts (agent.think.prompt).
func composeDeepgramPrompt(cfg LiveConfig) string {
	prompt := strings.TrimSpace(cfg.FrameworkPrompt)
	if refinement := strings.TrimSpace(cfg.RefinementPrompt); refinement != "" {
		if prompt == "" {
			prompt = refinement
		} else {
			prompt = prompt + "\n\n" + refinement
		}
	}
	return prompt
}

// deepgramAgentLanguage reduces a SpeechKit locale ("de-DE", "en-US") to the
// two-letter language code Deepgram's agent.language expects. "auto"/empty
// returns "" so the field is omitted.
func deepgramAgentLanguage(locale string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "" || locale == "auto" {
		return ""
	}
	if idx := strings.IndexAny(locale, "-_"); idx > 0 {
		return locale[:idx]
	}
	return locale
}

// buildDeepgramFunctions translates kernel ToolDefinitions into the
// agent.think.functions array.
func buildDeepgramFunctions(defs []ToolDefinition) []map[string]any {
	if len(defs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(defs))
	for _, def := range defs {
		entry := map[string]any{
			"name":        def.Name,
			"description": def.Description,
		}
		if def.ParametersJSONSchema != nil {
			entry["parameters"] = def.ParametersJSONSchema
		}
		out = append(out, entry)
	}
	return out
}

func dgFirst(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// splitKeyterms turns a VocabularyHint blob into individual Nova-3 keyterms,
// splitting on newlines, commas, and semicolons and dropping blanks.
func splitKeyterms(hint string) []string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return nil
	}
	fields := strings.FieldsFunc(hint, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if term := strings.TrimSpace(f); term != "" {
			out = append(out, term)
		}
	}
	return out
}

// Compile-time assertions that DeepgramLive satisfies the kernel interfaces.
var (
	_ LiveProvider           = (*DeepgramLive)(nil)
	_ LiveInstructionUpdater = (*DeepgramLive)(nil)
)
