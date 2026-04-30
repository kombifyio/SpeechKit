//go:build linux

package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	assistpkg "github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/config"
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
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
		_ = json.NewEncoder(w).Encode(serverSettingsSnapshot(app))
	})
}

func handleServerSettingsPatch(w http.ResponseWriter, r *http.Request, app *App) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
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
	if err := config.SaveServerModelSettings(path, next); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "settings_save_failed", "message": err.Error()},
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":           "saved",
		"restart_required": true,
		"message":          "saved; restart/recreate the server stack to apply model runtime changes",
		"desired":          config.SanitizeServerModelSettings(next),
		"settings":         serverSettingsSnapshot(app),
	})
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
				"configured": envPresent(cfg.HuggingFace.TokenEnv),
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
				"openai":      providerConfigured(cfg.Providers.OpenAI.Enabled, cfg.Providers.OpenAI.APIKeyEnv),
				"groq":        providerConfigured(cfg.Providers.Groq.Enabled, cfg.Providers.Groq.APIKeyEnv),
				"google":      providerConfigured(cfg.Providers.Google.Enabled, cfg.Providers.Google.APIKeyEnv),
				"open_router": providerConfigured(cfg.Providers.OpenRouter.Enabled, cfg.Providers.OpenRouter.APIKeyEnv),
			},
		},
		"voice_agent": map[string]any{
			"provider": effectiveVoiceAgentProvider(cfg),
			"gemini": map[string]any{
				"configured": providerConfigured(true, cfg.Providers.Google.APIKeyEnv)["configured"],
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
			"seeded":    len(cfg.Personas),
			"roles":     len(cfg.Roles),
			"sequences": len(cfg.Sequences),
		},
		"editable": map[string]any{
			"active":           active,
			"desired":          desired,
			"restart_required": restartRequired,
		},
	}
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

func providerConfigured(enabled bool, envName string) map[string]any {
	return map[string]any{
		"enabled":    enabled,
		"configured": envPresent(envName),
	}
}

func effectiveVoiceAgentProvider(cfg *config.Config) string {
	if cfg == nil {
		return "gemini"
	}
	if provider := strings.TrimSpace(cfg.VoiceAgent.Provider); provider != "" {
		return strings.ToLower(provider)
	}
	return "gemini"
}

func envBool(name string) bool {
	return envBoolDefault(name, false)
}

func envBoolDefault(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(strings.TrimSpace(name))))
	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func envPresent(name string) bool {
	return strings.TrimSpace(os.Getenv(strings.TrimSpace(name))) != ""
}

func activeServerModelSettings(cfg *config.Config) config.ServerModelSettings {
	if cfg == nil {
		cfg = &config.Config{}
	}
	return config.ServerModelSettings{
		Version:     1,
		Modes:       activeServerModeSettings(cfg),
		Credentials: activeServerCredentialSettings(cfg),
		Dictation: config.ServerDictationSettings{
			Dictionary: stringPtr(cfg.Vocabulary.Dictionary),
		},
		Assist: config.ServerAssistSettings{
			EnabledTools: activeAssistToolIDs(cfg),
		},
		STT: config.ServerSTTSettings{
			Enabled: boolPtr(cfg.VPS.Enabled),
			URL:     cfg.VPS.URL,
			Model:   firstNonEmpty(cfg.VPS.Model, "whisper-1"),
		},
		LLM: config.ServerLLMSettings{
			Enabled:      boolPtr(cfg.LocalLLM.Enabled),
			BaseURL:      cfg.LocalLLM.BaseURL,
			UtilityModel: cfg.LocalLLM.UtilityModel,
			AssistModel:  cfg.LocalLLM.AssistModel,
			AgentModel:   cfg.LocalLLM.AgentModel,
			HFRepo:       strings.TrimSpace(os.Getenv("SPEECHKIT_SELFHOSTED_LLM_REPO")),
		},
		VoiceAgent: config.ServerVoiceAgentSettings{
			Provider:       effectiveVoiceAgentProvider(cfg),
			PromptTemplate: stringPtr(cfg.VoiceAgent.FrameworkPrompt),
		},
		TTS: config.ServerOptionalTTSSettings{
			Enabled: boolPtr(cfg.TTS.Enabled),
		},
	}
}

