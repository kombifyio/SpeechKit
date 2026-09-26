//go:build linux

package core

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/server/pairing"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

func registerServerSettings(app *App) {
	if app == nil || app.Mux == nil {
		return
	}
	app.Mux.HandleFunc("/v1/server/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPatch {
			w.Header().Set("Allow", "GET, HEAD, PATCH")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodPatch {
			handleServerSettingsPatch(w, r, app)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		snapshot := serverSettingsSnapshot(app)
		if !serverSettingsFullAccess(r) {
			snapshot = serverSettingsBootstrapSnapshot(snapshot)
		}
		_ = json.NewEncoder(w).Encode(snapshot)
	})
}

func handleServerSettingsPatch(w http.ResponseWriter, r *http.Request, app *App) {
	// Audit S-13: when the caller is using an admin-session cookie
	// (Source="admin_session"), require a matching X-CSRF-Token header.
	// Bearer / edge-HMAC / smoke / no-auth callers are exempt — they are
	// not subject to cross-site request forgery because their
	// credentials are not automatically attached by the browser.
	if !middleware.EnforceAdminCSRF(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !serverSettingsBootstrapWriteAllowed(app) && !serverSettingsAdminAccess(r) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "admin_required",
				"message": "server settings writes require an admin identity after bootstrap",
			},
		})
		return
	}
	if !envBool(config.ServerSettingsWriteEnv) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "settings_write_disabled",
				"message": config.ServerSettingsWriteEnv + " must be true to save server model settings",
			},
		})
		return
	}
	if !serverSettingsWriteAllowed(r, app) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "admin_required",
				"message": "server settings changes require an admin identity after bootstrap",
			},
		})
		return
	}

	var patch config.ServerModelSettings
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&patch); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "invalid_settings", "message": err.Error()},
		})
		return
	}
	if err := ensureSingleJSONValue(dec); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "invalid_settings", "message": err.Error()},
		})
		return
	}

	path := config.ServerSettingsPath(app.Cfg)
	base := activeServerModelSettings(app.Cfg)
	if existing, ok, err := config.LoadServerModelSettings(path); err == nil && ok {
		base = existing
	} else if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "settings_load_failed", "message": err.Error()},
		})
		return
	}
	next := mergeServerModelSettings(base, patch)
	if next.OnboardingComplete {
		next.OnboardingVersion = app.Version
	}
	generatedToken := ""
	if shouldGenerateServerToken(patch.ServerAuth) {
		token, err := generateServerBearerToken()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "token_generation_failed", "message": err.Error()},
			})
			return
		}
		generatedToken = token
		next.ServerAuth.Mode = config.ServerAuthModeManagedBearer
		if strings.TrimSpace(next.ServerAuth.BearerTokenEnv) == "" {
			next.ServerAuth.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"
		}
		next.ServerAuth.TokenValue = token
	}
	if err := config.SaveServerModelSettings(path, next); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "settings_save_failed", "message": err.Error()},
		})
		return
	}
	runtimeAuth := next.ServerAuth
	if stored, ok, err := config.LoadServerModelSettings(path); err == nil && ok {
		next = stored
	} else if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "settings_load_failed", "message": err.Error()},
		})
		return
	}
	applyRuntimeServerAuth(app, runtimeAuth)
	applyRuntimeServerAdminAuth(app, next.AdminAuth)

	fullAccess := serverSettingsFullAccess(r)
	desired := serverSettingsResponseDesired(next)
	if !fullAccess {
		desired = serverSettingsBootstrapDesired(next)
	}
	response := map[string]any{
		"status":           "saved",
		"restart_required": true,
		"desired":          desired,
	}
	if fullAccess {
		response["message"] = "saved; restart/recreate the server stack to apply model runtime changes"
		response["settings"] = serverSettingsSnapshot(app)
	}
	if generatedToken != "" {
		response["generated_token"] = map[string]any{
			"token":       generatedToken,
			"env":         firstNonEmpty(next.ServerAuth.BearerTokenEnv, "SPEECHKIT_SERVER_TOKEN"),
			"auth_mode":   "bearer",
			"header_name": "Authorization",
		}
		if payload, err := pairing.New(pairingServerURL(app, r), generatedToken, pairingInstanceName(app.Cfg)); err == nil {
			response["pairing"] = payload
			if raw, err := json.Marshal(payload); err == nil {
				if svg, err := pairing.QRSVG(string(raw)); err == nil {
					response["pairing_qr_svg"] = svg
				}
			}
		}
	}
	if strings.TrimSpace(patch.AdminAuth.PasswordValue) != "" {
		now := time.Now()
		secure := serverRequestIsSecure(app, r)
		if cookie, err := middleware.NewAdminSessionCookie(app.Cfg.Server.AdminUsername, app.Cfg.Server.AdminPasswordHash, secure, now); err == nil && cookie != nil {
			http.SetCookie(w, cookie)
			// Audit S-13: pair the session cookie with the CSRF
			// double-submit cookie so the SPA can immediately make
			// state-changing admin calls without a separate roundtrip.
			if csrf := middleware.NewAdminCSRFCookie(cookie.Value, secure, now); csrf != nil {
				http.SetCookie(w, csrf)
			}
		}
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

func serverSettingsSnapshot(app *App) map[string]any {
	cfg := app.Cfg
	if cfg == nil {
		cfg = &config.Config{}
	}

	providers := []string{}
	if app.STTRouter != nil {
		providers = app.STTRouter.AvailableProviders()
	}
	overall, components, uptime := app.Health.Snapshot()
	active := activeServerModelSettings(cfg)
	desired := active
	settingsPath := config.ServerSettingsPath(cfg)
	settingsPersisted := false
	completedVersion := ""
	if stored, ok, err := config.LoadServerModelSettings(settingsPath); err == nil && ok {
		desired = config.SanitizeServerModelSettings(stored)
		completedVersion = strings.TrimSpace(desired.OnboardingVersion)
		desired.OnboardingComplete = desired.OnboardingComplete && completedVersion == strings.TrimSpace(app.Version)
		active.OnboardingComplete = desired.OnboardingComplete
		active.OnboardingVersion = desired.OnboardingVersion
		settingsPersisted = true
	}
	restartRequired := !serverModelSettingsEqual(active, desired)
	onboardingUI := envBoolDefault(config.ServerOnboardingUIEnv, true)

	return map[string]any{
		"version":        app.Version,
		"modes":          enabledModeNames(app),
		"status":         overall,
		"uptime_seconds": uptime,
		"components":     components,
		"catalog":        serverProviderCatalog(),
		"onboarding": map[string]any{
			"enabled":                onboardingUI,
			"complete":               desired.OnboardingComplete,
			"required":               onboardingUI && !desired.OnboardingComplete,
			"current_deploy_version": app.Version,
			"completed_version":      completedVersion,
		},
		"runtime": map[string]any{
			"self_hosted_defaults": envBool(config.ServerSelfHostedDefaultsEnv),
			"model_dir":            cfg.Server.ModelDir,
			"onboarding_ui":        onboardingUI,
			"settings_write":       envBool(config.ServerSettingsWriteEnv),
			"settings_persisted":   settingsPersisted,
			"restart_required":     restartRequired,
		},
		"assistant_ui": map[string]any{
			"enabled":            envBoolDefault(config.ServerAssistantUIEnv, true),
			"variant":            config.NormalizeAssistantVariant(cfg.Server.AssistantUI.Variant),
			"mark":               config.NormalizeAssistantMark(cfg.Server.AssistantUI.Mark),
			"transcript_default": cfg.Server.AssistantUI.TranscriptDefault,
		},
		"auth": map[string]any{
			"mode":               cfg.Server.AuthMode,
			"bearer_token_env":   firstNonEmpty(cfg.Server.BearerTokenEnv, "SPEECHKIT_SERVER_TOKEN"),
			"bearer_token_set":   envPresent(firstNonEmpty(cfg.Server.BearerTokenEnv, "SPEECHKIT_SERVER_TOKEN")),
			"admin_auth_enabled": cfg.Server.AdminAuthEnabled,
			"admin_username":     cfg.Server.AdminUsername,
			"admin_password_set": strings.TrimSpace(cfg.Server.AdminPasswordHash) != "",
		},
		"stt": map[string]any{
			"strategy":  cfg.Routing.Strategy,
			"providers": providers,
			"self_hosted": map[string]any{
				"enabled": cfg.VPS.Enabled,
				"url":     cfg.VPS.URL,
				"model":   firstNonEmpty(cfg.VPS.Model, "whisper-1"),
			},
			"huggingface": map[string]any{
				"enabled":    cfg.HuggingFace.Enabled,
				"model":      cfg.HuggingFace.Model,
				"configured": config.ProviderCredentialStatusFor(cfg, "huggingface").Available,
			},
		},
		"llm": map[string]any{
			"local": map[string]any{
				"enabled":      cfg.LocalLLM.Enabled,
				"base_url":     cfg.LocalLLM.BaseURL,
				"utility":      cfg.LocalLLM.UtilityModel,
				"assist":       cfg.LocalLLM.AssistModel,
				"voice_agent":  cfg.LocalLLM.AgentModel,
				"flow_ready":   app.AssistFlow != nil || app.AgentFlow != nil,
				"runtime_name": "llama.cpp",
			},
			"cloud": map[string]any{
				"openai":      providerConfigured(cfg, "openai", cfg.Providers.OpenAI.Enabled),
				"groq":        providerConfigured(cfg, "groq", cfg.Providers.Groq.Enabled),
				"google":      providerConfigured(cfg, "google", cfg.Providers.Google.Enabled),
				"open_router": providerConfigured(cfg, "openrouter", cfg.Providers.OpenRouter.Enabled),
			},
		},
		"voice_agent": map[string]any{
			"provider":         effectiveVoiceAgentProvider(cfg),
			"agent_profile_id": voiceagentprofile.NormalizeID(cfg.VoiceAgent.AgentProfileID),
			"agent_profiles":   voiceAgentProfileCatalog(),
			"gemini": map[string]any{
				"configured": providerConfigured(cfg, "google", true)["configured"],
				"model":      cfg.VoiceAgent.Model,
				"fallback":   cfg.VoiceAgent.FallbackModel,
			},
			"cascaded": map[string]any{
				"stt_ready":   app.STTRouter != nil,
				"agent_ready": app.AgentFlow != nil,
				"tts_ready":   app.TTSEnabled,
			},
		},
		"tts": map[string]any{
			"enabled": cfg.TTS.Enabled,
			"ready":   app.TTSEnabled,
			"mode":    "optional",
		},
		"personas": map[string]any{
			"seeded":    len(cfg.Personas) + len(voiceagentprofile.BuiltInProfiles()),
			"roles":     len(cfg.Roles) + builtInVoiceAgentRoleCount(),
			"sequences": len(cfg.Sequences),
		},
		"editable": map[string]any{
			"active":           serverSettingsResponseDesired(active),
			"desired":          serverSettingsResponseDesired(desired),
			"restart_required": restartRequired,
		},
	}
}

func serverSettingsAdminAccess(r *http.Request) bool {
	if r == nil {
		return false
	}
	return middleware.IdentityFromContext(r.Context()).Role == "admin"
}

func serverSettingsFullAccess(r *http.Request) bool {
	if r == nil {
		return false
	}
	switch middleware.IdentityFromContext(r.Context()).Source {
	case "bearer", "edge_hmac", "admin_session":
		return true
	default:
		return false
	}
}

func serverSettingsWriteAllowed(r *http.Request, app *App) bool {
	if r != nil {
		id := middleware.IdentityFromContext(r.Context())
		if id.Source == "admin_session" || strings.EqualFold(strings.TrimSpace(id.Role), "admin") {
			return true
		}
		if id.Source != "" && id.Source != "none" {
			return false
		}
	}
	return serverSettingsBootstrapWriteAllowed(app)
}

func serverSettingsBootstrapSnapshot(full map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"version", "status", "onboarding", "catalog", "assistant_ui"} {
		if value, ok := full[key]; ok {
			out[key] = value
		}
	}
	if editable, ok := full["editable"].(map[string]any); ok {
		if desired, ok := editable["desired"].(config.ServerModelSettings); ok {
			out["editable"] = map[string]any{
				"desired": serverSettingsBootstrapDesired(desired),
			}
		}
	}
	if runtime, ok := full["runtime"].(map[string]any); ok {
		out["runtime"] = map[string]any{
			"settings_write": runtime["settings_write"],
		}
	}
	return out
}

