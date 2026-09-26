// Package deepgram adapts the Deepgram Voice Agent API
// (wss://agent.deepgram.com/v1/agent/converse) to [live.LiveProvider]: one
// WebSocket carries listen (Flux or Nova STT), think (a Deepgram-managed or
// bring-your-own LLM) and speak (Aura-2 or Flux TTS). It needs a Deepgram
// API key in the [live.LiveConfig].
package deepgram

import (
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// Deepgram Voice Agent API constants. The Agent WebSocket carries the full
// audio-to-audio loop (Flux listen → configurable think LLM → Aura-2 speak)
// over a single connection. SpeechKit's mic emits 16 kHz PCM16, which Deepgram
// accepts directly on the input — no upsample needed. Output is requested at
// 24 kHz to match the kernel's live.LiveMessage.Audio contract.
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
	deepgramAgentOutputSampleRate = 24000 // matches live.LiveMessage.Audio 24 kHz contract.

	deepgramAgentKeepAlive    = 8 * time.Second
	deepgramAgentReadLimit    = 4 << 20
	deepgramAgentWriteTimeout = 15 * time.Second
)

// Provider implements live.LiveProvider against the Deepgram Voice Agent API
// (WebSocket). It mirrors the shared provider surface so callers don't need
// to know which backend is active.
//
// The think (LLM) leg is configurable: Deepgram drives the LLM server-side, so
// ThinkProvider/ThinkModel select which model reasons over the transcript.
// Defaults target a widely-available option; the wiring layer overrides them
// from deployment config. Listen defaults to Deepgram Flux for turn-aware
// conversational STT and speak defaults to a Deepgram Aura-2 voice.
type Provider struct {
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
	lastConfig *live.LiveConfig

	closeMu  sync.Mutex
	closed   bool
	closeErr error

	keepAliveStop chan struct{}
}

// New returns a fresh Deepgram Voice Agent provider.
func New() *Provider { return &Provider{} }

// ConfigureThink applies the deployment's think-LLM selection to the provider.
// Non-empty provider/model override the package defaults; empty values keep the
// Deepgram-managed default. endpointURL/apiKey select a bring-your-own think LLM
// (see ThinkEndpointURL/ThinkAPIKey) and are cleared when empty. Both Targets
// call this from their Voice Agent wiring with values resolved from config.
func (p *Provider) ConfigureThink(provider, model, endpointURL, apiKey string) {
	if v := strings.TrimSpace(provider); v != "" {
		p.ThinkProvider = v
	}
	if v := strings.TrimSpace(model); v != "" {
		p.ThinkModel = v
	}
	p.ThinkEndpointURL = strings.TrimSpace(endpointURL)
	p.ThinkAPIKey = strings.TrimSpace(apiKey)
}

// AudioSettings carries the deployment's listen/speak leg selection for
// ConfigureAudio. Zero values keep the kernel defaults, so a caller can set only
// the fields its config actually specifies.
type AudioSettings struct {
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
func (p *Provider) ConfigureAudio(s AudioSettings) {
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
func (p *Provider) Name() string { return "deepgram-agent" }

// SessionCapabilities reports the profile, default model and capability
// flags of the Deepgram catalog descriptor.
func (p *Provider) SessionCapabilities() live.SessionCapabilities {
	return live.SessionCapabilitiesForProvider("deepgram")
}

// Compile-time assertions that Provider satisfies the kernel interfaces.
var (
	_ live.LiveProvider           = (*Provider)(nil)
	_ live.LiveInstructionUpdater = (*Provider)(nil)
)
