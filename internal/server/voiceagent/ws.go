//go:build linux

package voiceagent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/server/storageauth"
	"github.com/kombifyio/SpeechKit/internal/server/wssession"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
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

// ProviderFactory builds a Framework kernel voice-agent provider on demand.
// Each WebSocket session gets its own provider instance so concurrent
// sessions don't share Gemini Live state.
//
// The concrete production implementation returns a *voiceagent.GeminiLive;
// tests supply a fake that records frames and replies with canned audio.
type ProviderFactory interface {
	NewProvider() LiveProviderAdapter
}

// LiveProviderAdapter is the minimal slice of the kernel's LiveProvider
// interface the Server-Target adapter needs. Keeping it narrow makes it
// trivial for tests to stub without pulling in the full genai SDK.
type LiveProviderAdapter interface {
	Connect(ctx context.Context, cfg LiveConfigFrame) error
	SendAudio(chunk []byte) error
	SendAudioStreamEnd() error
	SendText(text string) error
	Receive(ctx context.Context) (*LiveMessage, error)
	Close() error
	Name() string
}

// LiveInstructionUpdater is implemented by providers that can update their
// active host instructions without treating the update as a user turn.
type LiveInstructionUpdater interface {
	UpdateInstructions(ctx context.Context, cfg LiveConfigFrame) error
}

// LiveToolResponder is implemented by providers that accept host-side tool
// results from the client.
type LiveToolResponder interface {
	SendToolResponse(ToolResponseFrame) error
}

// LiveConfigFrame is the subset of configuration the adapter derives from a
// StartFrame and the persona/role resolver. Kept as a separate type so the
// test double doesn't need to depend on the kernel's concrete LiveConfig
// (which embeds Google genai types).
type LiveConfigFrame struct {
	PersonaID string
	RoleID    string
	// Sequence metadata is internal runtime state derived from the
	// persona/role resolver. It lets the WebSocket adapter report and advance
	// workflow steps without coupling to the persona package.
	SequenceID         string
	SequenceCompletion string
	SequenceMaxTurns   int
	StepID             string
	StepIndex          int
	StepCount          int
	StepInstruction    string
	StepExitCriteria   string
	StepMaxTurns       int

	Model string
	// FallbackModel is forwarded to providers that support same-provider
	// fallback (kernel Gemini Live retries the fallback when the primary
	// connect fails). Empty disables the fallback.
	FallbackModel    string
	APIKey           string
	Voice            string
	SystemPrompt     string
	RefinementPrompt string
	Locale           string
	// Raw activity-detection passthrough; adapter translates to the
	// kernel's internal types.
	Automatic         bool
	StartSensitivity  string
	EndSensitivity    string
	PrefixPaddingMs   int32
	SilenceDurationMs int32
	ActivityHandling  string
	TurnCoverage      string
	Speaker           speaker.Options
	// Tools are server-executed tool definitions merged in by the adapter
	// from the SessionToolRouter (tool bridge). Provider bridges that support
	// session tools map these onto the kernel's LiveConfig.Tools; providers
	// without tool support ignore them.
	Tools []ToolDefinitionFrame
}

// LiveMessage is the subset of kernel/internal/voiceagent.LiveMessage the
// adapter relays to the client. Matching field names keep the translation
// trivial.
type LiveMessage struct {
	EventType              string
	EventTypes             []string
	ProviderMetadata       map[string]any
	Audio                  []byte
	Done                   bool
	OutputTranscript       string
	OutputTranscriptDone   bool
	InputTranscript        string
	InputTranscriptDone    bool
	InputSpeakerLabel      string
	InputPersonID          string
	InputDisplayName       string
	InputSpeakerConfidence float64
	ToolCalls              []ToolCall
	Interrupted            bool
	GoAway                 bool
	SessionResumable       bool
}

type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// PersonaResolver derives a LiveConfigFrame from a StartFrame. The server's
// persona registry implements this in M5; for M4 a stub resolver is used
// that echoes the StartFrame through with sensible defaults.
type PersonaResolver interface {
	Resolve(StartFrame) (LiveConfigFrame, error)
}

