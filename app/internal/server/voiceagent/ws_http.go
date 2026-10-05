//go:build linux

package voiceagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/app/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/app/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/app/internal/server/storageauth"
	"github.com/kombifyio/SpeechKit/app/internal/server/wssession"
	"github.com/kombifyio/SpeechKit/app/internal/store"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/responses"
)

// ── HTTP endpoints ──────────────────────────────────────────────────────────

type createSessionResponse struct {
	SessionID     string           `json:"session_id"`
	AISessionID   string           `json:"ai_session_id,omitempty"`
	WSURL         string           `json:"ws_url"`
	WSSubprotocol string           `json:"ws_subprotocol,omitempty"`
	Ticket        string           `json:"ticket"`
	ExpiresAt     string           `json:"expires_at"`
	LiveKit       *LiveKitJoinInfo `json:"livekit,omitempty"`
}

type createSessionRequest struct {
	AISessionID    string             `json:"ai_session_id,omitempty"`
	Provider       string             `json:"provider,omitempty"`
	TargetAgentID  string             `json:"target_agent_id,omitempty"`
	DirectEndpoint *responses.Context `json:"direct_endpoint,omitempty"`
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
	case subresource == "llm/chat/completions" && r.Method == http.MethodPost:
		h.native.serveCallback(w, r, sessionID)
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
	var input createSessionRequest
	var requestBytes []byte
	if r.Body != nil {
		var err error
		requestBytes, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 192<<10))
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "Voice context is too large")
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(requestBytes))
		if err := decoder.Decode(&input); err != nil && !errors.Is(err, io.EOF) {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
			return
		}
	}
	input.AISessionID = strings.TrimSpace(input.AISessionID)
	input.Provider = normalizeProviderName(input.Provider)
	input.TargetAgentID = strings.TrimSpace(input.TargetAgentID)
	binding := middleware.VoiceAgentBindingFromContext(r.Context())
	if binding.DirectEndpoint != nil || input.DirectEndpoint != nil || input.Provider == "kombify-endpoint" {
		digest := sha256.Sum256(requestBytes)
		if binding.DirectEndpoint == nil || input.DirectEndpoint == nil || input.Provider != "kombify-endpoint" || input.AISessionID != "" || input.TargetAgentID != "" ||
			hex.EncodeToString(digest[:]) != binding.RequestDigest || input.DirectEndpoint.Connection != binding.DirectEndpoint.Connection || input.DirectEndpoint.Surface != binding.DirectEndpoint.Surface ||
			input.DirectEndpoint.ConversationID != binding.DirectEndpoint.ConversationID || responses.ValidateContext(input.DirectEndpoint.Messages) != nil {
			httpx.WriteError(w, http.StatusForbidden, "voice_endpoint_binding_mismatch", "Own endpoint request does not match the authorized direct conversation")
			return
		}
		copy := *input.DirectEndpoint
		copy.Messages = append([]responses.Message(nil), copy.Messages...)
		binding.DirectEndpoint = &copy
	}
	if binding.TargetAgentID != "" && (input.Provider != "kombify-agent" || input.TargetAgentID != binding.TargetAgentID) {
		httpx.WriteError(w, http.StatusForbidden, "voice_agent_binding_mismatch", "registered agent request does not match the edge-authorized target")
		return
	}
	budget := wssession.VoiceBudget(middleware.VoiceBudgetFromContext(r.Context()))
	if h.requireVoiceBudget && (id.Source != "edge_hmac" || budget.ReservationID == "") {
		httpx.WriteError(w, http.StatusForbidden, "voice_budget_required", "Reserve Voice quota before starting a voice session.")
		return
	}
	session, ticket, err := h.manager.CreateWithVoiceBudget(Identity{
		UserID: id.UserID,
		OrgID:  id.OrgID,
		Plan:   id.Plan,
		Role:   id.Role,
	}, input.AISessionID, budget, h.reportUnstartedSession)
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
		session.VoiceAgentBinding = wssession.VoiceAgentBinding(binding)
	}
	if err != nil {
		switch {
		case errors.Is(err, wssession.ErrVoiceBudgetUnavailable):
			httpx.WriteError(w, http.StatusForbidden, "voice_budget_unavailable", "Reserve new Voice quota before starting a voice session.")
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
		info, err := h.issueSessionLiveKitToken(r.Context(), session)
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
		AISessionID:   session.AISessionID,
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
	info, err := h.issueSessionLiveKitToken(r.Context(), s)
	if err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "livekit_token_unavailable", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(info)
}

func (h *Handler) issueSessionLiveKitToken(ctx context.Context, session *ManagedSession) (LiveKitJoinInfo, error) {
	if h.requireVoiceBudget && session.VoiceBudget.ReservationID == "" {
		return LiveKitJoinInfo{}, errors.New("voiceagent: Voice quota reservation is required")
	}
	if session.VoiceBudget.ReservationID != "" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, time.Unix(session.VoiceBudget.ExpiresAt, 0))
		defer cancel()
	}
	return h.liveKit.IssueJoinToken(ctx, session.ID, session.Owner)
}
