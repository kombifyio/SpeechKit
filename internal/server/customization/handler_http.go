//go:build linux

package customization

import (
	"encoding/json"
	"net/http"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if middleware.IdentityFromContext(r.Context()).Role == "admin" {
		return true
	}
	httpx.WriteError(w, http.StatusForbidden, "admin_required", "customization writes require an admin identity")
	return false
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	if err := dec.Decode(dst); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return false
	}
	return true
}

func writeList(w http.ResponseWriter, key string, value any, err error) {
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, key+"_read_failed", err.Error())
		return
	}
	writeJSON(w, map[string]any{key: value})
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
	httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on this resource")
}