// HandlerOptions configures the WebSocket handler.
type HandlerOptions struct {
	Manager *SessionManager
	// Provider is the single-provider form (back-compat): when Providers is
	// empty this factory becomes the sole, default backend.
	Provider ProviderFactory
	// Providers is the multi-provider form: a name→factory map (e.g.
	// "deepgram", "gemini", "cascaded") that the client selects between
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

// ── HTTP endpoints ──────────────────────────────────────────────────────────

type createSessionResponse struct {
	SessionID     string           `json:"session_id"`
	WSURL         string           `json:"ws_url"`
	WSSubprotocol string           `json:"ws_subprotocol,omitempty"`
	Ticket        string           `json:"ticket"`
	ExpiresAt     string           `json:"expires_at"`
	LiveKit       *LiveKitJoinInfo `json:"livekit,omitempty"`
}

type listSessionsResponse struct {
	Sessions []listedSession `json:"sessions"`
	Metrics  SessionStats    `json:"metrics"`
}

type listedSession struct {
	SessionID   string `json:"session_id"`
	State       string `json:"state"`
	CreatedAt   string `json:"created_at"`
	HasWSClient bool   `json:"has_ws_client"`
}

func (h *Handler) collectionHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createSession(w, r)
	case http.MethodGet:
		h.listSessions(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"only GET and POST are accepted at this endpoint")
	}
}

func (h *Handler) itemHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/voiceagent/sessions/")
	// Expect "{id}", "{id}/ws", "{id}/livekit-token", "{id}/transcript", or "{id}/summary".
	var (
		sessionID   string
		subresource string
	)
	if slash := strings.Index(path, "/"); slash >= 0 {
		sessionID = path[:slash]
		subresource = path[slash+1:]
	} else {
		sessionID = path
	}
	if strings.TrimSpace(sessionID) == "" {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "session id missing from path")
		return
	}

	switch {
	case subresource == "ws" && r.Method == http.MethodGet:
		h.upgradeWS(w, r, sessionID)
	case subresource == "livekit-token" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		h.liveKitToken(w, r, sessionID)
	case subresource == "" && r.Method == http.MethodDelete:
		h.deleteSession(w, r, sessionID)
	case (subresource == "transcript" || subresource == "summary") && r.Method == http.MethodGet:
		h.persistedSessionSubresource(w, r, sessionID, subresource)
	default:
		w.Header().Set("Allow", "GET (ws/transcript/summary), DELETE")
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"unsupported method for this sub-resource")
	}
}