func mergeServerModelSettings(base, patch config.ServerModelSettings) config.ServerModelSettings {
	base.Version = 1
	if patch.OnboardingComplete {
		base.OnboardingComplete = true
	}
	if value := strings.TrimSpace(patch.OnboardingVersion); value != "" {
		base.OnboardingVersion = value
	}
	base.Modes.Dictation = mergeServerModeSetting(base.Modes.Dictation, patch.Modes.Dictation)
	base.Modes.Assist = mergeServerModeSetting(base.Modes.Assist, patch.Modes.Assist)
	base.Modes.VoiceAgent = mergeServerModeSetting(base.Modes.VoiceAgent, patch.Modes.VoiceAgent)
	base.Credentials.OpenAI = mergeServerCredentialSetting(base.Credentials.OpenAI, patch.Credentials.OpenAI)
	base.Credentials.Groq = mergeServerCredentialSetting(base.Credentials.Groq, patch.Credentials.Groq)
	base.Credentials.Google = mergeServerCredentialSetting(base.Credentials.Google, patch.Credentials.Google)
	base.Credentials.HuggingFace = mergeServerCredentialSetting(base.Credentials.HuggingFace, patch.Credentials.HuggingFace)
	base.Credentials.OpenRouter = mergeServerCredentialSetting(base.Credentials.OpenRouter, patch.Credentials.OpenRouter)
	if patch.Dictation.Dictionary != nil {
		base.Dictation.Dictionary = patch.Dictation.Dictionary
	}
	if patch.Assist.EnabledTools != nil {
		base.Assist.EnabledTools = append([]string(nil), patch.Assist.EnabledTools...)
	}
	if patch.STT.Enabled != nil {
		base.STT.Enabled = patch.STT.Enabled
	}
	if value := strings.TrimSpace(patch.STT.URL); value != "" {
		base.STT.URL = value
	}
	if value := strings.TrimSpace(patch.STT.Model); value != "" {
		base.STT.Model = value
	}
	if patch.LLM.Enabled != nil {
		base.LLM.Enabled = patch.LLM.Enabled
	}
	if value := strings.TrimSpace(patch.LLM.BaseURL); value != "" {
		base.LLM.BaseURL = value
	}
	if value := strings.TrimSpace(patch.LLM.UtilityModel); value != "" {
		base.LLM.UtilityModel = value
	}
	if value := strings.TrimSpace(patch.LLM.AssistModel); value != "" {
		base.LLM.AssistModel = value
	}
	if value := strings.TrimSpace(patch.LLM.AgentModel); value != "" {
		base.LLM.AgentModel = value
	}
	if value := strings.TrimSpace(patch.LLM.HFRepo); value != "" {
		base.LLM.HFRepo = value
	}
	if value := strings.TrimSpace(patch.VoiceAgent.Provider); value != "" {
		base.VoiceAgent.Provider = strings.ToLower(value)
	}
	if patch.VoiceAgent.PromptTemplate != nil {
		base.VoiceAgent.PromptTemplate = patch.VoiceAgent.PromptTemplate
	}
	if patch.TTS.Enabled != nil {
		base.TTS.Enabled = patch.TTS.Enabled
	}
	return base
}

func serverModelSettingsEqual(a, b config.ServerModelSettings) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return bytes.Equal(aj, bj)
}

