package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Deepgram Voice Agent API constants. The Agent WebSocket carries the full
// audio-to-audio loop (Flux listen → configurable think LLM → Aura-2 speak)
// over a single connection. SpeechKit's mic emits 16 kHz PCM16, which Deepgram
// accepts directly on the input — no upsample needed. Output is requested at
// 24 kHz to match the kernel's LiveMessage.Audio contract.
const (
	deepgramAgentURL = "wss://agent.deepgram.com/v1/agent/converse"

	deepgramListenModelDefault   = "flux-general-multi"
	deepgramSpeakModelDefaultEN  = "aura-2-thalia-en"
	deepgramSpeakModelDefaultDE  = "aura-2-viktoria-de"
	deepgramSpeakModelFluxEN     = "flux-kit-en"
	deepgramThinkProviderDefault = "open_ai"
	deepgramThinkModelDefault    = "gpt-4o-mini"

	// Flux listen tuning ranges from the Voice Agent Settings schema. Values
	// outside these bounds are rejected by Deepgram, so a misconfigured
	// deployment must not be able to fail the handshake.
	deepgramEOTThresholdMin      = 0.5
	deepgramEOTThresholdMax      = 0.9
	deepgramEagerEOTThresholdMin = 0.3
	deepgramEagerEOTThresholdMax = 0.9
	deepgramEOTTimeoutMinMs      = 500
	deepgramEOTTimeoutMaxMs      = 60000

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
// from deployment config. Listen defaults to Deepgram Flux for turn-aware
// conversational STT and speak defaults to a Deepgram Aura-2 voice.
type DeepgramLive struct {
	// Optional overrides; zero values fall back to the package defaults.
	ListenModel   string
	SpeakModel    string
	ThinkProvider string
	ThinkModel    string

	// Flux listen turn-detection tuning. Zero leaves Deepgram's defaults in
	// place; non-zero values are clamped to the documented ranges and are only
	// sent when the listen model is a Flux model (Nova rejects them).
	EOTThreshold      float64
	EagerEOTThreshold float64
	EOTTimeoutMs      int

	// SpeakSpeed adjusts delivery pace on the speak leg. Zero keeps the
	// provider default.
	SpeakSpeed float64

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

// DeepgramAudioSettings carries the deployment's listen/speak leg selection for
// ConfigureAudio. Zero values keep the kernel defaults, so a caller can set only
// the fields its config actually specifies.
type DeepgramAudioSettings struct {
	// ListenModel names the STT model (e.g. "flux-general-multi", "nova-3").
	ListenModel string
	// SpeakModel names the TTS voice. An "aura-*" voice uses the v1 speak leg;
	// a "flux-*" voice uses the v2 (Flux TTS) leg and is only honoured for
	// English-pinned sessions — see resolveSpeakModel.
	SpeakModel string
	// SpeakSpeed sets the delivery pace (Flux accepts 0.85–1.15 in 0.05 steps).
	SpeakSpeed float64
	// EOTThreshold, EagerEOTThreshold, and EOTTimeoutMs tune Flux's
	// model-integrated end-of-turn detection.
	EOTThreshold      float64
	EagerEOTThreshold float64
	EOTTimeoutMs      int
}

// ConfigureAudio applies the deployment's listen/speak selection and Flux
// turn-detection tuning to the provider. Empty/zero fields keep the kernel
// defaults. Both Targets call this from their Voice Agent wiring alongside
// ConfigureThink.
func (p *DeepgramLive) ConfigureAudio(s DeepgramAudioSettings) {
	if v := strings.TrimSpace(s.ListenModel); v != "" {
		p.ListenModel = v
	}
	if v := strings.TrimSpace(s.SpeakModel); v != "" {
		p.SpeakModel = v
	}
	if s.SpeakSpeed > 0 {
		p.SpeakSpeed = s.SpeakSpeed
	}
	if s.EOTThreshold > 0 {
		p.EOTThreshold = s.EOTThreshold
	}
	if s.EagerEOTThreshold > 0 {
		p.EagerEOTThreshold = s.EagerEOTThreshold
	}
	if s.EOTTimeoutMs > 0 {
		p.EOTTimeoutMs = s.EOTTimeoutMs
	}
}

// Name identifies the provider in Voice Agent logs.
func (p *DeepgramLive) Name() string { return "deepgram-agent" }

func (p *DeepgramLive) SessionCapabilities() SessionCapabilities {
	return sessionCapabilitiesForProvider("deepgram")
}

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
			return normalizeLiveMessageEvents(&LiveMessage{
				EventType: LiveEventOutputAudio,
				Audio:     data,
			}, "binary_audio"), nil
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
	if prompt := appendContextPrompt(composeDeepgramPrompt(cfg), resolved.ContextPrompt); prompt != "" {
		think["prompt"] = prompt
	}
	if funcs := buildDeepgramFunctions(cfg.Tools); len(funcs) > 0 {
		think["functions"] = funcs
	}

	listenModel := dgFirst(p.ListenModel, deepgramListenModelDefault)
	listenProvider := map[string]any{
		"type":  "deepgram",
		"model": listenModel,
	}
	// VocabularyHint boosts recognition of domain terms — Nova-3 exposes this
	// as keyterms on the listen provider.
	if keyterms := resolved.Keyterms; len(keyterms) > 0 {
		listenProvider["keyterms"] = keyterms
	}
	hints := deepgramListenLanguageHints(resolved.LanguageHints, resolved.Locale, listenModel)
	if len(hints) > 0 {
		listenProvider["language_hints"] = hints
	}
	if deepgramModelUsesFlux(listenModel) {
		// Flux runs on the Voice Agent API's v2 listen leg; the version field is
		// required for it and invalid for the Nova (v1) models. Turn detection is
		// model-integrated, so the thresholds only exist on this path.
		listenProvider["version"] = "v2"
		if v, ok := dgClamp(p.EOTThreshold, deepgramEOTThresholdMin, deepgramEOTThresholdMax); ok {
			listenProvider["eot_threshold"] = v
		}
		if v, ok := dgClamp(p.EagerEOTThreshold, deepgramEagerEOTThresholdMin, deepgramEagerEOTThresholdMax); ok {
			listenProvider["eager_eot_threshold"] = v
		}
		if v, ok := dgClamp(float64(p.EOTTimeoutMs), deepgramEOTTimeoutMinMs, deepgramEOTTimeoutMaxMs); ok {
			listenProvider["eot_timeout_ms"] = int(v)
		}
	}

	agent := map[string]any{
		"listen": map[string]any{"provider": listenProvider},
		"think":  think,
		"speak":  p.buildSpeak(resolved.Locale, resolved.Voice, hints),
	}
	if lang := deepgramAgentLanguage(resolved.Locale); lang != "" && !deepgramModelUsesFlux(listenModel) {
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

// buildSpeak assembles agent.speak. An Aura voice stays a single provider
// object; a Flux TTS voice travels on the v2 leg and is emitted as the array
// form, with the locale's Aura-2 voice as the second entry so Deepgram falls
// back if the Flux leg is unavailable. Speak is always sent explicitly —
// omitting it makes Deepgram default to Flux TTS server-side, which would
// silently break non-English sessions.
func (p *DeepgramLive) buildSpeak(locale, voice string, hints []string) any {
	speakModel := p.resolveSpeakModel(locale, voice, hints)
	aura := map[string]any{
		"provider": map[string]any{"type": "deepgram", "model": deepgramAuraModelForLocale(locale)},
	}
	if !deepgramModelUsesFlux(speakModel) {
		return map[string]any{
			"provider": map[string]any{"type": "deepgram", "model": speakModel},
		}
	}
	flux := map[string]any{"type": "deepgram", "version": "v2", "model": speakModel}
	if speed := deepgramFluxSpeakSpeed(p.SpeakSpeed); speed > 0 {
		flux["speed"] = speed
	}
	return []any{map[string]any{"provider": flux}, aura}
}

// resolveSpeakModel picks the Deepgram voice for a session. An explicit voice
// wins over the configured SpeakModel, which wins over the locale default.
//
// Flux TTS voices are English-only, while a Flux listen session can code-switch
// mid-call, so a Flux voice is honoured only when the session is English-pinned:
// an English (or unset) locale and no non-English language hint. Anything else
// falls back to the Aura-2 voice for the locale. Deepgram's speak fallback array
// cannot cover this — it fires on provider failure, not on a successful
// synthesis in the wrong language.
func (p *DeepgramLive) resolveSpeakModel(locale, voice string, hints []string) string {
	selected := ""
	if v := strings.TrimSpace(voice); deepgramIsSpeakVoice(v) {
		selected = v
	} else if p.SpeakModel != "" {
		selected = strings.TrimSpace(p.SpeakModel)
	}
	if selected == "" {
		return deepgramAuraModelForLocale(locale)
	}
	if deepgramModelUsesFlux(selected) && !deepgramSessionIsEnglishPinned(locale, hints) {
		fallback := deepgramAuraModelForLocale(locale)
		slog.Warn("deepgram agent: Flux TTS is English-only; using Aura-2 for this session",
			"requested_voice", selected, "locale", locale, "language_hints", hints, "speak_model", fallback)
		return fallback
	}
	return selected
}

// deepgramIsSpeakVoice reports whether a configured voice names a Deepgram TTS
// model (Aura or Flux) rather than another provider's voice id.
func deepgramIsSpeakVoice(voice string) bool {
	lower := strings.ToLower(strings.TrimSpace(voice))
	return strings.HasPrefix(lower, "aura") || strings.HasPrefix(lower, "flux")
}

func deepgramAuraModelForLocale(locale string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "de") {
		return deepgramSpeakModelDefaultDE
	}
	return deepgramSpeakModelDefaultEN
}

// deepgramSessionIsEnglishPinned reports whether every language the session can
// produce is English. An empty locale and empty hints count as pinned: that is
// the framework's English default, not an unknown language.
func deepgramSessionIsEnglishPinned(locale string, hints []string) bool {
	isEnglish := func(value string) bool {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return true
		}
		if value == "auto" || value == "multi" {
			return false
		}
		if idx := strings.IndexAny(value, "-_"); idx > 0 {
			value = value[:idx]
		}
		return value == "en"
	}
	if !isEnglish(locale) {
		return false
	}
	for _, hint := range hints {
		if !isEnglish(hint) {
			return false
		}
	}
	return true
}

// deepgramFluxSpeakSpeed snaps a configured speed onto the discrete steps Flux
// TTS accepts (0.85–1.15 in 0.05 increments). Out-of-range values clamp to the
// nearest bound; 0 means "unset" and returns 0 so the field is omitted.
func deepgramFluxSpeakSpeed(speed float64) float64 {
	if speed <= 0 {
		return 0
	}
	steps := []float64{0.85, 0.9, 0.95, 1.0, 1.05, 1.1, 1.15}
	best := steps[0]
	for _, step := range steps {
		if math.Abs(step-speed) < math.Abs(best-speed) {
			best = step
		}
	}
	return best
}

// dgClamp reports whether an optional numeric tuning value was set (> 0) and
// returns it clamped into the provider's accepted range.
func dgClamp(value, lower, upper float64) (float64, bool) {
	if value <= 0 {
		return 0, false
	}
	if value < lower {
		return lower, true
	}
	if value > upper {
		return upper, true
	}
	return value, true
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
		return normalizeLiveMessageEvents(&LiveMessage{EventType: LiveEventInterrupted, Interrupted: true}, env.Type), false, nil
	case "StartOfTurn", "TurnResumed":
		// Flux turn events signal fresh user activity. Surface them as
		// interruptions so host UIs can stop playback across both Deepgram Voice
		// Agent and lower-level Flux transports without provider-specific code.
		return normalizeLiveMessageEvents(&LiveMessage{EventType: LiveEventInterrupted, Interrupted: true}, env.Type), false, nil
	case "EagerEndOfTurn", "EndOfTurn":
		return normalizeLiveMessageEvents(&LiveMessage{EventType: LiveEventTurnEnd}, env.Type), false, nil
	case "ConversationText":
		var ev struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		_ = json.Unmarshal(data, &ev)
		if strings.EqualFold(ev.Role, "user") {
			return normalizeLiveMessageEvents(&LiveMessage{
				EventType:           LiveEventInputFinal,
				InputTranscript:     ev.Content,
				InputTranscriptDone: true,
			}, env.Type), false, nil
		}
		return normalizeLiveMessageEvents(&LiveMessage{
			EventType:            LiveEventOutputText,
			Text:                 ev.Content,
			OutputTranscript:     ev.Content,
			OutputTranscriptDone: true,
		}, env.Type), false, nil
	case "AgentAudioDone":
		// End of the agent's spoken turn.
		return normalizeLiveMessageEvents(&LiveMessage{EventType: LiveEventTurnEnd, Done: true}, env.Type), false, nil
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
		return normalizeLiveMessageEvents(&LiveMessage{
			EventType: LiveEventToolCall,
			ToolCalls: []ToolCall{{
				ID:   ev.FunctionCallID,
				Name: ev.FunctionName,
				Args: args,
			}},
		}, env.Type), false, nil
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

// deepgramModelUsesFlux reports whether a listen or speak model names a Flux
// model ("flux-general-multi", "flux-kit-en", …). Both legs move to the
// provider's v2 API when it does.
func deepgramModelUsesFlux(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "flux-")
}

func deepgramListenLanguageHints(hints []string, locale, listenModel string) []string {
	out := make([]string, 0, len(hints)+1)
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || value == "auto" {
			return
		}
		if idx := strings.IndexAny(value, "-_"); idx > 0 {
			value = value[:idx]
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, hint := range hints {
		add(hint)
	}
	if deepgramModelUsesFlux(listenModel) {
		add(locale)
	}
	return out
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

// Compile-time assertions that DeepgramLive satisfies the kernel interfaces.
var (
	_ LiveProvider           = (*DeepgramLive)(nil)
	_ LiveInstructionUpdater = (*DeepgramLive)(nil)
)