func (h *Handler) persistedSessionSubresource(w http.ResponseWriter, r *http.Request, sessionID, subresource string) {
	vs, ok := h.store.(store.VoiceAgentSessionStore)
	if h.store == nil || !ok {
		httpx.WriteError(w, http.StatusServiceUnavailable, "store_unavailable", "voice agent session storage is not configured")
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(sessionID), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, http.StatusNotFound, "session_not_found", "voice agent session id must be numeric")
		return
	}
	session, err := vs.GetVoiceAgentSession(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteError(w, http.StatusNotFound, "session_not_found", "voice agent session not found")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "session_read_failed", err.Error())
		return
	}
	if !storageauth.CanReadOwned(r, session.OwnerUserID, session.OwnerOrgID) {
		httpx.WriteError(w, http.StatusForbidden, "forbidden", "voice agent session is not owned by this caller")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if subresource == "transcript" {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         session.ID,
			"transcript": session.Transcript,
			"turns":      session.Turns,
			"language":   session.Language,
			"created_at": session.CreatedAt,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":         session.ID,
		"summary":    session.Summary,
		"language":   session.Language,
		"created_at": session.CreatedAt,
	})
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	id := middleware.IdentityFromContext(r.Context())
	if id.UserID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "identity not available on context")
		return
	}
	session, ticket, err := h.manager.Create(Identity{
		UserID: id.UserID,
		OrgID:  id.OrgID,
		Plan:   id.Plan,
		Role:   id.Role,
	})
	if err == nil {
		// Capture the optional per-session tool-bridge credential the edge
		// forwarded with this request. The middleware only attaches it when
		// edge-HMAC auth succeeded, so a plain bearer/browser caller cannot
		// inject one. Memory-only secret: it is stored on the session record
		// exclusively — never in the ticket, the JSON response, or logs.
		session.BridgeCredential = middleware.EdgeOboSubjectTokenFromContext(r.Context())
		// Same trust boundary, non-secret payload: the edge-resolved voice
		// preferences (provider/persona names) captured at mint time. The WS
		// upgrade authenticates via ticket and never sees the edge headers,
		// so the session record is the only carrier. Used by the adapter as
		// defaults when the start frame omits provider or persona_id.
		session.VoicePrefs = wssession.VoicePrefs(middleware.VoicePrefsFromContext(r.Context()))
	}
	if err != nil {
		switch {
		case errors.Is(err, ErrIdentityLimitExceeded):
			httpx.WriteError(w, http.StatusConflict, "per_user_limit_exceeded", err.Error())
		case errors.Is(err, ErrGlobalLimitExceeded):
			httpx.WriteError(w, http.StatusServiceUnavailable, "global_limit_exceeded", err.Error())
		default:
			httpx.WriteError(w, http.StatusInternalServerError, "create_failed", err.Error())
		}
		return
	}

	wsURL := h.webSocketURL(r, session.ID)
	wsSubprotocol := wsTicketSubprotocol(ticket)
	expires := h.manager.TicketExpiresAt().Format(time.RFC3339)
	var liveKit *LiveKitJoinInfo
	if h.liveKit != nil && h.liveKit.Enabled() {
		info, err := h.liveKit.IssueJoinToken(r.Context(), session.ID, session.Owner)
		if err != nil {
			h.manager.Remove(session.ID)
			httpx.WriteError(w, http.StatusServiceUnavailable, "livekit_token_unavailable", err.Error())
			return
		}
		liveKit = &info
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(createSessionResponse{
		SessionID:     session.ID,
		WSURL:         wsURL,
		WSSubprotocol: wsSubprotocol,
		Ticket:        ticket,
		ExpiresAt:     expires,
		LiveKit:       liveKit,
	})
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	id := middleware.IdentityFromContext(r.Context())
	if id.UserID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "identity not available on context")
		return
	}
	sessions := h.manager.List(id.UserID)
	resp := listSessionsResponse{
		Sessions: make([]listedSession, 0, len(sessions)),
		Metrics:  h.manager.Stats(id.UserID),
	}
	for _, s := range sessions {
		resp.Sessions = append(resp.Sessions, listedSession{
			SessionID:   s.ID,
			State:       string(s.State),
			CreatedAt:   s.CreatedAt.Format(time.RFC3339),
			HasWSClient: s.HasWSClient,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	id := middleware.IdentityFromContext(r.Context())
	if id.UserID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "identity not available on context")
		return
	}
	s, err := h.manager.Get(sessionID)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "session_not_found", err.Error())
		return
	}
	if s.Owner.UserID != id.UserID && id.Role != "admin" {
		httpx.WriteError(w, http.StatusForbidden, "forbidden", "you do not own this session")
		return
	}
	h.manager.Remove(sessionID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) liveKitToken(w http.ResponseWriter, r *http.Request, sessionID string) {
	if h.liveKit == nil || !h.liveKit.Enabled() {
		httpx.WriteError(w, http.StatusServiceUnavailable, "livekit_disabled", "LiveKit token minting is not configured")
		return
	}
	id := middleware.IdentityFromContext(r.Context())
	if id.UserID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "identity not available on context")
		return
	}
	s, err := h.manager.Get(sessionID)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "session_not_found", err.Error())
		return
	}
	if s.Owner.UserID != id.UserID && id.Role != "admin" {
		httpx.WriteError(w, http.StatusForbidden, "forbidden", "you do not own this session")
		return
	}
	info, err := h.liveKit.IssueJoinToken(r.Context(), sessionID, s.Owner)
	if err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "livekit_token_unavailable", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(info)
}

