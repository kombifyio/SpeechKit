//go:build linux

package core

import (
	"os"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist/skills"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

func providerConfigured(cfg *config.Config, target string, enabled bool) map[string]any {
	status := config.ProviderCredentialStatusFor(cfg, target)
	return map[string]any{
		"enabled":    enabled,
		"configured": status.Available,
		"env":        status.EnvName,
	}
}

func effectiveVoiceAgentProvider(cfg *config.Config) string {
	if cfg == nil {
		return "assemblyai"
	}
	if provider := strings.TrimSpace(cfg.VoiceAgent.Provider); provider != "" {
		return normalizeVoiceAgentProvider(provider)
	}
	return "assemblyai"
}

func activeServerAuthSettings(cfg *config.Config) config.ServerAuthSettings {
	if cfg == nil {
		return config.ServerAuthSettings{}
	}
	mode := config.ServerAuthModeSelfManaged
	if strings.EqualFold(strings.TrimSpace(cfg.Server.AuthMode), "bearer") && envPresent(firstNonEmpty(cfg.Server.BearerTokenEnv, "SPEECHKIT_SERVER_TOKEN")) {
		mode = config.ServerAuthModeManagedBearer
	}
	return config.ServerAuthSettings{
		Mode:           mode,
		BearerTokenEnv: firstNonEmpty(cfg.Server.BearerTokenEnv, "SPEECHKIT_SERVER_TOKEN"),
	}
}

func voiceAgentProfileCatalog() []map[string]any {
	profiles := voiceagentprofile.BuiltInProfiles()
	out := make([]map[string]any, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, map[string]any{
			"id":           profile.ID,
			"display_name": profile.DisplayName,
			"description":  profile.Description,
			"voice":        profile.Voice,
			"built_in":     profile.BuiltIn,
		})
	}
	return out
}

func builtInVoiceAgentRoleCount() int {
	count := 0
	for _, profile := range voiceagentprofile.BuiltInProfiles() {
		if profile.RoleID != "" && profile.FrameworkPrompt != "" {
			count++
		}
	}
	return count
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
		Version:    1,
		ServerAuth: activeServerAuthSettings(cfg),
		AdminAuth: config.ServerAdminAuthSettings{
			Enabled:      boolPtr(cfg.Server.AdminAuthEnabled),
			Username:     strings.TrimSpace(cfg.Server.AdminUsername),
			PasswordHash: strings.TrimSpace(cfg.Server.AdminPasswordHash),
		},
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
			AgentProfileID: voiceagentprofile.NormalizeID(cfg.VoiceAgent.AgentProfileID),
			PromptTemplate: stringPtr(cfg.VoiceAgent.FrameworkPrompt),
		},
		TTS: config.ServerOptionalTTSSettings{
			Enabled: boolPtr(cfg.TTS.Enabled),
		},
	}
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
		return serverModeSettingFromProvider("local", framework.ModeDictation, firstNonEmpty(cfg.VPS.Model, "whisper-1"))
	case cfg.HuggingFace.Enabled:
		return serverModeSettingFromProvider("huggingface", framework.ModeDictation, cfg.HuggingFace.Model)
	case cfg.Providers.Ollama.Enabled:
		return serverModeSettingFromProvider("ollama", framework.ModeDictation, cfg.Providers.Ollama.STTModel)
	default:
		return serverModeSettingFromProvider("openai", framework.ModeDictation, cfg.Providers.OpenAI.STTModel)
	}
}

func activeAssistModeSetting(cfg *config.Config) config.ServerModeSetting {
	switch {
	case cfg.LocalLLM.Enabled:
		return serverModeSettingFromProvider("local", framework.ModeAssist, firstNonEmpty(cfg.LocalLLM.AssistModel, cfg.LocalLLM.Model))
	case cfg.Providers.Ollama.Enabled:
		return serverModeSettingFromProvider("ollama", framework.ModeAssist, cfg.Providers.Ollama.AssistModel)
	case cfg.HuggingFace.Enabled:
		return serverModeSettingFromProvider("huggingface", framework.ModeAssist, cfg.HuggingFace.AssistModel)
	default:
		return serverModeSettingFromProvider("openai", framework.ModeAssist, cfg.Providers.OpenAI.AssistModel)
	}
}

