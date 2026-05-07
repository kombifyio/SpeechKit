//go:build linux

package transcripts

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/storageauth"
	"github.com/kombifyio/SpeechKit/internal/store"
)

type Handler struct {
	store store.Store
}

func New(store store.Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/transcripts", h.transcripts)
	mux.HandleFunc("/v1/transcripts/", h.transcriptItem)
}

func (h *Handler) MountVoiceAgentReads(mux *http.ServeMux) {
	mux.HandleFunc("/v1/voiceagent/sessions/", h.voiceAgentSessionSubresource)
}

func (h *Handler) transcripts(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "store_unavailable", "transcript storage is not configured")
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	opts := storageauth.ListOptsForIdentity(r, queryLimit(r, 50))
	list, err := h.store.ListTranscriptions(r.Context(), opts)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "transcripts_read_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transcripts": list})
}

func (h *Handler) transcriptItem(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "store_unavailable", "transcript storage is not configured")
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	id, ok := idFromPath(r.URL.Path, "/v1/transcripts/")
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "transcript_not_found", "transcript id missing")
		return
	}
	got, err := h.store.GetTranscription(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteError(w, http.StatusNotFound, "transcript_not_found", "transcript not found")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "transcript_read_failed", err.Error())
		return
	}
	if !storageauth.CanReadOwned(r, got.OwnerUserID, got.OwnerOrgID) {
		httpx.WriteError(w, http.StatusForbidden, "forbidden", "transcript is not owned by this caller")
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func (h *Handler) voiceAgentSessionSubresource(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "store_unavailable", "voice agent session storage is not configured")
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	vs, ok := h.store.(store.VoiceAgentSessionStore)
	if !ok {
		httpx.WriteError(w, http.StatusServiceUnavailable, "store_unavailable", "voice agent session storage is not configured")
		return
	}
	id, tail, ok := sessionPath(r.URL.Path)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "session_not_found", "voice agent session subresource not found")
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
	switch tail {
	case "transcript":
		writeJSON(w, http.StatusOK, map[string]any{
			"id":         session.ID,
			"transcript": session.Transcript,
			"turns":      session.Turns,
			"language":   session.Language,
			"created_at": session.CreatedAt,
		})
	case "summary":
		writeJSON(w, http.StatusOK, map[string]any{
			"id":         session.ID,
			"summary":    session.Summary,
			"language":   session.Language,
			"created_at": session.CreatedAt,
		})
	default:
		httpx.WriteError(w, http.StatusNotFound, "session_not_found", "voice agent session subresource not found")
	}
}

func sessionPath(path string) (int64, string, bool) {
	rest := strings.TrimPrefix(path, "/v1/voiceagent/sessions/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 {
		return 0, "", false
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, "", false
	}
	return id, parts[1], true
}

func idFromPath(path, prefix string) (int64, bool) {
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" || strings.Contains(rest, "/") {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	return id, err == nil && id > 0
}

func queryLimit(r *http.Request, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	if n > 200 {
		return 200
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func methodNotAllowed(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on this resource")
}
