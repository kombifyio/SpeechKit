package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// defaultOpenAIRealtimeModel is the kernel-level default for the OpenAI
// Realtime provider when live.LiveConfig.Model is empty. gpt-realtime-2 requires the
// GA Realtime WebSocket API, so the provider must not send the legacy beta
// routing header.
const defaultOpenAIRealtimeModel = "gpt-realtime-2"

// DefaultRealtimeModel is the public runtime default for OpenAI-backed
// Voice Agent sessions.
const DefaultRealtimeModel = defaultOpenAIRealtimeModel

// OpenAI Realtime API constants. The API expects 24 kHz, 16-bit signed,
// little-endian PCM mono on the input audio buffer; SpeechKit's mic capture
// emits 16 kHz, so the provider performs the 2:3 upsample inline. Output
// PCM from the server is also 24 kHz, which matches the kernel's
// live.LiveMessage.Audio contract — no resample on the way back out.
const (
	openaiRealtimeBaseURL = "wss://api.openai.com/v1/realtime"

	openaiInputSampleRate = 24000 // OpenAI Realtime expects 24 kHz PCM input

	openaiSessionUpdateTimeout = 15 * time.Second
)

// Provider implements live.LiveProvider against the OpenAI Realtime API
// (WebSocket). It mirrors GeminiLive's surface so callers don't need to
// know which backend is active.
type Provider struct {
	// DialURL builds the WebSocket URL from the resolved base URL and the
	// model (or deployment) to address. nil dials the OpenAI Realtime form
	// base?model=<model>. Providers that serve this wire protocol from
	// another host with extra query parameters (Foundry Voice Live) set it.
	DialURL func(baseURL, model string) string
	// DialHeaders builds the handshake headers for cfg. nil sends
	// "Authorization: Bearer" carrying the token from cfg.BearerToken when
	// the host set one, otherwise cfg.APIKey. Providers with a different
	// credential header set it; ctx bounds token acquisition.
	DialHeaders func(ctx context.Context, cfg live.LiveConfig) (http.Header, error)
	// BuildSession builds the session object sent in session.update. model
	// is the resolved model and instructions the assembled host prompt. nil
	// builds the GA OpenAI Realtime session shape.
	BuildSession func(cfg live.LiveConfig, model, instructions string) map[string]any

	mu         sync.RWMutex
	conn       *websocket.Conn
	lastConfig *live.LiveConfig

	closeMu  sync.Mutex
	closed   bool
	closeErr error

	// sessionReady is closed once the server has acknowledged the initial
	// session.update with a session.updated event. Receive() can run before
	// session.updated arrives because the server emits session.created
	// immediately after dial; sequencing the first Send call after this
	// channel keeps configuration deterministic.
	sessionReady   chan struct{}
	sessionReadyMu sync.Mutex
}

// New returns a fresh OpenAI Realtime provider.
func New() *Provider {
	return &Provider{}
}

// Name identifies the provider in Voice Agent logs.
func (p *Provider) Name() string { return "openai-realtime" }

func (p *Provider) SessionCapabilities() live.SessionCapabilities {
	return live.SessionCapabilitiesForProvider("openai")
}

// Connect dials the OpenAI Realtime WebSocket, sends the configured
// instructions/voice/tools as a session.update, and waits for the
// session.updated acknowledgement before returning.
func (p *Provider) Connect(ctx context.Context, cfg live.LiveConfig) error {
	if strings.TrimSpace(cfg.APIKey) == "" && cfg.BearerToken == nil {
		return fmt.Errorf("openai realtime: %w", live.ErrMissingAPIKey)
	}
	model := resolveOpenAIRealtimeModel(cfg.Model)
	header, err := p.dialHeaders(ctx, cfg)
	if err != nil {
		return fmt.Errorf("openai realtime: %w", err)
	}
	baseURL := realtimeBaseURL(cfg)

	dialURL := p.dialURL(baseURL, model)
	conn, dialResp, err := websocket.Dial(ctx, dialURL, &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		// Same-provider fallback: retry with FallbackModel when distinct.
		if fallback := strings.TrimSpace(cfg.FallbackModel); live.ShouldTryFallback(model, fallback) {
			fallbackURL := p.dialURL(baseURL, fallback)
			conn, dialResp, err = websocket.Dial(ctx, fallbackURL, &websocket.DialOptions{
				HTTPHeader: header,
			})
			if err != nil {
				return fmt.Errorf("openai realtime: dial primary %q + fallback %q failed: %w", model, fallback, err)
			}
			slog.Info("openai realtime: connected via fallback model", "primary", model, "fallback", fallback)
			model = fallback
		} else {
			return fmt.Errorf("openai realtime: dial %q: %w", model, err)
		}
	}
	if dialResp != nil && dialResp.Body != nil {
		_ = dialResp.Body.Close()
	}
	// PCM frames base64-encoded can grow large; OpenAI documents up to ~16 MB
	// per message but we cap at 4 MB to surface runaway-buffer bugs early.
	conn.SetReadLimit(4 << 20)

	cfgCopy := cfg
	cfgCopy.Model = model

	p.mu.Lock()
	p.conn = conn
	p.lastConfig = &cfgCopy
	p.closed = false
	p.closeErr = nil
	p.mu.Unlock()

	p.resetSessionReady()

	if err := p.sendSessionUpdate(ctx, cfgCopy); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "session.update failed")
		return fmt.Errorf("openai realtime: session.update: %w", err)
	}
	return nil
}

