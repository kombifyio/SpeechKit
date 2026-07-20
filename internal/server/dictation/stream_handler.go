//go:build linux

package dictation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/server/wssession"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// defaultStreamWSReadLimitBytes mirrors the Voice Agent per-frame cap: raw
// PCM chunks are well under 4 KB in practice; 64 KiB leaves ample headroom
// without a memory amplification vector.
const defaultStreamWSReadLimitBytes int64 = 64 * 1024

// StreamRouter is the minimal surface the streaming handler needs from the
// STT router. The production implementation is `internal/router.Router`;
// tests provide a fake.
type StreamRouter interface {
	StartDictationStream(ctx context.Context, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (speechkit.DictationStream, error)
	HasDictationStreaming() bool
}

// StreamHandlerOptions configures the streaming Dictation WebSocket surface.
type StreamHandlerOptions struct {
	Manager *wssession.SessionManager
	Router  StreamRouter
	// PublicURL overrides request-derived scheme/host in returned ws_url
	// values (Docker/proxy deployments).
	PublicURL         string
	AllowedOrigins    []string
	TrustedProxyCIDRs []string
	// IdleTimeout terminates a session without any client- or provider-side
	// activity. Zero defaults to 5 minutes; negative disables.
	IdleTimeout time.Duration
	// MaxSessionDuration hard-caps a session's wall-clock lifetime. Zero
	// disables the cap.
	MaxSessionDuration time.Duration
	// MaxStreamAudio caps the cumulative PCM a session may upload, expressed
	// as decoded audio duration. Zero disables the budget.
	MaxStreamAudio time.Duration
	// ReadLimit caps per-frame bytes; zero or negative falls back to 64 KiB.
	ReadLimit int64
}

// StreamHandler exposes the session-creation endpoint and the WS upgrade
// endpoint under /v1/dictation/stream/*.
type StreamHandler struct {
	manager            *wssession.SessionManager
	router             StreamRouter
	publicURL          string
	allowedOrigins     []string
	idleTimeout        time.Duration
	maxSessionDuration time.Duration
	maxStreamAudio     time.Duration
	readLimit          int64
	trustedProxies     httpx.TrustedProxies
}

// NewStreamHandler constructs the streaming handler.
func NewStreamHandler(opts StreamHandlerOptions) (*StreamHandler, error) {
	if opts.Manager == nil {
		return nil, errors.New("dictation: stream Manager is required")
	}
	if opts.Router == nil {
		return nil, errors.New("dictation: stream Router is required")
	}
	idle := opts.IdleTimeout
	if idle == 0 {
		idle = 5 * time.Minute
	} else if idle < 0 {
		idle = 0
	}
	maxSessionDuration := opts.MaxSessionDuration
	if maxSessionDuration < 0 {
		maxSessionDuration = 0
	}
	maxStreamAudio := opts.MaxStreamAudio
	if maxStreamAudio < 0 {
		maxStreamAudio = 0
	}
	readLimit := opts.ReadLimit
	if readLimit <= 0 {
		readLimit = defaultStreamWSReadLimitBytes
	}
	trustedProxies, _ := httpx.NewTrustedProxies(opts.TrustedProxyCIDRs)
	return &StreamHandler{
		manager:            opts.Manager,
		router:             opts.Router,
		publicURL:          strings.TrimSpace(opts.PublicURL),
		allowedOrigins:     wssession.NormalizeAllowedOrigins(opts.AllowedOrigins),
		idleTimeout:        idle,
		maxSessionDuration: maxSessionDuration,
		maxStreamAudio:     maxStreamAudio,
		readLimit:          readLimit,
		trustedProxies:     trustedProxies,
	}, nil
}

// Mount wires the streaming endpoints onto mux:
//
//	POST   /v1/dictation/stream/sessions          — create session + mint ticket
//	DELETE /v1/dictation/stream/sessions/{id}     — force close a session
//	GET    /v1/dictation/stream/sessions/{id}/ws  — upgrade to WebSocket with Sec-WebSocket-Protocol: ticket.<ticket>
func (h *StreamHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/dictation/stream/sessions", h.collectionHandler)
	mux.HandleFunc("/v1/dictation/stream/sessions/", h.itemHandler)
}

// streamCapabilities tells the client, at session-create time, whether live
// provider streaming is actually available so it can fall back to the batch
// endpoint without a doomed WS round trip. Emulation is reserved ("off" in
// v1; a config-gated chunked-batch mode may fill it later).
type streamCapabilities struct {
	Streaming bool   `json:"streaming"`
	Emulation string `json:"emulation"`
}

type createStreamSessionResponse struct {
	SessionID     string             `json:"session_id"`
	WSURL         string             `json:"ws_url"`
	WSSubprotocol string             `json:"ws_subprotocol,omitempty"`
	Ticket        string             `json:"ticket"`
	ExpiresAt     string             `json:"expires_at"`
	Capabilities  streamCapabilities `json:"capabilities"`
}

func (h *StreamHandler) collectionHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createSession(w, r)
	default:
		w.Header().Set("Allow", "POST")
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"only POST is accepted at this endpoint")
	}
}

