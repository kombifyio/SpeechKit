//go:build linux

package voiceagent

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/wssession"
	"github.com/kombifyio/SpeechKit/internal/store"
)

// defaultWSReadLimitBytes is the per-frame size cap when HandlerOptions.ReadLimit
// is unset or non-positive. Tightened from 1 MiB to 64 KiB in audit S-12 because
// raw PCM chunks are well under 4 KB in practice; 64 KiB leaves ample headroom
// without giving attackers a 1 MB memory amplification vector per frame.
const defaultWSReadLimitBytes int64 = 64 * 1024

// Origin/ticket plumbing is shared with the streaming-Dictation surface via
// internal/server/wssession; the aliases keep this package's names (and their
// tests) stable.
const (
	envAllowEmptyWSOriginVar  = wssession.EnvAllowEmptyOrigin
	envAllowWildcardOriginVar = wssession.EnvAllowWildcardOrigin
	wsTicketSubprotocolPrefix = wssession.TicketSubprotocolPrefix
)

// HandlerOptions configures the WebSocket handler.
type HandlerOptions struct {
	Manager *SessionManager
	// Provider is the single-provider form (back-compat): when Providers is
	// empty this factory becomes the sole, default backend.
	Provider ProviderFactory
	// Providers is the multi-provider form: a name→factory map (e.g.
	// "deepgram", "assemblyai", "cascaded") that the client selects between
	// per session via StartFrame.Provider. DefaultProvider names the entry
	// used when a session omits an explicit provider.
	Providers           map[string]ProviderFactory
	DefaultProvider     string
	Persona             PersonaResolver
	PublicURL           string
	AllowedOrigins      []string
	TrustedProxyCIDRs   []string
	MaxAllowedClockSkew time.Duration
	// IdleTimeout terminates a session that hasn't seen any activity
	// (client frame OR provider message) within the duration. Zero
	// disables the server-side idle watchdog. Defaults to 15 minutes
	// when zero is passed; pass a negative value to disable explicitly.
	IdleTimeout time.Duration
	// MaxSessionDuration terminates a session after this wall-clock duration
	// even if it remains active. Zero disables the hard cap.
	MaxSessionDuration time.Duration
	Store              store.Store
	LiveKit            *LiveKitTokenIssuer
	MediaBridge        MediaBridgeFactory
	// ReadLimit caps per-frame bytes the upgraded WebSocket will read.
	// Zero or negative falls back to defaultWSReadLimitBytes (64 KiB).
	ReadLimit int64
	// ToolRouter, when non-nil, supplies server-executed tools for each
	// session (voice-agent tool bridge). Nil disables server-side tool
	// execution; all provider tool calls pass through to the client as
	// before.
	ToolRouter SessionToolRouter
	Usage      UsageReporter
}

// Handler exposes both the HTTP session-creation endpoint and the WS
// upgrade endpoint under /v1/voiceagent/*.
type Handler struct {
	manager            *SessionManager
	providers          map[string]ProviderFactory
	defaultProvider    string
	persona            PersonaResolver
	publicURL          string
	allowedOrigins     []string
	idleTimeout        time.Duration
	maxSessionDuration time.Duration
	store              store.Store
	liveKit            *LiveKitTokenIssuer
	mediaBridge        MediaBridgeFactory
	readLimit          int64
	trustedProxies     httpx.TrustedProxies
	toolRouter         SessionToolRouter
	usage              UsageReporter
}

// New constructs a handler. All options except MaxAllowedClockSkew are
// required — the adapter cannot function without a manager, provider, and
// persona resolver.
func New(opts HandlerOptions) (*Handler, error) {
	if opts.Manager == nil {
		return nil, errors.New("voiceagent: Manager is required")
	}
	if opts.Persona == nil {
		return nil, errors.New("voiceagent: Persona resolver is required")
	}
	// Build the provider registry. Multi-provider (Providers map) takes
	// precedence; a lone Provider is accepted as the single default backend
	// so existing callers and tests keep working unchanged.
	providers := make(map[string]ProviderFactory, len(opts.Providers)+1)
	for name, f := range opts.Providers {
		n := strings.ToLower(strings.TrimSpace(name))
		if n != "" && f != nil {
			providers[n] = f
		}
	}
	defaultProvider := strings.ToLower(strings.TrimSpace(opts.DefaultProvider))
	if len(providers) == 0 {
		if opts.Provider == nil {
			return nil, errors.New("voiceagent: at least one Provider is required")
		}
		if defaultProvider == "" {
			defaultProvider = "default"
		}
		providers[defaultProvider] = opts.Provider
	}
	if defaultProvider == "" || providers[defaultProvider] == nil {
		return nil, errors.New("voiceagent: DefaultProvider must name one of the configured Providers")
	}
	idle := opts.IdleTimeout
	if idle == 0 {
		idle = 15 * time.Minute
	} else if idle < 0 {
		idle = 0
	}
	maxSessionDuration := opts.MaxSessionDuration
	if maxSessionDuration < 0 {
		maxSessionDuration = 0
	}
	mediaBridge := opts.MediaBridge
	if mediaBridge == nil && opts.LiveKit != nil && opts.LiveKit.Enabled() {
		mediaBridge = NewLiveKitMediaBridgeFactory(opts.LiveKit)
	}
	readLimit := opts.ReadLimit
	if readLimit <= 0 {
		readLimit = defaultWSReadLimitBytes
	}
	trustedProxies, _ := httpx.NewTrustedProxies(opts.TrustedProxyCIDRs)
	return &Handler{
		manager:            opts.Manager,
		providers:          providers,
		defaultProvider:    defaultProvider,
		persona:            opts.Persona,
		publicURL:          strings.TrimSpace(opts.PublicURL),
		allowedOrigins:     normalizeAllowedOrigins(opts.AllowedOrigins),
		idleTimeout:        idle,
		maxSessionDuration: maxSessionDuration,
		store:              opts.Store,
		liveKit:            opts.LiveKit,
		mediaBridge:        mediaBridge,
		readLimit:          readLimit,
		trustedProxies:     trustedProxies,
		toolRouter:         opts.ToolRouter,
		usage:              opts.Usage,
	}, nil
}

// Mount wires the voiceagent endpoints onto mux:
//
//	POST   /v1/voiceagent/sessions        — create session + mint ticket
//	GET    /v1/voiceagent/sessions        — list caller's active sessions
//	DELETE /v1/voiceagent/sessions/{id}   — force close a session
//	GET    /v1/voiceagent/sessions/{id}/ws — upgrade to WebSocket with Sec-WebSocket-Protocol: ticket.<ticket>
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/voiceagent/sessions", h.collectionHandler)
	mux.HandleFunc("/v1/voiceagent/sessions/", h.itemHandler)
}