func resolveOpenAIRealtimeModel(model string) string {
	if trimmed := strings.TrimSpace(model); trimmed != "" {
		return trimmed
	}
	return defaultOpenAIRealtimeModel
}

// realtimeBaseURL honours the LiveConfig.Endpoint override so OpenAI-protocol
// hosts (e.g. Microsoft Foundry's wss://<host>/openai/v1/realtime) can reuse
// this provider unchanged.
func realtimeBaseURL(cfg live.LiveConfig) string {
	if endpoint := strings.TrimSpace(cfg.Endpoint); endpoint != "" {
		return endpoint
	}
	return openaiRealtimeBaseURL
}

func openAIRealtimeHeaders(apiKey string) http.Header {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+apiKey)
	return header
}

func (p *Provider) dialURL(baseURL, model string) string {
	if p.DialURL != nil {
		return p.DialURL(baseURL, model)
	}
	return fmt.Sprintf("%s?model=%s", baseURL, model)
}

// dialHeaders prefers a host-supplied bearer token source over the static
// API key so OpenAI-protocol hosts with Entra auth (Foundry) work unchanged.
func (p *Provider) dialHeaders(ctx context.Context, cfg live.LiveConfig) (http.Header, error) {
	if p.DialHeaders != nil {
		return p.DialHeaders(ctx, cfg)
	}
	if cfg.BearerToken != nil {
		token, err := cfg.BearerToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("bearer token: %w", err)
		}
		if strings.TrimSpace(token) == "" {
			return nil, errors.New("bearer token: empty token")
		}
		return openAIRealtimeHeaders(token), nil
	}
	return openAIRealtimeHeaders(cfg.APIKey), nil
}