func (h *StreamHandler) itemHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/dictation/stream/sessions/")
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
	case subresource == "" && r.Method == http.MethodDelete:
		h.deleteSession(w, r, sessionID)
	default:
		w.Header().Set("Allow", "GET (ws), DELETE")
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"unsupported method for this sub-resource")
	}
}

func (h *StreamHandler) createSession(w http.ResponseWriter, r *http.Request) {
	id := middleware.IdentityFromContext(r.Context())
	if id.UserID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "identity not available on context")
		return
	}
	session, ticket, err := h.manager.Create(wssession.Identity{
		UserID: id.UserID,
		OrgID:  id.OrgID,
		Plan:   id.Plan,
		Role:   id.Role,
	})
	if err == nil {
		// Capture the edge-resolved voice preferences at mint time: the WS
		// upgrade authenticates via ticket and never sees the edge headers.
		// The middleware attaches them only under a verified edge HMAC, so a
		// plain bearer/browser caller cannot inject a preference. Non-secret
		// provider names; used as the segment default when a `start` frame
		// omits provider_profile_id.
		session.VoicePrefs = wssession.VoicePrefs(middleware.VoicePrefsFromContext(r.Context()))
	}
	if err != nil {
		switch {
		case errors.Is(err, wssession.ErrIdentityLimitExceeded):
			httpx.WriteError(w, http.StatusConflict, "per_user_limit_exceeded", err.Error())
		case errors.Is(err, wssession.ErrGlobalLimitExceeded):
			httpx.WriteError(w, http.StatusServiceUnavailable, "global_limit_exceeded", err.Error())
		default:
			httpx.WriteError(w, http.StatusInternalServerError, "create_failed", err.Error())
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(createStreamSessionResponse{
		SessionID:     session.ID,
		WSURL:         h.webSocketURL(r, session.ID),
		WSSubprotocol: wssession.TicketSubprotocol(ticket),
		Ticket:        ticket,
		ExpiresAt:     h.manager.TicketExpiresAt().Format(time.RFC3339),
		Capabilities: streamCapabilities{
			Streaming: h.router.HasDictationStreaming(),
			Emulation: "off",
		},
	})
}

func (h *StreamHandler) deleteSession(w http.ResponseWriter, r *http.Request, sessionID string) {
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

func (h *StreamHandler) upgradeWS(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !wssession.UpgradeOriginAllowed(r, h.allowedOrigins) {
		httpx.WriteError(w, http.StatusForbidden, "origin_not_allowed", "websocket origin is not allowed")
		return
	}

	ticket, ticketSubproto := wssession.ExtractTicket(r)
	if err := h.manager.VerifyTicket(sessionID, ticket); err != nil {
		switch {
		case errors.Is(err, wssession.ErrSessionExpired):
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
		case errors.Is(err, wssession.ErrSessionAlreadyActive):
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
		// Echo the negotiated subprotocol per RFC 6455 §4.2.2 so the browser
		// accepts the handshake.
		acceptOpts.Subprotocols = []string{ticketSubproto}
	}
	conn, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		h.manager.Remove(sessionID)
		return
	}
	conn.SetReadLimit(h.readLimit)

	adapter := &StreamAdapter{
		Session:        session,
		Conn:           conn,
		Router:         h.router,
		IdleTimeout:    h.idleTimeout,
		MaxDuration:    h.maxSessionDuration,
		MaxStreamAudio: h.maxStreamAudio,
		OnClose: func() {
			h.manager.Remove(sessionID)
		},
	}
	// Run the pumps; returns only when the session ends or the client
	// disconnects. Not run in a goroutine: the HTTP handler owns the
	// connection for its lifetime, which keeps observability correct.
	adapter.Run(r.Context())
}

func (h *StreamHandler) webSocketURL(r *http.Request, sessionID string) string {
	return wssession.WebSocketURL(r.Host, r.Header.Get(httpx.APIPrefixHeader), h.publicURL,
		h.trustedProxies.RequestIsHTTPS(r), "/dictation/stream/sessions/"+sessionID+"/ws")
}
