// Package openailive adapts OpenAI's full-duplex Live API (model gpt-live-1,
// WebSocket wss://api.openai.com/v1/live/sessions) to [live.LiveProvider].
//
// GPT-Live is not the Realtime protocol: the client opens the socket without
// query parameters, sends session.start with the model inside the session
// object and waits for session.started. The live model only listens and
// speaks; reasoning and tools are delegated to a backend. SpeechKit maps
// kernel tools onto Responses delegation, so a caller that offers tools must
// also choose the backend model ([Provider.BackendModel]).
//
// Every wire name used here is taken from the official openai-python SDK
// (src/openai/resources/live/live.py, src/openai/types/live/*.py) and the
// Microsoft Learn GPT-Live articles (articles/foundry/openai/how-to/gpt-live.md,
// how-to/gpt-live-delegation.md, gpt-live-reference.md). Microsoft Foundry
// serves the same protocol from wss://<host>/openai/v1/live/sessions with the
// deployment name as the model: [NewFoundry] returns that variant, which
// requires cfg.Endpoint and sends the Foundry resource key or a Microsoft
// Entra token (cfg.BearerToken) as the Bearer token the v1 surface accepts.
//
// Stability: Experimental — may change in any release.
package openailive

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/internal/logutil"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/openai"
)

const (
	// DefaultModel is the Live model sent in session.start when
	// cfg.Model is empty (types/live/session_config.py: model).
	DefaultModel = "gpt-live-1"
	// DefaultEndpoint is the OpenAI Live WebSocket: the SDK's default base
	// URL https://api.openai.com/v1 with the ws scheme and /live/sessions
	// appended (resources/live/live.py: _prepare_url).
	DefaultEndpoint = "wss://api.openai.com/v1/live/sessions"
	// ProfileID names this provider's sessions in [live.SessionCapabilities].
	ProfileID = "realtime.openai.gpt-live-1"
	// FoundryProfileID names the sessions of the [NewFoundry] variant.
	FoundryProfileID = "realtime.foundry.gpt-live-1"

	providerID        = "gpt-live"
	foundryProviderID = "foundry-gpt-live"

	// sampleRate is the PCM rate requested with audio.format. Live WebSockets
	// accept 16000 or 24000 (types/live/audio_format.py); 24 kHz is the rate
	// the Foundry reference documents for both directions and the official
	// examples/live/audio_transcript.py requests, and it matches the
	// kernel's 24 kHz LiveMessage.Audio contract, so output needs no resample.
	sampleRate = 24000

	startTimeout      = 15 * time.Second
	closeWriteTimeout = 2 * time.Second
)

// ErrBackendModelRequired is returned by Connect when the host offers tools
// but no Responses backend model: GPT-Live runs tools only in its delegated
// backend, never in the live voice model.
var ErrBackendModelRequired = errors.New("openai live: BackendModel is required when tools are configured")

// BuiltInVoices are the voice names the Live API accepts in
// audio.output.voice (types/live/built_in_voice.py). The server default is
// marin.
var BuiltInVoices = []string{
	"alloy", "ash", "ballad", "beacon", "bossa", "cedar", "cinder", "coral",
	"delta", "echo", "gleam", "marin", "meridian", "quartz", "ripple", "sage",
	"shimmer", "stone", "tempo", "verse", "vesper", "willow",
}

// Provider implements live.LiveProvider against the GPT-Live WebSocket API.
// Set the exported fields before Connect.
type Provider struct {
	// BackendModel is the Responses model GPT-Live delegates reasoning and
	// tools to (session.delegation = {type: "responses", responses: {model}}).
	// Empty keeps the server default, client delegation, which is only valid
	// without tools.
	BackendModel string
	// BackendInstructions is the separate backend prompt
	// (delegation.responses.instructions). The live model receives the
	// kernel's assembled prompt as session.instructions.
	BackendInstructions string
	// DialURL maps the resolved endpoint (cfg.Endpoint or DefaultEndpoint)
	// to the URL to dial. nil dials it unchanged; the model never goes into
	// the URL (types/live/session_config.py: model).
	DialURL func(endpoint string) string
	// DialHeaders builds the handshake headers. nil sends
	// "Authorization: Bearer" with the cfg.BearerToken token when set,
	// otherwise cfg.APIKey, which both OpenAI and the Foundry v1 surface
	// accept. A host that needs another auth scheme sets it.
	DialHeaders func(ctx context.Context, cfg live.LiveConfig) (http.Header, error)
	// Logger receives diagnostics; never audio or transcript text. Nil falls
	// back to slog.Default() at log time.
	Logger *slog.Logger

	mu   sync.RWMutex
	conn *websocket.Conn
	// Session-timeline barge-in tracking (see userSpokeOverOutput).
	outputEndMs int64
	bargeInAtMs int64
	bargedIn    bool
	model       string
	sessionID   string

	// pending maps an unanswered function call_id to its delegation_id so
	// SendToolResponse continues the backend only once every call of that
	// delegation has a result (how-to/gpt-live-delegation.md).
	toolMu  sync.Mutex
	pending map[string]string

	closeMu sync.Mutex
	closed  bool

	// foundry marks the Microsoft Foundry variant ([NewFoundry]).
	foundry bool
}

