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
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/secrets"
	"github.com/kombifyio/SpeechKit/internal/serverclient"
)

type apiV1ServerConnectionPatch struct {
	Enabled           *bool                               `json:"enabled"`
	ActiveTargetID    *string                             `json:"activeTargetId"`
	URL               *string                             `json:"url"`
	BearerTokenEnv    *string                             `json:"bearerTokenEnv"`
	AuthMode          *string                             `json:"authMode"`
	FallbackToLocal   *bool                               `json:"fallbackToLocal"`
	RequestTimeoutSec *int                                `json:"requestTimeoutSec"`
	Targets           *[]apiV1ServerConnectionTargetPatch `json:"targets"`
	Token             *string                             `json:"token"`
}

type apiV1ServerConnectionTargetPatch struct {
	ID                string `json:"id"`
	Label             string `json:"label"`
	URL               string `json:"url"`
	BearerTokenEnv    string `json:"bearerTokenEnv"`
	AuthMode          string `json:"authMode"`
	FallbackToLocal   bool   `json:"fallbackToLocal"`
	RequestTimeoutSec int    `json:"requestTimeoutSec"`
}

type apiV1ServerConnectionSmokeRequest struct {
	URL               string `json:"url"`
	BearerTokenEnv    string `json:"bearerTokenEnv"`
	AuthMode          string `json:"authMode"`
	Token             string `json:"token"`
	RequestTimeoutSec int    `json:"requestTimeoutSec"`
}