func ensureSingleJSONValue(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err == nil {
		return errors.New("request body must contain a single JSON object")
	} else {
		return err
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func stringPtr(v string) *string {
	return &v
}

func activeServerModeSettings(cfg *config.Config) config.ServerModeProviderSettings {
	return config.ServerModeProviderSettings{
		Dictation:  activeDictationModeSetting(cfg),
		Assist:     activeAssistModeSetting(cfg),
		VoiceAgent: activeVoiceAgentModeSetting(cfg),
	}
}

func activeDictationModeSetting(cfg *config.Config) config.ServerModeSetting {
	switch {
	case cfg.VPS.Enabled:
		return config.ServerModeSetting{ProviderKind: "local_built_in", ProfileID: "stt.local.whispercpp", Model: firstNonEmpty(cfg.VPS.Model, "whisper-1")}
	case cfg.HuggingFace.Enabled:
		return config.ServerModeSetting{ProviderKind: "cloud_provider", ProfileID: "stt.routed.whisper-large-v3", Model: cfg.HuggingFace.Model}
	case cfg.Providers.Ollama.Enabled:
		return config.ServerModeSetting{ProviderKind: "local_provider", ProfileID: "stt.ollama.gemma4-e4b-transcribe", Model: cfg.Providers.Ollama.STTModel}
	default:
		return config.ServerModeSetting{ProviderKind: "direct_provider", ProfileID: "stt.openai.whisper-1", Model: cfg.Providers.OpenAI.STTModel}
	}
}

func activeAssistModeSetting(cfg *config.Config) config.ServerModeSetting {
	switch {
	case cfg.LocalLLM.Enabled:
		return config.ServerModeSetting{ProviderKind: "local_built_in", ProfileID: "assist.builtin.gemma4-e4b", Model: firstNonEmpty(cfg.LocalLLM.AssistModel, cfg.LocalLLM.Model)}
	case cfg.Providers.Ollama.Enabled:
		return config.ServerModeSetting{ProviderKind: "local_provider", ProfileID: "assist.ollama.gemma4-e4b", Model: cfg.Providers.Ollama.AssistModel}
	case cfg.HuggingFace.Enabled:
		return config.ServerModeSetting{ProviderKind: "cloud_provider", ProfileID: "assist.routed.qwen35-27b", Model: cfg.HuggingFace.AssistModel}
	default:
		return config.ServerModeSetting{ProviderKind: "direct_provider", ProfileID: "assist.openai.gpt-5.4", Model: cfg.Providers.OpenAI.AssistModel}
	}
}

func activeVoiceAgentModeSetting(cfg *config.Config) config.ServerModeSetting {
	if effectiveVoiceAgentProvider(cfg) == "gemini" {
		return config.ServerModeSetting{ProviderKind: "direct_provider", ProfileID: "realtime.google.gemini-native-audio", Model: cfg.VoiceAgent.Model}
	}
	return config.ServerModeSetting{ProviderKind: "local_built_in", ProfileID: "realtime.builtin.pipeline", Model: firstNonEmpty(cfg.LocalLLM.AgentModel, cfg.LocalLLM.Model)}
}

func activeServerCredentialSettings(cfg *config.Config) config.ServerCredentialSettings {
	return config.ServerCredentialSettings{
		OpenAI:      activeCredential(cfg.Providers.OpenAI.Enabled, cfg.Providers.OpenAI.APIKeyEnv),
		Groq:        activeCredential(cfg.Providers.Groq.Enabled, cfg.Providers.Groq.APIKeyEnv),
		Google:      activeCredential(cfg.Providers.Google.Enabled, cfg.Providers.Google.APIKeyEnv),
		HuggingFace: activeCredential(cfg.HuggingFace.Enabled, config.HuggingFaceTokenEnvName(cfg)),
		OpenRouter:  activeCredential(cfg.Providers.OpenRouter.Enabled, cfg.Providers.OpenRouter.APIKeyEnv),
	}
}

func activeCredential(enabled bool, envName string) config.ServerProviderCredentialSettings {
	return config.ServerProviderCredentialSettings{
		Enabled: boolPtr(enabled),
		Env:     strings.TrimSpace(envName),
	}
}

func activeAssistToolIDs(cfg *config.Config) []string {
	if cfg != nil && cfg.Assist.EnabledTools != nil {
		return append([]string(nil), cfg.Assist.EnabledTools...)
	}
	return defaultAssistToolIDs()
}

func defaultAssistToolIDs() []string {
	registry := assistpkg.DefaultUtilityRegistry()
	defs := registry.List()
	ids := make([]string, 0, len(defs))
	for _, def := range defs {
		if def.Enabled {
			ids = append(ids, string(def.ID))
		}
	}
	return ids
}

func mergeServerModeSetting(base, patch config.ServerModeSetting) config.ServerModeSetting {
	if patch.Enabled != nil {
		base.Enabled = patch.Enabled
	}
	if value := strings.TrimSpace(patch.ProviderKind); value != "" {
		base.ProviderKind = value
	}
	if value := strings.TrimSpace(patch.ProfileID); value != "" {
		base.ProfileID = value
	}
	if value := strings.TrimSpace(patch.Model); value != "" {
		base.Model = value
	}
	return base
}

func mergeServerCredentialSetting(base, patch config.ServerProviderCredentialSettings) config.ServerProviderCredentialSettings {
	if patch.Enabled != nil {
		base.Enabled = patch.Enabled
	}
	if value := strings.TrimSpace(patch.Env); value != "" {
		base.Env = value
	}
	if value := strings.TrimSpace(patch.Value); value != "" {
		base.Value = value
	}
	return base
}

func serverProviderCatalog() map[string]any {
	modes := map[string][]framework.ProviderProfile{}
	for _, profile := range framework.DefaultProviderProfiles() {
		modes[string(profile.Mode)] = append(modes[string(profile.Mode)], profile)
	}
	return map[string]any{
		"provider_kinds": []framework.ProviderKind{
			framework.ProviderKindLocalBuiltIn,
			framework.ProviderKindLocalProvider,
			framework.ProviderKindCloudProvider,
			framework.ProviderKindDirectProvider,
		},
		"modes":        modes,
		"assist_tools": assistToolCatalog(),
	}
}

func assistToolCatalog() []map[string]any {
	defs := assistpkg.DefaultUtilityRegistry().List()
	tools := make([]map[string]any, 0, len(defs))
	for _, def := range defs {
		tools = append(tools, map[string]any{
			"id":              string(def.ID),
			"label":           def.Label,
			"input":           string(def.Input),
			"default_enabled": def.Enabled,
			"requires_model":  def.RequiresModel,
		})
	}
	return tools
}