// New returns a GPT-Live provider for OpenAI.
func New() *Provider { return &Provider{} }

// NewFoundry returns a GPT-Live provider for a Microsoft Foundry deployment
// (provider "foundry-gpt-live"). cfg.Endpoint must name the resource's
// wss://<host>/openai/v1/live/sessions URL and cfg.Model the deployment;
// BackendModel is a Foundry Responses deployment.
func NewFoundry() *Provider { return &Provider{foundry: true} }

// Name identifies the provider in Voice Agent logs.
func (p *Provider) Name() string {
	if p.foundry {
		return foundryProviderID
	}
	return providerID
}

func (p *Provider) profileID() string {
	if p.foundry {
		return FoundryProfileID
	}
	return ProfileID
}

// ContinuousDuplex implements [live.ContinuousDuplexProvider]: the Live
// protocol streams without turn boundaries (no transcript-done or
// output-audio-done event), so the session settles turns on its timer.
func (p *Provider) ContinuousDuplex() bool { return true }

// SessionCapabilities reports the model of the connected session (the
// default before Connect) and the capabilities this adapter maps.
func (p *Provider) SessionCapabilities() live.SessionCapabilities {
	p.mu.RLock()
	model, sessionID := p.model, p.sessionID
	p.mu.RUnlock()
	caps := live.SessionCapabilities{
		Provider:  p.Name(),
		ProfileID: p.profileID(),
		Model:     firstNonEmpty(model, DefaultModel),
		Capabilities: []live.LiveCapabilityFlag{
			live.LiveCapabilityRealtimeAudio,
			live.LiveCapabilityTranscript,
			live.LiveCapabilityToolCalling,
		},
	}
	if sessionID != "" {
		caps.ProviderMetadata = map[string]any{"session_id": sessionID}
	}
	return caps
}

// Compile-time assertions that Provider satisfies the kernel interfaces.
var (
	_ live.LiveProvider            = (*Provider)(nil)
	_ live.LiveSessionCapabilities = (*Provider)(nil)
)

// Connect dials the Live endpoint, sends session.start and blocks until
// session.started (resources/live/live.py: LiveSessionResource.start). A
// startup error event fails Connect with the server's code and message.
func (p *Provider) Connect(ctx context.Context, cfg live.LiveConfig) error {
	// The Foundry variant never falls back to DefaultEndpoint: that would
	// send the Foundry key to api.openai.com.
	if p.foundry && strings.TrimSpace(cfg.Endpoint) == "" {
		return fmt.Errorf("foundry gpt-live: %w (derive it from the Foundry project endpoint, e.g. wss://<account-host>/openai/v1/live/sessions)", live.ErrMissingEndpoint)
	}
	if strings.TrimSpace(cfg.APIKey) == "" && cfg.BearerToken == nil {
		return fmt.Errorf("openai live: %w", live.ErrMissingAPIKey)
	}
	if len(cfg.Tools) > 0 && strings.TrimSpace(p.BackendModel) == "" {
		return ErrBackendModelRequired
	}
	header, err := p.dialHeaders(ctx, cfg)
	if err != nil {
		return fmt.Errorf("openai live: %w", err)
	}
	model := firstNonEmpty(cfg.Model, DefaultModel)
	conn, resp, err := websocket.Dial(ctx, p.dialURL(cfg), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return fmt.Errorf("openai live: dial: %w", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	conn.SetReadLimit(4 << 20)

	startCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	sessionID, err := p.start(startCtx, conn, cfg, model)
	if err != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "session.start failed")
		return fmt.Errorf("openai live: %w", err)
	}

	p.closeMu.Lock()
	p.mu.Lock()
	p.conn, p.model, p.sessionID = conn, model, sessionID
	p.mu.Unlock()
	p.closed = false
	p.closeMu.Unlock()
	p.toolMu.Lock()
	p.pending = map[string]string{}
	p.toolMu.Unlock()
	return nil
}

