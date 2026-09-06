//go:build linux

package vocabulary

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/store"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

type Handler struct {
	store store.UserDictionaryStore
}

func New(dictionaryStore store.UserDictionaryStore) *Handler {
	return &Handler{store: dictionaryStore}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/vocabulary/dictionary", h.dictionary)
}

type replaceRequest struct {
	Language string                      `json:"language"`
	Entries  []store.UserDictionaryEntry `json:"entries"`
}

func (h *Handler) dictionary(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "store_unavailable", "vocabulary storage is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !validateScopeQuery(w, r) {
			return
		}
		language := strings.TrimSpace(r.URL.Query().Get("language"))
		entries, err := h.store.ListUserDictionaryEntries(r.Context(), language)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "dictionary_read_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	case http.MethodPost:
		if middleware.IdentityFromContext(r.Context()).Role != "admin" {
			httpx.WriteError(w, http.StatusForbidden, "admin_required", "dictionary writes require an admin identity")
			return
		}
		var body replaceRequest
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.Language == "" {
			body.Language = strings.TrimSpace(r.URL.Query().Get("language"))
		}
		if err := h.store.ReplaceUserDictionaryEntries(r.Context(), body.Language, body.Entries); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "dictionary_write_failed", err.Error())
			return
		}
		entries, _ := h.store.ListUserDictionaryEntries(r.Context(), body.Language)
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on this resource")
	}
}

func validateScopeQuery(w http.ResponseWriter, r *http.Request) bool {
	query := r.URL.Query()
	if query.Has("scope_key") && query.Get("scope_key") == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_scope", "scope_key must not be empty; omit it for unkeyed scopes")
		return false
	}
	values, exists := query["scope"]
	if !exists {
		return true
	}
	scope := ""
	if len(values) > 0 {
		scope = values[0]
	}
	if isPublishedScopeKind(speechcustomize.ScopeKind(scope)) {
		return true
	}
	httpx.WriteError(w, http.StatusBadRequest, "invalid_scope", "scope must be one of builtin, app, install, org, workspace, user, session")
	return false
}

func isPublishedScopeKind(kind speechcustomize.ScopeKind) bool {
	switch kind {
	case speechcustomize.ScopeBuiltin,
		speechcustomize.ScopeApp,
		speechcustomize.ScopeInstall,
		speechcustomize.ScopeOrg,
		speechcustomize.ScopeWorkspace,
		speechcustomize.ScopeUser,
		speechcustomize.ScopeSession:
		return true
	default:
		return false
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
