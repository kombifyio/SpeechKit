//go:build linux

package voiceagent

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/wssession"

	"github.com/coder/websocket"
)

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
		OnUsage: func(usage VoiceUsage) {
			if h.usage == nil || strings.TrimSpace(session.BridgeCredential) == "" {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.usage.Report(ctx, session.BridgeCredential, usage); err != nil {
				slog.Warn("voiceagent: usage report failed", "session_id", session.ID, "err", err)
			}
		},
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
