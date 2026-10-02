package config

import (
	"fmt"
	"net/url"
	"strings"
)

func validateServerModelSettings(settings ServerModelSettings) error {
	if err := validateServerSettingsStringLengths(settings); err != nil {
		return err
	}
	if err := validateServerAuthSettings(settings.ServerAuth); err != nil {
		return err
	}
	if err := validateServerAdminAuthSettings(settings.AdminAuth); err != nil {
		return err
	}
	if err := validateServerAssistSettings(settings.Assist); err != nil {
		return err
	}
	if err := validateServerProviderURLs(settings); err != nil {
		return err
	}
	if err := validateServerVoiceAgentProvider(settings.VoiceAgent.Provider); err != nil {
		return err
	}
	if err := validateServerModeProviderKinds(settings.Modes); err != nil {
		return err
	}
	return validateServerCredentialSettings(settings.Credentials)
}

type namedServerSetting struct {
	name  string
	value string
	limit int
}

func validateServerSettingsStringLengths(settings ServerModelSettings) error {
	fields := []namedServerSetting{
		{"server_auth.mode", settings.ServerAuth.Mode, 512},
		{"server_auth.env", settings.ServerAuth.BearerTokenEnv, 512},
		{"server_auth.token_value", settings.ServerAuth.TokenValue, 4096},
		{"admin_auth.user", settings.AdminAuth.Username, 512},
		{"admin_auth.hash", settings.AdminAuth.PasswordHash, 512},
		{"admin_auth.password", settings.AdminAuth.PasswordValue, 4096},
		{"stt.url", settings.STT.URL, 512},
		{"stt.model", settings.STT.Model, 512},
		{"llm.base_url", settings.LLM.BaseURL, 512},
		{"llm.utility_model", settings.LLM.UtilityModel, 512},
		{"llm.assist_model", settings.LLM.AssistModel, 512},
		{"llm.agent_model", settings.LLM.AgentModel, 512},
		{"llm.hf_repo", settings.LLM.HFRepo, 512},
		{"voice.provider", settings.VoiceAgent.Provider, 512},
		{"voice.agent", settings.VoiceAgent.AgentProfileID, 512},
		{"dictation.profile", settings.Modes.Dictation.ProfileID, 512},
		{"dictation.model", settings.Modes.Dictation.Model, 512},
		{"assist.profile", settings.Modes.Assist.ProfileID, 512},
		{"assist.model", settings.Modes.Assist.Model, 512},
		{"voice.profile", settings.Modes.VoiceAgent.ProfileID, 512},
		{"voice.model", settings.Modes.VoiceAgent.Model, 512},
		{"openai.value", settings.Credentials.OpenAI.Value, 4096},
		{"groq.value", settings.Credentials.Groq.Value, 4096},
		{"google.value", settings.Credentials.Google.Value, 4096},
		{"deepgram.value", settings.Credentials.Deepgram.Value, 4096},
		{"assemblyai.value", settings.Credentials.AssemblyAI.Value, 4096},
		{"huggingface.value", settings.Credentials.HuggingFace.Value, 4096},
		{"openrouter.value", settings.Credentials.OpenRouter.Value, 4096},
	}
	for _, field := range fields {
		if len(field.value) > field.limit {
			return fmt.Errorf("%s is too long", field.name)
		}
	}
	if settings.Dictation.Dictionary != nil && len(*settings.Dictation.Dictionary) > 8192 {
		return fmt.Errorf("dictation.dictionary is too long")
	}
	if settings.VoiceAgent.PromptTemplate != nil && len(*settings.VoiceAgent.PromptTemplate) > 8192 {
		return fmt.Errorf("voice_agent.prompt_template is too long")
	}
	return nil
}

func validateServerAuthSettings(settings ServerAuthSettings) error {
	switch strings.ToLower(strings.TrimSpace(settings.Mode)) {
	case "", ServerAuthModeManagedBearer, ServerAuthModeSelfManaged:
	default:
		return fmt.Errorf("server_auth.mode must be managed_bearer or self_managed")
	}
	if settings.BearerTokenEnv != "" && !validEnvName(settings.BearerTokenEnv) {
		return fmt.Errorf("server_auth.bearer_token_env must be a valid environment variable name")
	}
	return nil
}

func validateServerAdminAuthSettings(settings ServerAdminAuthSettings) error {
	enabled := settings.Enabled != nil && *settings.Enabled
	hasPassword := settings.PasswordHash != "" || settings.PasswordValue != ""
	if enabled && !hasPassword {
		return fmt.Errorf("admin_auth.password is required when admin auth is enabled")
	}
	if (hasPassword || enabled) && settings.Username == "" {
		return fmt.Errorf("admin_auth.username is required when an admin password is configured")
	}
	return nil
}

func validateServerAssistSettings(settings ServerAssistSettings) error {
	for _, tool := range settings.EnabledTools {
		if !validServerToolID(tool) {
			return fmt.Errorf("assist.enabled_tools contains an invalid tool id")
		}
	}
	return nil
}

func validateServerProviderURLs(settings ServerModelSettings) error {
	for _, field := range []namedServerSetting{
		{"stt.url", settings.STT.URL, 0},
		{"llm.base_url", settings.LLM.BaseURL, 0},
	} {
		if err := validateServerProviderURL(field.name, field.value); err != nil {
			return err
		}
	}
	return nil
}

func validateServerProviderURL(name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		return fmt.Errorf("%s must start with http:// or https://", name)
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("%s must be a valid URL", name)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not contain user-info", name)
	}
	return nil
}

func validateServerVoiceAgentProvider(provider string) error {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "cascaded", "gemini", "google", "openai", "deepgram", "assemblyai", "kombify-agent", "moshi":
		return nil
	default:
		return fmt.Errorf("voice_agent.provider must be cascaded, gemini/google, openai, deepgram, assemblyai, kombify-agent, or moshi")
	}
}

func validateServerModeProviderKinds(settings ServerModeProviderSettings) error {
	for _, field := range []namedServerSetting{
		{"dictation.provider_kind", settings.Dictation.ProviderKind, 0},
		{"assist.provider_kind", settings.Assist.ProviderKind, 0},
		{"voice.provider_kind", settings.VoiceAgent.ProviderKind, 0},
	} {
		if !validServerProviderKind(field.value) {
			return fmt.Errorf("%s must be local_built_in, local_provider, cloud_provider, or direct_provider", field.name)
		}
	}
	return nil
}

func validateServerCredentialSettings(settings ServerCredentialSettings) error {
	for _, field := range []namedServerSetting{
		{"openai.env", settings.OpenAI.Env, 0},
		{"groq.env", settings.Groq.Env, 0},
		{"google.env", settings.Google.Env, 0},
		{"deepgram.env", settings.Deepgram.Env, 0},
		{"assemblyai.env", settings.AssemblyAI.Env, 0},
		{"huggingface.env", settings.HuggingFace.Env, 0},
		{"openrouter.env", settings.OpenRouter.Env, 0},
	} {
		if field.value != "" && !validEnvName(field.value) {
			return fmt.Errorf("%s must be a valid environment variable name", field.name)
		}
	}
	return nil
}