func activeVoiceAgentModeSetting(cfg *config.Config) config.ServerModeSetting {
	if cfg == nil {
		cfg = &config.Config{}
	}
	provider := effectiveVoiceAgentProvider(cfg)
	switch provider {
	case ProviderGemini:
		return serverModeSettingFromProvider("google", framework.ModeVoiceAgent, firstNonEmpty(cfg.VoiceAgent.Model, liveDefaultModel(provider)))
	case ProviderDeepgram:
		return serverModeSettingFromProvider("deepgram", framework.ModeVoiceAgent, firstNonEmpty(cfg.VoiceAgent.Model, liveDefaultModel(provider)))
	case ProviderAssemblyAI:
		return serverModeSettingFromProvider("assemblyai", framework.ModeVoiceAgent, firstNonEmpty(cfg.VoiceAgent.Model, liveDefaultModel(provider)))
	case ProviderOpenAI:
		return serverModeSettingFromProvider("openai", framework.ModeVoiceAgent, firstNonEmpty(cfg.Providers.OpenAI.RealtimeModel, cfg.VoiceAgent.Model, liveDefaultModel(provider)))
	}
	return serverModeSettingFromProvider("local", framework.ModeVoiceAgent, firstNonEmpty(cfg.LocalLLM.AgentModel, cfg.LocalLLM.Model))
}

func serverModeSettingFromProvider(provider string, mode framework.Mode, model string) config.ServerModeSetting {
	if profile, ok := findProviderProfileForModel(provider, mode, model); ok {
		return config.ServerModeSetting{
			ProviderKind: string(profile.ProviderKind),
			ProfileID:    profile.ID,
			Model:        firstNonEmpty(model, profile.ModelID),
		}
	}
	if profile, ok := catalog.FindProviderDefault(provider, mode); ok {
		return config.ServerModeSetting{
			ProviderKind: string(profile.ProviderKind),
			ProfileID:    profile.ProfileID,
			Model:        firstNonEmpty(model, profile.ModelID),
		}
	}
	return config.ServerModeSetting{Model: model}
}

func findProviderProfileForModel(provider string, mode framework.Mode, model string) (framework.ProviderProfile, bool) {
	provider = catalog.NormalizeProviderID(provider)
	mode = framework.NormalizeMode(mode)
	model = strings.TrimSpace(model)
	if provider == "" || mode == framework.ModeNone || model == "" {
		return framework.ProviderProfile{}, false
	}
	for _, profile := range catalog.DefaultProviderProfiles() {
		if catalog.NormalizeProviderID(profile.Provider) != provider {
			continue
		}
		if framework.NormalizeMode(profile.Mode) != mode {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(profile.ModelID), model) {
			return profile, true
		}
		for _, variant := range profile.Variants {
			if strings.EqualFold(strings.TrimSpace(variant.ModelID), model) {
				return profile, true
			}
		}
	}
	return framework.ProviderProfile{}, false
}

func activeServerCredentialSettings(cfg *config.Config) config.ServerCredentialSettings {
	return config.ServerCredentialSettings{
		OpenAI:      activeCredential(cfg.Providers.OpenAI.Enabled, config.ProviderCredentialEnvName(cfg, "openai")),
		Groq:        activeCredential(cfg.Providers.Groq.Enabled, config.ProviderCredentialEnvName(cfg, "groq")),
		Google:      activeCredential(cfg.Providers.Google.Enabled, config.ProviderCredentialEnvName(cfg, "google")),
		Deepgram:    activeCredential(cfg.Providers.Deepgram.Enabled, config.ProviderCredentialEnvName(cfg, "deepgram")),
		AssemblyAI:  activeCredential(cfg.Providers.AssemblyAI.Enabled, config.ProviderCredentialEnvName(cfg, "assemblyai")),
		HuggingFace: activeCredential(cfg.HuggingFace.Enabled, config.ProviderCredentialEnvName(cfg, "huggingface")),
		OpenRouter:  activeCredential(cfg.Providers.OpenRouter.Enabled, config.ProviderCredentialEnvName(cfg, "openrouter")),
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
	registry := skills.DefaultUtilityRegistry()
	defs := registry.List()
	ids := make([]string, 0, len(defs))
	for _, def := range defs {
		if def.Enabled {
			ids = append(ids, string(def.ID))
		}
	}
	return ids
}

func serverProviderCatalog() map[string]any {
	modes := map[string][]framework.ProviderProfile{}
	for _, profile := range catalog.DefaultProviderProfiles() {
		modes[string(profile.Mode)] = append(modes[string(profile.Mode)], profile)
	}
	return map[string]any{
		"provider_kinds": []framework.ProviderKind{
			framework.ProviderKindLocalBuiltIn,
			framework.ProviderKindLocalProvider,
			framework.ProviderKindCloudProvider,
			framework.ProviderKindDirectProvider,
		},
		"modes":             modes,
		"provider_matrix":   catalog.DefaultProviderMatrix(),
		"provider_defaults": catalog.DefaultProviderDefaults(),
		"assist_tools":      assistToolCatalog(),
	}
}

func assistToolCatalog() []map[string]any {
	defs := skills.DefaultUtilityRegistry().List()
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
