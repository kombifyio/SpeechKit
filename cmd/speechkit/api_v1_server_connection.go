package main

// api_v1_server_connection.go exposes the [server_connection] device-target
// section through the same /api/v1/* control plane the React UI consumes.
// GET returns the current settings (with the bearer token elided — only
// the env var name + "is the env var set?" boolean travel over this
// boundary). PATCH accepts partial updates with the same trust boundary
// rules as the rest of the control plane: invalid values clamp to safe
// defaults instead of being rejected outright, so a frontend bug never
// blocks the user.

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
)

type apiV1ServerConnectionPatch struct {
	Enabled           *bool   `json:"enabled"`
	URL               *string `json:"url"`
	BearerTokenEnv    *string `json:"bearerTokenEnv"`
	FallbackToLocal   *bool   `json:"fallbackToLocal"`
	RequestTimeoutSec *int    `json:"requestTimeoutSec"`
}

func handleAPIV1ServerConnection(w http.ResponseWriter, r *http.Request, cfgPath string, cfg *config.Config, state *appState) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, serverConnectionSettingFromConfig(cfg.ServerConnection))
	case http.MethodPatch:
		var patch apiV1ServerConnectionPatch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := applyAPIV1ServerConnectionPatch(cfgPath, cfg, state, patch); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, serverConnectionSettingFromConfig(cfg.ServerConnection))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func applyAPIV1ServerConnectionPatch(cfgPath string, cfg *config.Config, state *appState, patch apiV1ServerConnectionPatch) error {
	if cfg == nil {
		return apiV1UserError("config unavailable")
	}
	if patch.Enabled != nil {
		cfg.ServerConnection.Enabled = *patch.Enabled || anyServerModeSelected(cfg)
	}
	if patch.URL != nil {
		cfg.ServerConnection.URL = strings.TrimSpace(*patch.URL)
	}
	if patch.BearerTokenEnv != nil {
		env := strings.TrimSpace(*patch.BearerTokenEnv)
		if env == "" {
			env = "SPEECHKIT_SERVER_TOKEN"
		}
		cfg.ServerConnection.BearerTokenEnv = env
	}
	if patch.FallbackToLocal != nil {
		cfg.ServerConnection.FallbackToLocal = *patch.FallbackToLocal
	}
	if patch.RequestTimeoutSec != nil {
		// Clamp to a sane range. 0 means "no client-side timeout"; values
		// beyond an hour smell like a bug, not intent.
		v := *patch.RequestTimeoutSec
		if v < 0 {
			v = 0
		} else if v > 3600 {
			v = 3600
		}
		cfg.ServerConnection.RequestTimeoutSec = v
	}
	config.ApplyManagedDevServerDefaults(cfg)
	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}
	return refreshServerDelegates(cfg, state)
}