type apiV1ServerConnectionSmokeResponse struct {
	OK           bool   `json:"ok"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	HealthStatus int    `json:"healthStatus,omitempty"`
	ReadyStatus  int    `json:"readyStatus,omitempty"`
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
	if patch.Targets != nil {
		cfg.ServerConnection.Targets = normalizeAPIV1ServerConnectionTargets(*patch.Targets)
	}
	if patch.ActiveTargetID != nil {
		activeID := strings.TrimSpace(*patch.ActiveTargetID)
		if activeID != "" {
			target, ok := findServerConnectionTarget(cfg.ServerConnection.Targets, activeID)
			if !ok {
				return apiV1UserError("server target not found")
			}
			cfg.ServerConnection.ActiveTargetID = activeID
			applyServerConnectionTarget(&cfg.ServerConnection, target)
		} else {
			cfg.ServerConnection.ActiveTargetID = ""
		}
	}
	if patch.URL != nil {
		cfg.ServerConnection.URL = strings.TrimSpace(*patch.URL)
	}
	if patch.AuthMode != nil {
		cfg.ServerConnection.AuthMode = config.NormalizeServerConnectionAuthMode(*patch.AuthMode)
	}
	cfg.ServerConnection.AuthMode = config.NormalizeServerConnectionAuthMode(cfg.ServerConnection.AuthMode)
	if patch.BearerTokenEnv != nil {
		env := strings.TrimSpace(*patch.BearerTokenEnv)
		if env == "" {
			env = defaultServerConnectionTokenEnv(cfg.ServerConnection.AuthMode)
		}
		cfg.ServerConnection.BearerTokenEnv = env
	}
	if strings.TrimSpace(cfg.ServerConnection.BearerTokenEnv) == "" {
		cfg.ServerConnection.BearerTokenEnv = defaultServerConnectionTokenEnv(cfg.ServerConnection.AuthMode)
	}
	if cfg.ServerConnection.RequestTimeoutSec <= 0 {
		cfg.ServerConnection.RequestTimeoutSec = 30
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
	if patch.Token != nil && strings.TrimSpace(*patch.Token) != "" {
		envName := strings.TrimSpace(cfg.ServerConnection.BearerTokenEnv)
		if envName == "" {
			envName = defaultServerConnectionTokenEnv(cfg.ServerConnection.AuthMode)
		}
		if err := secrets.SetNamedSecret(envName, strings.TrimSpace(*patch.Token)); err != nil {
			return err
		}
	}
	syncActiveServerConnectionTarget(&cfg.ServerConnection)
	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}
	return refreshServerDelegates(cfg, state)
}

func defaultServerConnectionTokenEnv(authMode string) string {
	return "SPEECHKIT_SERVER_TOKEN"
}

func normalizeAPIV1ServerConnectionTargets(raw []apiV1ServerConnectionTargetPatch) []config.ServerConnectionTargetConfig {
	targets := make([]config.ServerConnectionTargetConfig, 0, len(raw))
	for _, target := range raw {
		targets = append(targets, config.ServerConnectionTargetConfig{
			ID:                target.ID,
			Label:             target.Label,
			URL:               target.URL,
			BearerTokenEnv:    target.BearerTokenEnv,
			AuthMode:          target.AuthMode,
			FallbackToLocal:   target.FallbackToLocal,
			RequestTimeoutSec: target.RequestTimeoutSec,
		})
	}
	return normalizeServerConnectionTargets(targets)
}

func findServerConnectionTarget(targets []config.ServerConnectionTargetConfig, id string) (config.ServerConnectionTargetConfig, bool) {
	id = strings.TrimSpace(id)
	for _, target := range normalizeServerConnectionTargets(targets) {
		if target.ID == id {
			return target, true
		}
	}
	return config.ServerConnectionTargetConfig{}, false
}

func applyServerConnectionTarget(cfg *config.ServerConnectionConfig, target config.ServerConnectionTargetConfig) {
	if cfg == nil {
		return
	}
	targets := normalizeServerConnectionTargets([]config.ServerConnectionTargetConfig{target})
	if len(targets) == 0 {
		return
	}
	target = targets[0]
	cfg.URL = target.URL
	cfg.BearerTokenEnv = target.BearerTokenEnv
	cfg.AuthMode = config.NormalizeServerConnectionAuthMode(target.AuthMode)
	cfg.FallbackToLocal = target.FallbackToLocal
	cfg.RequestTimeoutSec = normalizedServerConnectionTimeout(target.RequestTimeoutSec)
}

func syncActiveServerConnectionTarget(cfg *config.ServerConnectionConfig) {
	if cfg == nil || strings.TrimSpace(cfg.URL) == "" {
		return
	}
	cfg.AuthMode = config.NormalizeServerConnectionAuthMode(cfg.AuthMode)
	if strings.TrimSpace(cfg.BearerTokenEnv) == "" {
		cfg.BearerTokenEnv = defaultServerConnectionTokenEnv(cfg.AuthMode)
	}
	cfg.RequestTimeoutSec = normalizedServerConnectionTimeout(cfg.RequestTimeoutSec)

	current := serverConnectionTargetFromActiveConfig(*cfg)
	targets := normalizeServerConnectionTargets(cfg.Targets)
	if strings.TrimSpace(cfg.ActiveTargetID) == "" {
		cfg.ActiveTargetID = current.ID
	}
	for i, target := range targets {
		if target.ID == cfg.ActiveTargetID {
			current.ID = target.ID
			targets[i] = current
			cfg.Targets = targets
			return
		}
	}
	targets = append(targets, current)
	cfg.Targets = targets
}

func handleAPIV1ServerConnectionSmoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req apiV1ServerConnectionSmokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	writeJSON(w, smokeAPIV1ServerConnection(r.Context(), req))
}

func smokeAPIV1ServerConnection(ctx context.Context, req apiV1ServerConnectionSmokeRequest) apiV1ServerConnectionSmokeResponse {
	timeout := time.Duration(normalizedServerConnectionTimeout(req.RequestTimeoutSec)) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	token := strings.TrimSpace(req.Token)
	if token == "" {
		token = strings.TrimSpace(config.ResolveSecret(req.BearerTokenEnv))
	}
	client, err := serverclient.New(serverclient.Options{
		BaseURL:        strings.TrimSpace(req.URL),
		BearerToken:    token,
		AuthMode:       req.AuthMode,
		RequestTimeout: timeout,
	})
	if err != nil {
		return apiV1ServerConnectionSmokeResponse{
			OK:      false,
			Status:  "failed",
			Message: err.Error(),
		}
	}
	probe, err := client.Probe(probeCtx)
	resp := apiV1ServerConnectionSmokeResponse{
		HealthStatus: probe.HealthStatus,
		ReadyStatus:  probe.ReadyStatus,
	}
	if err != nil {
		resp.OK = false
		resp.Status = "failed"
		resp.Message = err.Error()
		return resp
	}
	resp.OK = true
	resp.Status = "ok"
	resp.Message = "healthz and readyz passed"
	return resp
}