// ── WebSocket upgrade ───────────────────────────────────────────────────────

func (h *Handler) upgradeWS(w http.ResponseWriter, r *http.Request, sessionID string) {
	// Ticketed native clients (no Origin header) proceed to ticket
	// verification; browser requests stay subject to the Origin allowlist.
	if !wssession.UpgradeOriginAllowed(r, h.allowedOrigins) {
		httpx.WriteError(w, http.StatusForbidden, "origin_not_allowed", "websocket origin is not allowed")
		return
	}

	ticket, ticketSubproto := extractWSTicket(r)
	if err := h.manager.VerifyTicket(sessionID, ticket); err != nil {
		switch {
		case errors.Is(err, ErrSessionExpired):
			httpx.WriteError(w, http.StatusGone, "ticket_expired", err.Error())
		default:
			httpx.WriteError(w, http.StatusUnauthorized, "invalid_ticket", err.Error())
		}
		return
	}
	session, err := h.manager.Get(sessionID)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "session_not_found", err.Error())
		return
	}
	if err := h.manager.Attach(sessionID); err != nil {
		switch {
		case errors.Is(err, ErrSessionAlreadyActive):
			httpx.WriteError(w, http.StatusConflict, "already_active", err.Error())
		default:
			httpx.WriteError(w, http.StatusInternalServerError, "attach_failed", err.Error())
		}
		return
	}

	acceptOpts := &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Origin was checked explicitly above.
	}
	if ticketSubproto != "" {
		// Echo the negotiated subprotocol per RFC 6455 §4.2.2 so the
		// browser accepts the handshake. Listing it here makes the
		// coder/websocket library emit Sec-WebSocket-Protocol on the
		// 101 response.
		acceptOpts.Subprotocols = []string{ticketSubproto}
	}
	conn, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		h.manager.Remove(sessionID)
		slog.Warn("voiceagent: WS upgrade failed", "session_id", sessionID, "err", err) // #nosec G706 -- slog writes request/session values as structured attributes, not interpolated log text.
		return
	}
	conn.SetReadLimit(h.readLimit)

	adapter := &Adapter{
		Session:         session,
		Conn:            conn,
		Providers:       h.providers,
		DefaultProvider: h.defaultProvider,
		Persona:         h.persona,
		MediaBridge:     h.mediaBridge,
		IdleTimeout:     h.idleTimeout,
		MaxDuration:     h.maxSessionDuration,
		ToolRouter:      h.toolRouter,
		OnClose: func() {
			h.manager.Remove(sessionID)
		},
	}
	// Run the pumps; returns only when session ends or the client
	// disconnects. We intentionally do not run this in a goroutine: the
	// HTTP handler owns the connection for its lifetime, which keeps
	// observability and tracing correct.
	adapter.Run(r.Context())
}

func (h *Handler) webSocketURL(r *http.Request, sessionID string) string {
	requestIsHTTPS := h != nil && h.trustedProxies.RequestIsHTTPS(r)
	publicURL := ""
	if h != nil {
		publicURL = h.publicURL
	}
	return wssession.WebSocketURL(r.Host, r.Header.Get(httpx.APIPrefixHeader), publicURL,
		requestIsHTTPS, "/voiceagent/sessions/"+sessionID+"/ws")
}

func wsTicketSubprotocol(ticket string) string {
	return wssession.TicketSubprotocol(ticket)
}

// extractWSTicket reads only the Sec-WebSocket-Protocol subprotocol form.
// The returned subproto string MUST be echoed back to the client via
// AcceptOptions.Subprotocols so the WS handshake completes per RFC 6455.
func extractWSTicket(r *http.Request) (ticket, subproto string) {
	return wssession.ExtractTicket(r)
}

func normalizeAllowedOrigins(origins []string) []string {
	return wssession.NormalizeAllowedOrigins(origins)
}

func originAllowedForWebSocket(origin string, allowed []string) bool {
	return wssession.OriginAllowed(origin, allowed)
}
