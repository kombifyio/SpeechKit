//go:build linux

package persona

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

// Handler exposes CRUD endpoints for personas, roles, and sequences.
// Read endpoints are open to any authenticated caller; write endpoints
// require middleware.Identity.Role == "admin".
//
// Storage is the in-memory Registry; when a Persister is attached
// (M5b SQLite, optional Postgres later), writes round-trip through it
// before committing to memory and persistence failures are propagated
// to the HTTP response (T22 — caller sees 500, not silent 200).
type Handler struct {
	registry *Registry
	// AllowWrites lets deployments disable mutation entirely (useful when
	// the server should be read-only for a given environment). Defaults to
	// true.
	allowWrites bool
}

// writeUpsertError translates a Registry upsert error to the right HTTP
// status: validation errors are 400, persistence failures are 500.
// Centralised so the same mapping covers personas, roles, and sequences.
func writeUpsertError(w http.ResponseWriter, code400 string, err error) {
	if errors.Is(err, ErrPersist) {
		httpx.WriteError(w, http.StatusInternalServerError, "persist_failed", err.Error())
		return
	}
	httpx.WriteError(w, http.StatusBadRequest, code400, err.Error())
}

// writeDeleteError mirrors writeUpsertError for Delete*: not-found is 404,
// persistence failures are 500.
func writeDeleteError(w http.ResponseWriter, code404 string, err error) {
	if errors.Is(err, ErrPersist) {
		httpx.WriteError(w, http.StatusInternalServerError, "persist_failed", err.Error())
		return
	}
	httpx.WriteError(w, http.StatusNotFound, code404, err.Error())
}

// HandlerOptions configures a Handler.
type HandlerOptions struct {
	Registry    *Registry
	AllowWrites bool
}

// New constructs a Handler. Registry is required.
func New(opts HandlerOptions) (*Handler, error) {
	if opts.Registry == nil {
		return nil, errors.New("persona: registry required")
	}
	return &Handler{
		registry:    opts.Registry,
		allowWrites: opts.AllowWrites,
	}, nil
}

// Mount wires all persona/role/sequence endpoints onto mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/personas", h.dispatchPersonas)
	mux.HandleFunc("/v1/personas/", h.dispatchPersonaItem)
	mux.HandleFunc("/v1/roles", h.dispatchRoles)
	mux.HandleFunc("/v1/roles/", h.dispatchRoleItem)
	mux.HandleFunc("/v1/sequences", h.dispatchSequences)
	mux.HandleFunc("/v1/sequences/", h.dispatchSequenceItem)
}

// ── Personas ────────────────────────────────────────────────────────────────

