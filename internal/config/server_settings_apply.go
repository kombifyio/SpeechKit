package config

import (
	"strings"

	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

func ApplyServerModelSettingsFile(cfg *Config) ([]string, error) {
	if cfg == nil {
		return nil, nil
	}
	path := ServerSettingsPath(cfg)
	settings, ok, err := LoadServerModelSettings(path)
	if err != nil || !ok {
		return nil, err
	}
	return ApplyServerModelSettings(cfg, settings), nil
}

func ApplyServerModelSettings(cfg *Config, settings ServerModelSettings) []string {
	if cfg == nil {
		return nil
	}
	var notes []string
	notes = append(notes, ApplyServerAuthSettings(cfg, settings.ServerAuth)...)
	notes = append(notes, ApplyServerAdminAuthSettings(cfg, settings.AdminAuth)...)
	notes = append(notes, applyServerCredentialSettings(cfg, settings.Credentials)...)
	notes = append(notes, applyServerModeProviderSettings(cfg, settings.Modes)...)
	if settings.Dictation.Dictionary != nil {
		cfg.Vocabulary.Dictionary = *settings.Dictation.Dictionary
		notes = append(notes, "server settings: dictation dictionary updated")
	}
	if settings.Assist.EnabledTools != nil {
		cfg.Assist.EnabledTools = append([]string(nil), settings.Assist.EnabledTools...)
		notes = append(notes, "server settings: Assist tools updated")
	}
	if settings.STT.Enabled != nil {
		cfg.VPS.Enabled = *settings.STT.Enabled
		notes = append(notes, "server settings: STT enabled updated")
	}
	if value := cleanSetting(settings.STT.URL); value != "" {
		cfg.VPS.URL = value
		notes = append(notes, "server settings: STT URL updated")
	}
	if value := cleanSetting(settings.STT.Model); value != "" {
		cfg.VPS.Model = value
		notes = append(notes, "server settings: STT model updated")
	}

	if settings.LLM.Enabled != nil {
		cfg.LocalLLM.Enabled = *settings.LLM.Enabled
		notes = append(notes, "server settings: LLM enabled updated")
	}
	if value := cleanSetting(settings.LLM.BaseURL); value != "" {
		cfg.LocalLLM.BaseURL = value
		notes = append(notes, "server settings: LLM base URL updated")
	}
	if value := cleanSetting(settings.LLM.UtilityModel); value != "" {
		cfg.LocalLLM.UtilityModel = normalizeServerLLMModel(value)
		notes = append(notes, "server settings: utility model updated")
	}
	if value := cleanSetting(settings.LLM.AssistModel); value != "" {
		cfg.LocalLLM.AssistModel = normalizeServerLLMModel(value)
		notes = append(notes, "server settings: assist model updated")
	}
	if value := cleanSetting(settings.LLM.AgentModel); value != "" {
		cfg.LocalLLM.AgentModel = normalizeServerLLMModel(value)
		notes = append(notes, "server settings: agent model updated")
	}

	if value := cleanSetting(settings.VoiceAgent.Provider); value != "" {
		cfg.VoiceAgent.Provider = strings.ToLower(value)
		notes = append(notes, "server settings: voice agent provider updated")
	}
	if value := cleanSetting(settings.VoiceAgent.AgentProfileID); value != "" {
		cfg.VoiceAgent.AgentProfileID = voiceagentprofile.NormalizeID(value)
		notes = append(notes, "server settings: voice agent profile updated")
	}
	if value := cleanSetting(settings.VoiceAgent.AgentSequenceID); value != "" {
		cfg.VoiceAgent.AgentSequenceID = value
		notes = append(notes, "server settings: voice agent sequence updated")
	}
	if settings.VoiceAgent.PromptTemplate != nil {
		cfg.VoiceAgent.FrameworkPrompt = *settings.VoiceAgent.PromptTemplate
		notes = append(notes, "server settings: voice agent prompt template updated")
	}
	if settings.TTS.Enabled != nil {
		cfg.TTS.Enabled = *settings.TTS.Enabled
		notes = append(notes, "server settings: TTS enabled updated")
	}
	EnableAlwaysOnLLM(cfg)
	return notes
}

func ApplyServerAdminAuthSettings(cfg *Config, auth ServerAdminAuthSettings) []string {
	if cfg == nil {
		return nil
	}
	var notes []string
	if auth.Enabled != nil {
		cfg.Server.AdminAuthEnabled = *auth.Enabled
		notes = append(notes, "server settings: admin login updated")
	}
	if value := cleanSetting(auth.Username); value != "" {
		cfg.Server.AdminUsername = value
		notes = append(notes, "server settings: admin username updated")
	}
	if value := cleanSetting(auth.PasswordHash); value != "" {
		cfg.Server.AdminPasswordHash = value
		notes = append(notes, "server settings: admin password updated")
	}
	return notes
}

func ApplyServerAuthSettings(cfg *Config, auth ServerAuthSettings) []string {
	if cfg == nil {
		return nil
	}
	var notes []string
	mode := cleanSetting(auth.Mode)
	envName := cleanSetting(auth.BearerTokenEnv)
	if envName == "" {
		envName = "SPEECHKIT_SERVER_TOKEN"
	}
	switch mode {
	case ServerAuthModeManagedBearer:
		cfg.Server.AuthMode = "bearer"
		cfg.Server.BearerTokenEnv = envName
		notes = append(notes, "server settings: bearer auth managed by setup")
	case ServerAuthModeSelfManaged:
		if cleanSetting(auth.BearerTokenEnv) != "" {
			cfg.Server.BearerTokenEnv = envName
			notes = append(notes, "server settings: bearer token env updated")
		}
	default:
		return notes
	}
	if value := cleanSetting(auth.TokenValue); value != "" {
		setCredentialEnv(cfg.Server.BearerTokenEnv, "SPEECHKIT_SERVER_TOKEN", value)
		notes = append(notes, "server settings: bearer token value loaded")
	}
	return notes
}

func applyServerCredentialSettings(cfg *Config, credentials ServerCredentialSettings) []string {
	var notes []string
	for _, entry := range []struct {
		target     string
		credential ServerProviderCredentialSettings
	}{
		{target: "openai", credential: credentials.OpenAI},
		{target: "groq", credential: credentials.Groq},
		{target: "google", credential: credentials.Google},
		{target: "deepgram", credential: credentials.Deepgram},
		{target: "assemblyai", credential: credentials.AssemblyAI},
		{target: "huggingface", credential: credentials.HuggingFace},
		{target: "openrouter", credential: credentials.OpenRouter},
	} {
		notes = append(notes, applyServerProviderCredential(cfg, entry.target, entry.credential)...)
	}
	return notes
}

func applyServerProviderCredential(cfg *Config, target string, credential ServerProviderCredentialSettings) []string {
	var notes []string
	target = NormalizeProviderCredentialTarget(target)
	if target == "" {
		return notes
	}
	label := ProviderLabel(target)
	if credential.Enabled != nil {
		if err := SetProviderEnabled(cfg, ProviderForCredentialTarget(target), *credential.Enabled); err == nil {
			notes = append(notes, "server settings: "+label+" enabled updated")
		}
	}
	if env := cleanSetting(credential.Env); env != "" {
		if err := SetProviderCredentialEnvName(cfg, target, env); err == nil {
			notes = append(notes, "server settings: "+label+" credential env updated")
		}
	}
	if value := cleanSetting(credential.Value); value != "" {
		setCredentialEnv(ProviderCredentialEnvName(cfg, target), ProviderCredentialEnvName(nil, target), value)
		notes = append(notes, "server settings: "+label+" credential value loaded")
	}
	return notes
}
