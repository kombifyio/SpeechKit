//go:build linux

package configapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
)

type Handler struct {
	cfg         *config.Config
	version     string
	readyStatus func() string
}

func New(cfg *config.Config, version string, readyStatus func() string) *Handler {
	return &Handler{cfg: cfg, version: version, readyStatus: readyStatus}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/config", h.config)
}

func (h *Handler) config(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on this resource")
		return
	}
	overall := "unavailable"
	if h.readyStatus != nil {
		overall = h.readyStatus()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":       h.version,
		"public_url":    strings.TrimRight(strings.TrimSpace(h.cfg.Server.PublicURL), "/"),
		"modes":         h.cfg.Server.Modes,
		"auth_mode":     h.cfg.Server.AuthMode,
		"bearer_role":   h.cfg.Server.BearerRole,
		"max_upload_mb": h.cfg.Server.MaxUploadMB,
		"features": map[string]bool{
			"catalog":       h.cfg.Server.Features.Catalog,
			"storage_reads": h.cfg.Server.Features.StorageReads,
			"vocabulary":    h.cfg.Server.Features.Vocabulary,
			"tts_direct":    h.cfg.Server.Features.TTSDirect,
		},
		"ready_status": overall,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