func (h *Handler) dispatchPersonas(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"personas": h.registry.ListPersonas()})
	case http.MethodPost:
		if !h.requireWrite(w, r) {
			return
		}
		var p Persona
		if !decodeJSON(w, r, &p) {
			return
		}
		created, err := h.registry.UpsertPersona(p) //nolint:contextcheck // Registry exposes contextless writes; persistence calls are bounded by the SQL driver and surfaced synchronously.
		if err != nil {
			writeUpsertError(w, "invalid_persona", err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (h *Handler) dispatchPersonaItem(w http.ResponseWriter, r *http.Request) {
	id := idFromPath(r.URL.Path, "/v1/personas/")
	if id == "" {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "persona id missing")
		return
	}
	switch r.Method {
	case http.MethodGet:
		p, err := h.registry.GetPersona(id)
		if err != nil {
			httpx.WriteError(w, http.StatusNotFound, "persona_not_found", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodPatch, http.MethodPut:
		if !h.requireWrite(w, r) {
			return
		}
		var p Persona
		if !decodeJSON(w, r, &p) {
			return
		}
		// URL id wins; body id is ignored if inconsistent.
		p.ID = id
		updated, err := h.registry.UpsertPersona(p) //nolint:contextcheck // Registry exposes contextless writes; persistence calls are bounded by the SQL driver and surfaced synchronously.
		if err != nil {
			writeUpsertError(w, "invalid_persona", err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if !h.requireWrite(w, r) {
			return
		}
		if err := h.registry.DeletePersona(id); err != nil { //nolint:contextcheck // Registry exposes contextless deletes; persistence calls are bounded by the SQL driver and surfaced synchronously.
			writeDeleteError(w, "persona_not_found", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodPut, http.MethodDelete)
	}
}

// ── Roles ───────────────────────────────────────────────────────────────────

func (h *Handler) dispatchRoles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"roles": h.registry.ListRoles()})
	case http.MethodPost:
		if !h.requireWrite(w, r) {
			return
		}
		var role Role
		if !decodeJSON(w, r, &role) {
			return
		}
		created, err := h.registry.UpsertRole(role) //nolint:contextcheck // Registry exposes contextless writes; persistence calls are bounded by the SQL driver and surfaced synchronously.
		if err != nil {
			writeUpsertError(w, "invalid_role", err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (h *Handler) dispatchRoleItem(w http.ResponseWriter, r *http.Request) {
	id := idFromPath(r.URL.Path, "/v1/roles/")
	if id == "" {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "role id missing")
		return
	}
	switch r.Method {
	case http.MethodGet:
		role, err := h.registry.GetRole(id)
		if err != nil {
			httpx.WriteError(w, http.StatusNotFound, "role_not_found", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, role)
	case http.MethodPatch, http.MethodPut:
		if !h.requireWrite(w, r) {
			return
		}
		var role Role
		if !decodeJSON(w, r, &role) {
			return
		}
		role.ID = id
		updated, err := h.registry.UpsertRole(role) //nolint:contextcheck // Registry exposes contextless writes; persistence calls are bounded by the SQL driver and surfaced synchronously.
		if err != nil {
			writeUpsertError(w, "invalid_role", err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if !h.requireWrite(w, r) {
			return
		}
		if err := h.registry.DeleteRole(id); err != nil { //nolint:contextcheck // Registry exposes contextless deletes; persistence calls are bounded by the SQL driver and surfaced synchronously.
			writeDeleteError(w, "role_not_found", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodPut, http.MethodDelete)
	}
}

// ── Sequences ───────────────────────────────────────────────────────────────

func (h *Handler) dispatchSequences(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"sequences": h.registry.ListSequences()})
	case http.MethodPost:
		if !h.requireWrite(w, r) {
			return
		}
		var s Sequence
		if !decodeJSON(w, r, &s) {
			return
		}
		created, err := h.registry.UpsertSequence(s) //nolint:contextcheck // Registry exposes contextless writes; persistence calls are bounded by the SQL driver and surfaced synchronously.
		if err != nil {
			writeUpsertError(w, "invalid_sequence", err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (h *Handler) dispatchSequenceItem(w http.ResponseWriter, r *http.Request) {
	id := idFromPath(r.URL.Path, "/v1/sequences/")
	if id == "" {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "sequence id missing")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s, err := h.registry.GetSequence(id)
		if err != nil {
			httpx.WriteError(w, http.StatusNotFound, "sequence_not_found", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, s)
	case http.MethodPatch, http.MethodPut:
		if !h.requireWrite(w, r) {
			return
		}
		var s Sequence
		if !decodeJSON(w, r, &s) {
			return
		}
		s.ID = id
		updated, err := h.registry.UpsertSequence(s) //nolint:contextcheck // Registry exposes contextless writes; persistence calls are bounded by the SQL driver and surfaced synchronously.
		if err != nil {
			writeUpsertError(w, "invalid_sequence", err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if !h.requireWrite(w, r) {
			return
		}
		if err := h.registry.DeleteSequence(id); err != nil { //nolint:contextcheck // Registry exposes contextless deletes; persistence calls are bounded by the SQL driver and surfaced synchronously.
			writeDeleteError(w, "sequence_not_found", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodPut, http.MethodDelete)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func (h *Handler) requireWrite(w http.ResponseWriter, r *http.Request) bool {
	if !h.allowWrites {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "writes_disabled",
			"this deployment is configured read-only for persona endpoints")
		return false
	}
	id := middleware.IdentityFromContext(r.Context())
	if id.Role != "admin" {
		httpx.WriteError(w, http.StatusForbidden, "admin_required",
			"persona writes require an admin identity")
		return false
	}
	return true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		httpx.WriteError(w, http.StatusBadRequest, "missing_body", "request body required")
		return false
	}
	defer func() { _ = r.Body.Close() }()
	body := http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB cap is generous for persona JSON
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
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

func methodNotAllowed(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
		"method not allowed on this resource")
}

func idFromPath(path, prefix string) string {
	stripped := strings.TrimPrefix(path, prefix)
	// Reject collection-only paths (e.g. /v1/personas/) + sub-resources that
	// don't match an item URL cleanly.
	if stripped == "" {
		return ""
	}
	// Sub-paths (/{id}/something) are out of scope; we only serve exact-item.
	if strings.Contains(stripped, "/") {
		return ""
	}
	return stripped
}