// start sends session.start and reads until session.started or error.
func (p *Provider) start(ctx context.Context, conn *websocket.Conn, cfg live.LiveConfig, model string) (string, error) {
	frame := map[string]any{"type": "session.start", "session": p.buildSession(cfg, model)}
	if err := writeJSON(ctx, conn, frame); err != nil {
		return "", fmt.Errorf("write session.start: %w", err)
	}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return "", fmt.Errorf("await session.started: %w", err)
		}
		var ev struct {
			Type    string `json:"type"`
			Session struct {
				ID string `json:"id"`
			} `json:"session"`
		}
		if err := decode(data, &ev); err != nil {
			return "", err
		}
		switch ev.Type {
		case "session.started":
			return ev.Session.ID, nil
		case "error":
			return "", serverError(data)
		}
	}
}

// buildSession builds the session.start configuration
// (types/live/session_config.py, how-to/gpt-live.md "Session configuration").
// The object is strict server-side, so only documented fields are sent.
func (p *Provider) buildSession(cfg live.LiveConfig, model string) map[string]any {
	// Both variants speak the same session vocabulary, so options resolve
	// against the one gpt-live manifest.
	resolved := live.ResolveLiveOptions(providerID, p.profileID(), cfg, nil, nil)
	output := map[string]any{}
	if voice := strings.ToLower(firstNonEmpty(resolved.Voice, cfg.Voice)); slices.Contains(BuiltInVoices, voice) {
		output["voice"] = voice
	} else if voice != "" {
		p.log().Debug("openai live: voice is not a built-in Live voice; using the server default", "voice", voice)
	}
	audio := map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": sampleRate}}
	if len(output) > 0 {
		audio["output"] = output
	}
	session := map[string]any{"model": model, "audio": audio}
	if instructions := assembleInstructions(cfg, resolved); instructions != "" {
		session["instructions"] = instructions
	}
	if backend := strings.TrimSpace(p.BackendModel); backend != "" {
		responses := map[string]any{"model": backend}
		if text := strings.TrimSpace(p.BackendInstructions); text != "" {
			responses["instructions"] = text
		}
		// Live function tools share the Realtime function schema
		// (types/live/function_tool.py: type, name, description, parameters).
		if tools := openai.BuildTools(cfg.Tools); len(tools) > 0 {
			responses["tools"] = tools
			responses["tool_choice"] = "auto"
		}
		if effort := strings.TrimSpace(resolved.ReasoningEffort); effort != "" {
			responses["reasoning"] = map[string]any{"effort": effort}
		}
		session["delegation"] = map[string]any{"type": "responses", "responses": responses}
	}
	return session
}

func assembleInstructions(cfg live.LiveConfig, resolved live.ResolvedLiveOptions) string {
	instructions := strings.TrimSpace(cfg.FrameworkPrompt)
	if refinement := strings.TrimSpace(cfg.RefinementPrompt); refinement != "" {
		if instructions == "" {
			instructions = refinement
		} else {
			instructions += "\n\n" + refinement
		}
	}
	return live.AppendContextPrompt(instructions, resolved.ContextPrompt)
}

func (p *Provider) dialURL(cfg live.LiveConfig) string {
	endpoint := firstNonEmpty(cfg.Endpoint, DefaultEndpoint)
	if p.DialURL != nil {
		return p.DialURL(endpoint)
	}
	return endpoint
}

// dialHeaders sends the SDK's bearer auth (resources/live/live.py:
// security={"bearer_auth": True}); a host token source wins over the key.
func (p *Provider) dialHeaders(ctx context.Context, cfg live.LiveConfig) (http.Header, error) {
	if p.DialHeaders != nil {
		return p.DialHeaders(ctx, cfg)
	}
	token := cfg.APIKey
	if cfg.BearerToken != nil {
		var err error
		if token, err = cfg.BearerToken(ctx); err != nil {
			return nil, fmt.Errorf("bearer token: %w", err)
		}
		if strings.TrimSpace(token) == "" {
			return nil, errors.New("bearer token: empty token")
		}
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	return header, nil
}

func (p *Provider) log() *slog.Logger { return logutil.Resolve(p.Logger) }

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
