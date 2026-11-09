//go:build linux

package core

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/kombifyio/SpeechKit/app/internal/config"
	"github.com/kombifyio/SpeechKit/app/internal/server/cloudlink"
	"github.com/kombifyio/SpeechKit/app/internal/server/discovery"
	"github.com/kombifyio/SpeechKit/app/internal/server/middleware"
)

// Operator endpoints of the account link. All require an admin identity
// (the /setup admin session with its CSRF header for writes, or a bearer
// with bearer_role = "admin"); a device bearer can neither link nor unlink.
const (
	cloudLinkPath       = "/v1/server/cloud-link"
	cloudLinkStartPath  = "/v1/server/cloud-link/start"
	cloudLinkUnlinkPath = "/v1/server/cloud-link/unlink"
)

func init() {
	optionalWiring = append(optionalWiring, wireCloudLink)
}

func wireCloudLink(cfg *config.Config, app *App) {
	if app == nil || app.Mux == nil {
		return
	}
	settings, resolveErr := cloudlink.Resolve(cfg)
	if cfg != nil && cfg.Server.CloudLink.Enabled {
		// The id exists before any link, so an announce-only sidecar that
		// shares the state directory announces the same id= from the start.
		if _, err := discovery.EnsureInstanceID(config.ServerStateDir(cfg)); err != nil {
			slog.Warn("cloud link: no instance id", "err", err)
		}
	}
	if resolveErr != nil && !errors.Is(resolveErr, cloudlink.ErrDisabled) {
		slog.Warn("cloud link unavailable", "reason", resolveErr.Error())
	}
	app.CloudLink = cloudlink.NewManager(settings, resolveErr, app.Version, func() cloudlink.Report {
		return cloudlink.ReportFromConfig(app.Cfg, app.Version, enabledModeNames(app))
	})
	app.Mux.HandleFunc(cloudLinkPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeCloudLinkJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": map[string]any{"code": "method_not_allowed"}})
			return
		}
		if !cloudLinkAdmin(w, r) {
			return
		}
		writeCloudLinkJSON(w, http.StatusOK, app.CloudLink.Status())
	})
	app.Mux.HandleFunc(cloudLinkStartPath, func(w http.ResponseWriter, r *http.Request) {
		if !cloudLinkWrite(w, r) {
			return
		}
		if _, err := app.CloudLink.StartInBackground(r.Context()); err != nil {
			writeCloudLinkJSON(w, http.StatusConflict, map[string]any{
				"error":  map[string]any{"code": "cloud_link_start_failed", "message": err.Error()},
				"status": app.CloudLink.Status(),
			})
			return
		}
		writeCloudLinkJSON(w, http.StatusAccepted, app.CloudLink.Status())
	})
	app.Mux.HandleFunc(cloudLinkUnlinkPath, func(w http.ResponseWriter, r *http.Request) {
		if !cloudLinkWrite(w, r) {
			return
		}
		res, err := app.CloudLink.Unlink(r.Context())
		if err != nil {
			writeCloudLinkJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]any{"code": "cloud_link_unlink_failed", "message": err.Error()},
			})
			return
		}
		writeCloudLinkJSON(w, http.StatusOK, map[string]any{"unlink": res, "status": app.CloudLink.Status()})
	})
}

func cloudLinkWrite(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeCloudLinkJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": map[string]any{"code": "method_not_allowed"}})
		return false
	}
	if !cloudLinkAdmin(w, r) {
		return false
	}
	return middleware.EnforceAdminCSRF(w, r)
}

func cloudLinkAdmin(w http.ResponseWriter, r *http.Request) bool {
	if serverSettingsAdminAccess(r) {
		return true
	}
	writeCloudLinkJSON(w, http.StatusForbidden, map[string]any{
		"error": map[string]any{"code": "admin_required", "message": "the account link requires an admin identity"},
	})
	return false
}

func writeCloudLinkJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