func serverSettingsBootstrapDesired(settings config.ServerModelSettings) config.ServerModelSettings {
	settings = serverSettingsResponseDesired(settings)
	settings.ServerAuth.BearerTokenEnv = ""
	settings.AdminAuth.PasswordHash = ""
	settings.Credentials = config.ServerCredentialSettings{}
	settings.STT.URL = ""
	settings.LLM.BaseURL = ""
	settings.Dictation.Dictionary = nil
	settings.VoiceAgent.PromptTemplate = nil
	return settings
}

func serverSettingsResponseDesired(settings config.ServerModelSettings) config.ServerModelSettings {
	settings = config.SanitizeServerModelSettings(settings)
	settings.AdminAuth.PasswordHash = ""
	settings.AdminAuth.PasswordValue = ""
	return settings
}

func enabledModeNames(app *App) []string {
	if app == nil || len(app.Modes) == 0 {
		return []string{string(ModeDictation), string(ModeAssist), string(ModeVoiceAgent)}
	}
	modes := make([]string, 0, 3)
	for _, mode := range []Mode{ModeDictation, ModeAssist, ModeVoiceAgent} {
		if app.Modes[mode] {
			modes = append(modes, string(mode))
		}
	}
	return modes
}

func serverSettingsBootstrapWriteAllowed(app *App) bool {
	if app == nil {
		return false
	}
	if !envBool(config.ServerSettingsWriteEnv) {
		return false
	}
	// Once any final post-onboarding state has been observed for this
	// process, the bootstrap window stays closed. Out-of-band tampering
	// with the settings file (deletion, downgrade) cannot reopen it
	// until a fresh process starts.
	if app.bootstrapSealed.Load() {
		return false
	}
	settingsPath := config.ServerSettingsPath(app.Cfg)
	stored, ok, err := config.LoadServerModelSettings(settingsPath)
	if err != nil {
		return false
	}
	if ok && stored.OnboardingComplete && strings.TrimSpace(stored.OnboardingVersion) == strings.TrimSpace(app.Version) {
		app.bootstrapSealed.Store(true)
		return false
	}
	if !serverAdminConfigured(app) {
		return true
	}
	return false
}

func serverAdminConfigured(app *App) bool {
	if app == nil {
		return false
	}
	if app.Cfg != nil && !app.Cfg.Server.AdminAuthEnabled {
		return false
	}
	if app.AuthState != nil && strings.TrimSpace(app.AuthState.AdminPasswordHash()) != "" {
		return true
	}
	return app.Cfg != nil && app.Cfg.Server.AdminAuthEnabled && strings.TrimSpace(app.Cfg.Server.AdminPasswordHash) != ""
}
