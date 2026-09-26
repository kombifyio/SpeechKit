// Package openai adapts the OpenAI Realtime WebSocket API
// (wss://api.openai.com/v1/realtime, GA session shape) to
// [live.LiveProvider]. It needs an API key or a bearer-token source in the
// [live.LiveConfig]; dial URL, handshake headers and session shape are
// pluggable so the Foundry and Voice Live packages reuse the same loop.
package openai

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
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
// (WebSocket). It mirrors the shared provider surface so callers don't need to
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

// SessionCapabilities reports the profile, default model and capability
// flags of the OpenAI catalog descriptor.
func (p *Provider) SessionCapabilities() live.SessionCapabilities {
	return live.SessionCapabilitiesForProvider("openai")
}

// Compile-time assertions that Provider satisfies the kernel interfaces.
var (
	_ live.LiveProvider           = (*Provider)(nil)
	_ live.LiveInstructionUpdater = (*Provider)(nil)
)
