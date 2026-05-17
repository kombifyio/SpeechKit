//go:build linux

package core

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

func registerDeploymentStatus(app *App) {
	if app == nil || app.Mux == nil {
		return
	}
	app.Mux.HandleFunc("/v1/deployment/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !deploymentStatusAllowed(r) {
			httpx.WriteError(w, http.StatusForbidden, "deployment_status_forbidden", "deployment status requires a service bearer or admin identity")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		_ = json.NewEncoder(w).Encode(deploymentStatusSnapshot(app))
	})
}

func deploymentStatusAllowed(r *http.Request) bool {
	id := middleware.IdentityFromContext(r.Context())
	switch strings.TrimSpace(id.Source) {
	case "bearer", "basic", "admin_session", "none":
		return true
	case "edge_hmac":
		return strings.TrimSpace(id.Role) == "admin"
	default:
		return strings.TrimSpace(id.Role) == "admin"
	}
}

func deploymentStatusSnapshot(app *App) map[string]any {
	cfg := app.Cfg
	if cfg == nil {
		cfg = &config.Config{}
	}
	status, components, uptime := app.Health.Snapshot()
	bearerEnv := firstNonEmpty(cfg.Server.BearerTokenEnv, "SPEECHKIT_SERVER_TOKEN")
	edgeEnv := firstNonEmpty(cfg.Server.EdgeAuthSecretEnv, "EDGE_AUTH_SECRET")
	return map[string]any{
		"version":        app.Version,
		"status":         status,
		"uptime_seconds": uptime,
		"components":     components,
		"headless": map[string]any{
			"settings_path":  config.ServerSettingsPath(cfg),
			"settings_write": envBool(config.ServerSettingsWriteEnv),
			"onboarding_ui":  envBoolDefault(config.ServerOnboardingUIEnv, true),
		},
		"auth": map[string]any{
			"mode":                 strings.ToLower(strings.TrimSpace(cfg.Server.AuthMode)),
			"bearer_token_env":     bearerEnv,
			"bearer_token_set":     envPresent(bearerEnv),
			"bearer_role":          strings.TrimSpace(cfg.Server.BearerRole),
			"edge_auth_secret_env": edgeEnv,
			"edge_auth_secret_set": envPresent(edgeEnv),
			"admin_username":       strings.TrimSpace(cfg.Server.AdminUsername),
			"admin_password_set":   strings.TrimSpace(cfg.Server.AdminPasswordHash) != "",
		},
		"providers": map[string]any{
			"google": map[string]any{
				"api_key": envStatus(firstNonEmpty(cfg.Providers.Google.APIKeyEnv, config.GoogleAIAPIKeyEnv)),
				"stt_key": envStatus(config.GoogleSTTAPIKeyEnvName(cfg)),
			},
			"openai": map[string]any{
				"api_key": envStatus(firstNonEmpty(cfg.Providers.OpenAI.APIKeyEnv, "OPENAI_API_KEY")),
			},
			"groq": map[string]any{
				"api_key": envStatus(firstNonEmpty(cfg.Providers.Groq.APIKeyEnv, "GROQ_API_KEY")),
			},
			"huggingface": map[string]any{
				"token": envStatus(config.HuggingFaceTokenEnvName(cfg)),
			},
			"openrouter": map[string]any{
				"api_key": envStatus(firstNonEmpty(cfg.Providers.OpenRouter.APIKeyEnv, "OPENROUTER_API_KEY")),
			},
			// cloud_keys_present is the load-bearing flag for the install-E2E
			// local-only gate (install-e2e-linux.yml). True when ANY of the
			// configured cloud-provider env keys is set, false otherwise.
			"cloud_keys_present": anyCloudKeyEnvSet(cfg),
		},
		"store": map[string]any{
			"backend": strings.ToLower(strings.TrimSpace(cfg.Store.Backend)),
		},
		"modes": enabledModeNames(app),
	}
}

func envStatus(envName string) map[string]any {
	envName = strings.TrimSpace(envName)
	return map[string]any{
		"env":        envName,
		"configured": envPresent(envName),
	}
}