func (p *Provider) buildSession(cfg live.LiveConfig, model, instructions string) map[string]any {
	if p.BuildSession != nil {
		return p.BuildSession(cfg, model, instructions)
	}
	cfg.Model = model
	session := buildOpenAISession(cfg)
	session["instructions"] = instructions
	return session
}

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
// response. With server VAD enabled OpenAI commits the buffer automatically
// at end-of-speech; this explicit commit covers push-to-talk style turns
// where the kernel decides when the user stopped speaking.
func (p *Provider) SendAudioStreamEnd() error {
	conn := p.snapshotConn()
	if conn == nil {
		return fmt.Errorf("openai realtime: %w", live.ErrNotConnected)
	}
	if err := p.sendJSON(conn, map[string]any{"type": "input_audio_buffer.commit"}); err != nil {
		return err
	}
	return p.sendJSON(conn, map[string]any{"type": "response.create"})
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

func buildOpenAISession(cfg live.LiveConfig) map[string]any {
	resolved := live.ResolveLiveOptions("openai", "realtime.openai.gpt-realtime-2", cfg, nil, nil)
	activity := ResolveActivityDetection(cfg.Policies.ActivityDetection, resolved)
	turnDetection := buildOpenAITurnDetection(activity)
	inputAudio := map[string]any{
		"format": map[string]any{
			"type": "audio/pcm",
			"rate": openaiInputSampleRate,
		},
	}
	if turnDetection != nil {
		inputAudio["turn_detection"] = turnDetection
	}
	if cfg.Policies.EnableInputAudioTranscription {
		inputAudio["transcription"] = map[string]any{
			"model": "whisper-1",
		}
	}
	session := map[string]any{
		"type":              "realtime",
		"model":             resolveOpenAIRealtimeModel(cfg.Model),
		"output_modalities": []string{"audio"},
		"audio": map[string]any{
			"input": inputAudio,
			"output": map[string]any{
				"format": map[string]any{
					"type": "audio/pcm",
					"rate": openaiInputSampleRate,
				},
				"voice": firstNonEmptyOpenAIVoice(resolved.Voice),
			},
		},
	}
	if tools := buildOpenAITools(cfg.Tools); len(tools) > 0 {
		session["tools"] = tools
		session["tool_choice"] = "auto"
	}
	if effort := strings.TrimSpace(resolved.ReasoningEffort); effort != "" {
		session["reasoning"] = map[string]any{"effort": effort}
	}
	return session
}

// ResolveActivityDetection applies the resolved endpointing and
// turn-detection option overrides to the kernel's activity policy: an
// endpointing override turns automatic detection on with that silence
// duration, and an explicit turn_detection=false turns it off. Providers
// that share the Realtime turn_detection contract reuse it.
func ResolveActivityDetection(policy live.ActivityDetectionPolicy, resolved live.ResolvedLiveOptions) live.ActivityDetectionPolicy {
	if resolved.HasEndpointingOverride() && resolved.EndpointingMs > 0 {
		policy.Automatic = true
		policy.SilenceDurationMs = endpointingMsToInt32(resolved.EndpointingMs)
	}
	if resolved.HasTurnDetectionOverride() && !resolved.TurnDetection {
		policy.Automatic = false
	}
	return policy
}

// BuildTurnDetection translates the kernel's activity policy into the
// Realtime API's server_vad turn_detection object. nil means push-to-talk:
// the kernel commits the audio buffer itself.
func BuildTurnDetection(policy live.ActivityDetectionPolicy) map[string]any {
	return buildOpenAITurnDetection(policy)
}

// BuildTools translates kernel tool definitions into Realtime session tool
// entries. Foundry Voice Live shares the function schema and reuses it.
func BuildTools(defs []live.ToolDefinition) []map[string]any {
	return buildOpenAITools(defs)
}

func endpointingMsToInt32(value int) int32 {
	if value <= 0 {
		return 0
	}
	const maxInt32 = int(^uint32(0) >> 1)
	if value > maxInt32 {
		return int32(maxInt32)
	}
	return int32(value) // #nosec G115 -- value is clamped to int32 range above.
}

// firstNonEmptyOpenAIVoice maps the kernel's voice name to a value the
// Realtime API accepts. OpenAI exposes a fixed set of named voices. Unknown
// SpeechKit/Gemini voice names intentionally fall back to alloy so switching
// provider=OpenAI does not turn a valid server config into a failed session.
func firstNonEmptyOpenAIVoice(voice string) string {
	v := strings.ToLower(strings.TrimSpace(voice))
	if v == "" {
		return "alloy"
	}
	switch v {
	case "alloy", "ash", "ballad", "cedar", "coral", "echo", "marin", "sage", "shimmer", "verse":
		return v
	default:
		return "alloy"
	}
}

// buildOpenAITurnDetection translates the kernel's live.ActivityDetectionPolicy
// into the Realtime API's `turn_detection` object. Returning nil disables
// server-side VAD entirely (push-to-talk mode where the kernel commits the
// audio buffer manually).
func buildOpenAITurnDetection(policy live.ActivityDetectionPolicy) map[string]any {
	if !policy.Automatic {
		return nil
	}
	td := map[string]any{
		"type":               "server_vad",
		"create_response":    true,
		"interrupt_response": true,
	}
	if policy.SilenceDurationMs > 0 {
		td["silence_duration_ms"] = int(policy.SilenceDurationMs)
	}
	if policy.PrefixPaddingMs > 0 {
		td["prefix_padding_ms"] = int(policy.PrefixPaddingMs)
	}
	// OpenAI's VAD threshold runs 0.0–1.0. We map kernel sensitivity:
	// low = trip late (high threshold), high = trip early (low threshold).
	switch strings.ToLower(string(policy.StartSensitivity)) {
	case "low":
		td["threshold"] = 0.7
	case "high":
		td["threshold"] = 0.3
	case "medium":
		td["threshold"] = 0.5
	}
	return td
}

// buildOpenAITools translates kernel ToolDefinitions into Realtime
// session.update tool entries.
func buildOpenAITools(defs []live.ToolDefinition) []map[string]any {
	if len(defs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(defs))
	for _, def := range defs {
		entry := map[string]any{
			"type":        "function",
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

// live.ShouldTryFallback (defined in live_gemini.go) is reused here intentionally —
// the same "primary != fallback, both non-empty" rule applies for OpenAI.
// No re-declaration; relying on package-level visibility.

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

// Compile-time assertions that Provider satisfies the kernel interfaces.
var (
	_ live.LiveProvider           = (*Provider)(nil)
	_ live.LiveInstructionUpdater = (*Provider)(nil)
)
