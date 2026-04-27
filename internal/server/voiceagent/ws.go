//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
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

// LiveConfigFrame is the subset of configuration the adapter derives from a
// StartFrame and the persona/role resolver. Kept as a separate type so the
// test double doesn't need to depend on the kernel's concrete LiveConfig
// (which embeds Google genai types).
type LiveConfigFrame struct {
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
}

// LiveMessage is the subset of kernel/internal/voiceagent.LiveMessage the
// adapter relays to the client. Matching field names keep the translation
// trivial.
type LiveMessage struct {
	Audio                []byte
	OutputTranscript     string
	OutputTranscriptDone bool
	InputTranscript      string
	InputTranscriptDone  bool
	Interrupted          bool
	GoAway               bool
}

// PersonaResolver derives a LiveConfigFrame from a StartFrame. The server's
// persona registry implements this in M5; for M4 a stub resolver is used
// that echoes the StartFrame through with sensible defaults.
type PersonaResolver interface {
	Resolve(StartFrame) (LiveConfigFrame, error)
}

// HandlerOptions configures the WebSocket handler.
type HandlerOptions struct {
	Manager             *SessionManager
	Provider            ProviderFactory
	Persona             PersonaResolver
	MaxAllowedClockSkew time.Duration
	// IdleTimeout terminates a session that hasn't seen any activity
	// (client frame OR provider message) within the duration. Zero
	// disables the server-side idle watchdog. Defaults to 15 minutes
	// when zero is passed; pass a negative value to disable explicitly.
	IdleTimeout time.Duration
}

// Handler exposes both the HTTP session-creation endpoint and the WS
// upgrade endpoint under /v1/voiceagent/*.
type Handler struct {
	manager     *SessionManager
	provider    ProviderFactory
	persona     PersonaResolver
	idleTimeout time.Duration
}

// New constructs a handler. All options except MaxAllowedClockSkew are
// required â€” the adapter cannot function without a manager, provider, and
// persona resolver.
func New(opts HandlerOptions) (*Handler, error) {
	if opts.Manager == nil {
		return nil, errors.New("voiceagent: Manager is required")
	}
	if opts.Provider == nil {
		return nil, errors.New("voiceagent: Provider is required")
	}
	if opts.Persona == nil {
		return nil, errors.New("voiceagent: Persona resolver is required")
	}
	idle := opts.IdleTimeout
	if idle == 0 {
		idle = 15 * time.Minute
	} else if idle < 0 {
		idle = 0
	}
	return &Handler{
		manager:     opts.Manager,
		provider:    opts.Provider,
		persona:     opts.Persona,
		idleTimeout: idle,
	}, nil
}

// Mount wires the voiceagent endpoints onto mux:
//
//	POST   /v1/voiceagent/sessions        â€” create session + mint ticket
//	GET    /v1/voiceagent/sessions        â€” list caller's active sessions
//	DELETE /v1/voiceagent/sessions/{id}   â€” force close a session
//	GET    /v1/voiceagent/sessions/{id}/ws?ticket=... â€” upgrade to WebSocket
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/voiceagent/sessions", h.collectionHandler)
	mux.HandleFunc("/v1/voiceagent/sessions/", h.itemHandler)
}

// â”€â”€ HTTP endpoints â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

type createSessionResponse struct {
	SessionID string `json:"session_id"`
	WSURL     string `json:"ws_url"`
	Ticket    string `json:"ticket"`
	ExpiresAt string `json:"expires_at"`
}

type listSessionsResponse struct {
	Sessions []listedSession `json:"sessions"`
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
	// Expect either "{id}" or "{id}/ws".
	var (
		sessionID string
		isWS      bool
	)
	if slash := strings.Index(path, "/"); slash >= 0 {
		sessionID = path[:slash]
		isWS = path[slash+1:] == "ws"
	} else {
		sessionID = path
	}
	if strings.TrimSpace(sessionID) == "" {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "session id missing from path")
		return
	}

	switch {
	case isWS && r.Method == http.MethodGet:
		h.upgradeWS(w, r, sessionID)
	case !isWS && r.Method == http.MethodDelete:
		h.deleteSession(w, r, sessionID)
	default:
		w.Header().Set("Allow", "GET (ws), DELETE")
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"unsupported method for this sub-resource")
	}
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

	// Build the WebSocket URL reflective of the request scheme / host.
	scheme := "wss"
	if r.TLS == nil && !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "ws"
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	wsURL := fmt.Sprintf("%s://%s/v1/voiceagent/sessions/%s/ws?ticket=%s", scheme, host, session.ID, ticket)
	expires := h.manager.opts.Clock().Add(h.manager.opts.TicketTTL).UTC().Format(time.RFC3339)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(createSessionResponse{
		SessionID: session.ID,
		WSURL:     wsURL,
		Ticket:    ticket,
		ExpiresAt: expires,
	})
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	id := middleware.IdentityFromContext(r.Context())
	if id.UserID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "identity not available on context")
		return
	}
	sessions := h.manager.List(id.UserID)
	resp := listSessionsResponse{Sessions: make([]listedSession, 0, len(sessions))}
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

// â”€â”€ WebSocket upgrade â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

func (h *Handler) upgradeWS(w http.ResponseWriter, r *http.Request, sessionID string) {
	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
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

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // origin check runs at the CORS middleware
	})
	if err != nil {
		h.manager.Remove(sessionID)
		slog.Warn("voiceagent: WS upgrade failed", "session_id", sessionID, "err", err)
		return
	}
	// Conservative read limit â€” raw PCM chunks should be well under 4 KB.
	conn.SetReadLimit(1 << 20)

	adapter := &Adapter{
		Session:     session,
		Conn:        conn,
		Provider:    h.provider.NewProvider(),
		Persona:     h.persona,
		IdleTimeout: h.idleTimeout,
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
