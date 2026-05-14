package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
	"golang.org/x/crypto/bcrypt"
)

const (
	ServerSettingsPathEnv  = "SPEECHKIT_SERVER_SETTINGS_PATH"
	ServerSettingsWriteEnv = "SPEECHKIT_SERVER_SETTINGS_WRITE"
	ServerOnboardingUIEnv  = "SPEECHKIT_SERVER_ONBOARDING_UI"

	defaultServerSettingsPath = "/var/lib/speechkit/data/server-settings.json"

	ServerAuthModeManagedBearer = "managed_bearer"
	ServerAuthModeSelfManaged   = "self_managed"
)

type ServerModelSettings struct {
	Version            int                        `json:"version,omitempty"`
	OnboardingComplete bool                       `json:"onboarding_complete,omitempty"`
	OnboardingVersion  string                     `json:"onboarding_version,omitempty"`
	ServerAuth         ServerAuthSettings         `json:"server_auth,omitempty"`
	AdminAuth          ServerAdminAuthSettings    `json:"admin_auth,omitempty"`
	Modes              ServerModeProviderSettings `json:"modes,omitempty"`
	Credentials        ServerCredentialSettings   `json:"credentials,omitempty"`
	Dictation          ServerDictationSettings    `json:"dictation,omitempty"`
	Assist             ServerAssistSettings       `json:"assist,omitempty"`
	STT                ServerSTTSettings          `json:"stt,omitempty"`
	LLM                ServerLLMSettings          `json:"llm,omitempty"`
	VoiceAgent         ServerVoiceAgentSettings   `json:"voice_agent,omitempty"`
	TTS                ServerOptionalTTSSettings  `json:"tts,omitempty"`
}

type ServerAuthSettings struct {
	Mode           string `json:"mode,omitempty"`
	BearerTokenEnv string `json:"bearer_token_env,omitempty"`
	GenerateToken  *bool  `json:"generate_token,omitempty"`
	TokenValue     string `json:"token_value,omitempty"`
}

type ServerAdminAuthSettings struct {
	Enabled       *bool  `json:"enabled,omitempty"`
	Username      string `json:"username,omitempty"`
	PasswordHash  string `json:"password_hash,omitempty"`
	PasswordValue string `json:"password,omitempty"`
}

type ServerModeProviderSettings struct {
	Dictation  ServerModeSetting `json:"dictation,omitempty"`
	Assist     ServerModeSetting `json:"assist,omitempty"`
	VoiceAgent ServerModeSetting `json:"voice_agent,omitempty"`
}

type ServerModeSetting struct {
	Enabled      *bool  `json:"enabled,omitempty"`
	ProviderKind string `json:"provider_kind,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	Model        string `json:"model,omitempty"`
}

type ServerCredentialSettings struct {
	OpenAI      ServerProviderCredentialSettings `json:"openai,omitempty"`
	Groq        ServerProviderCredentialSettings `json:"groq,omitempty"`
	Google      ServerProviderCredentialSettings `json:"google,omitempty"`
	HuggingFace ServerProviderCredentialSettings `json:"huggingface,omitempty"`
	OpenRouter  ServerProviderCredentialSettings `json:"openrouter,omitempty"`
}

type ServerProviderCredentialSettings struct {
	Enabled *bool  `json:"enabled,omitempty"`
	Env     string `json:"env,omitempty"`
	Value   string `json:"value,omitempty"`
}

type ServerDictationSettings struct {
	Dictionary *string `json:"dictionary,omitempty"`
}

type ServerAssistSettings struct {
	EnabledTools []string `json:"enabled_tools,omitempty"`
}

type ServerSTTSettings struct {
	Enabled *bool  `json:"enabled,omitempty"`
	URL     string `json:"url,omitempty"`
	Model   string `json:"model,omitempty"`
}

type ServerLLMSettings struct {
	Enabled      *bool  `json:"enabled,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
	UtilityModel string `json:"utility_model,omitempty"`
	AssistModel  string `json:"assist_model,omitempty"`
	AgentModel   string `json:"agent_model,omitempty"`
	HFRepo       string `json:"hf_repo,omitempty"`
}

type ServerVoiceAgentSettings struct {
	Provider        string  `json:"provider,omitempty"`
	AgentProfileID  string  `json:"agent_profile_id,omitempty"`
	AgentSequenceID string  `json:"agent_sequence_id,omitempty"`
	PromptTemplate  *string `json:"prompt_template,omitempty"`
}

type ServerOptionalTTSSettings struct {
	Enabled *bool `json:"enabled,omitempty"`
}

func ServerSettingsPath(cfg *Config) string {
	if path := strings.TrimSpace(os.Getenv(ServerSettingsPathEnv)); path != "" {
		return path
	}
	if cfg != nil && strings.TrimSpace(cfg.Store.SQLitePath) != "" {
		return filepath.Join(filepath.Dir(cfg.Store.SQLitePath), "server-settings.json")
	}
	return defaultServerSettingsPath
}

func LoadServerModelSettings(path string) (ServerModelSettings, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ServerModelSettings{}, false, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- server settings path is controlled by deployment config/env.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ServerModelSettings{}, false, nil
		}
		return ServerModelSettings{}, false, err
	}
	var settings ServerModelSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return ServerModelSettings{}, false, err
	}
	settings = NormalizeServerModelSettings(settings)
	return settings, true, nil
}

func SaveServerModelSettings(path string, settings ServerModelSettings) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("server settings path is empty")
	}
	settings = NormalizeServerModelSettings(settings)
	if settings.AdminAuth.PasswordValue != "" && settings.AdminAuth.Enabled == nil {
		enabled := true
		settings.AdminAuth.Enabled = &enabled
	}
	if err := validateServerModelSettings(settings); err != nil {
		return err
	}
	if value := settings.AdminAuth.PasswordValue; value != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash admin password: %w", err)
		}
		settings.AdminAuth.PasswordHash = string(hash)
		settings.AdminAuth.PasswordValue = ""
	}
	settings = SanitizeServerModelSettings(settings)
	settings.Version = 1
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create server settings dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write server settings: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace server settings: %w", err)
	}
	return nil
}

func SanitizeServerModelSettings(settings ServerModelSettings) ServerModelSettings {
	settings = NormalizeServerModelSettings(settings)
	settings.ServerAuth.GenerateToken = nil
	settings.ServerAuth.TokenValue = ""
	settings.AdminAuth.PasswordValue = ""
	settings.Credentials.OpenAI.Value = ""
	settings.Credentials.Groq.Value = ""
	settings.Credentials.Google.Value = ""
	settings.Credentials.HuggingFace.Value = ""
	settings.Credentials.OpenRouter.Value = ""
	return settings
}

func NormalizeServerModelSettings(settings ServerModelSettings) ServerModelSettings {
	settings.ServerAuth.Mode = strings.ToLower(strings.TrimSpace(settings.ServerAuth.Mode))
	settings.ServerAuth.BearerTokenEnv = strings.TrimSpace(settings.ServerAuth.BearerTokenEnv)
	settings.ServerAuth.TokenValue = strings.TrimSpace(settings.ServerAuth.TokenValue)
	settings.AdminAuth.Username = strings.TrimSpace(settings.AdminAuth.Username)
	settings.AdminAuth.PasswordHash = strings.TrimSpace(settings.AdminAuth.PasswordHash)
	settings.AdminAuth.PasswordValue = strings.TrimSpace(settings.AdminAuth.PasswordValue)
	settings.LLM.UtilityModel = normalizeServerModelValue(settings.LLM.UtilityModel)
	settings.LLM.AssistModel = normalizeServerModelValue(settings.LLM.AssistModel)
	settings.LLM.AgentModel = normalizeServerModelValue(settings.LLM.AgentModel)
	settings.Modes.Assist.Model = normalizeServerModelValue(settings.Modes.Assist.Model)
	settings.Modes.VoiceAgent.Model = normalizeServerModelValue(settings.Modes.VoiceAgent.Model)
	if settings.Dictation.Dictionary != nil {
		value := normalizeServerMultiline(*settings.Dictation.Dictionary)
		settings.Dictation.Dictionary = &value
	}
	if settings.VoiceAgent.PromptTemplate != nil {
		value := normalizeServerMultiline(*settings.VoiceAgent.PromptTemplate)
		settings.VoiceAgent.PromptTemplate = &value
	}
	if strings.TrimSpace(settings.VoiceAgent.AgentProfileID) != "" {
		settings.VoiceAgent.AgentProfileID = voiceagentprofile.NormalizeID(settings.VoiceAgent.AgentProfileID)
	}
	settings.Assist.EnabledTools = normalizeServerStringList(settings.Assist.EnabledTools)
	return settings
}

func normalizeServerModelValue(value string) string {
	value = strings.TrimSpace(value)
	if localLLMModelNeedsDefault(value) {
		return defaultServerLLMModel
	}
	return value
}

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
	notes = append(notes, applyOpenAICredential(cfg, credentials.OpenAI)...)
	notes = append(notes, applyGroqCredential(cfg, credentials.Groq)...)
	notes = append(notes, applyGoogleCredential(cfg, credentials.Google)...)
	notes = append(notes, applyHuggingFaceCredential(cfg, credentials.HuggingFace)...)
	notes = append(notes, applyOpenRouterCredential(cfg, credentials.OpenRouter)...)
	return notes
}

func applyOpenAICredential(cfg *Config, credential ServerProviderCredentialSettings) []string {
	var notes []string
	if credential.Enabled != nil {
		cfg.Providers.OpenAI.Enabled = *credential.Enabled
		notes = append(notes, "server settings: OpenAI enabled updated")
	}
	if env := cleanSetting(credential.Env); env != "" {
		cfg.Providers.OpenAI.APIKeyEnv = env
		notes = append(notes, "server settings: OpenAI key env updated")
	}
	if value := cleanSetting(credential.Value); value != "" {
		setCredentialEnv(cfg.Providers.OpenAI.APIKeyEnv, "OPENAI_API_KEY", value)
		notes = append(notes, "server settings: OpenAI key value loaded")
	}
	return notes
}

func applyGroqCredential(cfg *Config, credential ServerProviderCredentialSettings) []string {
	var notes []string
	if credential.Enabled != nil {
		cfg.Providers.Groq.Enabled = *credential.Enabled
		notes = append(notes, "server settings: Groq enabled updated")
	}
	if env := cleanSetting(credential.Env); env != "" {
		cfg.Providers.Groq.APIKeyEnv = env
		notes = append(notes, "server settings: Groq key env updated")
	}
	if value := cleanSetting(credential.Value); value != "" {
		setCredentialEnv(cfg.Providers.Groq.APIKeyEnv, "GROQ_API_KEY", value)
		notes = append(notes, "server settings: Groq key value loaded")
	}
	return notes
}

func applyGoogleCredential(cfg *Config, credential ServerProviderCredentialSettings) []string {
	var notes []string
	if credential.Enabled != nil {
		cfg.Providers.Google.Enabled = *credential.Enabled
		notes = append(notes, "server settings: Google enabled updated")
	}
	if env := cleanSetting(credential.Env); env != "" {
		cfg.Providers.Google.APIKeyEnv = env
		notes = append(notes, "server settings: Google key env updated")
	}
	if value := cleanSetting(credential.Value); value != "" {
		setCredentialEnv(cfg.Providers.Google.APIKeyEnv, "GOOGLE_AI_API_KEY", value)
		notes = append(notes, "server settings: Google key value loaded")
	}
	return notes
}

func applyHuggingFaceCredential(cfg *Config, credential ServerProviderCredentialSettings) []string {
	var notes []string
	if credential.Enabled != nil {
		cfg.HuggingFace.Enabled = *credential.Enabled
		notes = append(notes, "server settings: Hugging Face enabled updated")
	}
	if env := cleanSetting(credential.Env); env != "" {
		cfg.HuggingFace.TokenEnv = env
		notes = append(notes, "server settings: Hugging Face token env updated")
	}
	if value := cleanSetting(credential.Value); value != "" {
		setCredentialEnv(cfg.HuggingFace.TokenEnv, "HF_TOKEN", value)
		notes = append(notes, "server settings: Hugging Face token value loaded")
	}
	return notes
}

func applyOpenRouterCredential(cfg *Config, credential ServerProviderCredentialSettings) []string {
	var notes []string
	if credential.Enabled != nil {
		cfg.Providers.OpenRouter.Enabled = *credential.Enabled
		notes = append(notes, "server settings: OpenRouter enabled updated")
	}
	if env := cleanSetting(credential.Env); env != "" {
		cfg.Providers.OpenRouter.APIKeyEnv = env
		notes = append(notes, "server settings: OpenRouter key env updated")
	}
	if value := cleanSetting(credential.Value); value != "" {
		setCredentialEnv(cfg.Providers.OpenRouter.APIKeyEnv, "OPENROUTER_API_KEY", value)
		notes = append(notes, "server settings: OpenRouter key value loaded")
	}
	return notes
}

func applyServerModeProviderSettings(cfg *Config, modes ServerModeProviderSettings) []string {
	var notes []string
	if modeSettingDefined(modes.Dictation) {
		notes = append(notes, applyDictationModeSetting(cfg, modes.Dictation)...)
	}
	if modeSettingDefined(modes.Assist) {
		notes = append(notes, applyAssistModeSetting(cfg, modes.Assist)...)
	}
	if modeSettingDefined(modes.VoiceAgent) {
		notes = append(notes, applyVoiceAgentModeSetting(cfg, modes.VoiceAgent)...)
	}
	return notes
}

func applyDictationModeSetting(cfg *Config, mode ServerModeSetting) []string {
	var notes []string
	kind := normalizedProviderKind(mode)
	model := cleanSetting(mode.Model)
	if mode.Enabled != nil && !*mode.Enabled {
		cfg.General.DictateEnabled = false
		notes = append(notes, "server settings: Dictation disabled")
		return notes
	}
	switch kind {
	case "direct_provider":
		cfg.Local.Enabled = false
		cfg.Routing.Strategy = "cloud-only"
		cfg.VPS.Enabled = false
		cfg.HuggingFace.Enabled = false
		cfg.Providers.OpenAI.Enabled = true
		if model != "" {
			cfg.Providers.OpenAI.STTModel = model
		}
		notes = append(notes, "server settings: Dictation uses direct provider")
	case "cloud_provider":
		cfg.Local.Enabled = false
		cfg.Routing.Strategy = "cloud-only"
		cfg.VPS.Enabled = false
		if strings.Contains(strings.ToLower(mode.ProfileID), "openrouter") {
			cfg.HuggingFace.Enabled = false
			cfg.Providers.OpenRouter.Enabled = true
			if model != "" {
				cfg.Providers.OpenRouter.STTModel = model
			}
		} else {
			cfg.HuggingFace.Enabled = true
			if model != "" {
				cfg.HuggingFace.Model = model
			}
		}
		notes = append(notes, "server settings: Dictation uses cloud provider")
	case "local_provider":
		cfg.Local.Enabled = true
		cfg.Routing.Strategy = "local-only"
		cfg.VPS.Enabled = false
		cfg.Providers.Ollama.Enabled = true
		if model != "" {
			cfg.Providers.Ollama.STTModel = model
		}
		notes = append(notes, "server settings: Dictation uses local provider")
	default:
		cfg.Local.Enabled = false
		cfg.Routing.Strategy = "cloud-only"
		cfg.VPS.Enabled = true
		if model != "" {
			cfg.VPS.Model = model
		}
		notes = append(notes, "server settings: Dictation uses built-in provider")
	}
	return notes
}

func applyAssistModeSetting(cfg *Config, mode ServerModeSetting) []string {
	var notes []string
	kind := normalizedProviderKind(mode)
	model := cleanSetting(mode.Model)
	if mode.Enabled != nil && !*mode.Enabled {
		cfg.General.AssistEnabled = false
		notes = append(notes, "server settings: Assist disabled")
		return notes
	}
	switch kind {
	case "direct_provider":
		cfg.LocalLLM.Enabled = false
		if strings.Contains(strings.ToLower(mode.ProfileID), "openrouter") {
			cfg.Providers.OpenRouter.Enabled = true
			if model != "" {
				cfg.Providers.OpenRouter.AssistModel = model
			}
		} else if strings.Contains(strings.ToLower(mode.ProfileID), "google") {
			cfg.Providers.Google.Enabled = true
			if model != "" {
				cfg.Providers.Google.AssistModel = model
			}
		} else {
			cfg.Providers.OpenAI.Enabled = true
			if model != "" {
				cfg.Providers.OpenAI.AssistModel = model
			}
		}
		notes = append(notes, "server settings: Assist uses direct provider")
	case "cloud_provider":
		cfg.LocalLLM.Enabled = false
		if strings.Contains(strings.ToLower(mode.ProfileID), "openrouter") {
			cfg.HuggingFace.Enabled = false
			cfg.Providers.OpenRouter.Enabled = true
			if model != "" {
				cfg.Providers.OpenRouter.AssistModel = model
			}
		} else {
			cfg.HuggingFace.Enabled = true
			if model != "" {
				cfg.HuggingFace.AssistModel = model
			}
		}
		notes = append(notes, "server settings: Assist uses cloud provider")
	case "local_provider":
		cfg.LocalLLM.Enabled = false
		cfg.Providers.Ollama.Enabled = true
		if model != "" {
			cfg.Providers.Ollama.AssistModel = model
		}
		notes = append(notes, "server settings: Assist uses local provider")
	default:
		cfg.LocalLLM.Enabled = true
		if model != "" {
			model = normalizeServerLLMModel(model)
			cfg.LocalLLM.UtilityModel = model
			cfg.LocalLLM.AssistModel = model
		}
		notes = append(notes, "server settings: Assist uses built-in provider")
	}
	return notes
}

func applyVoiceAgentModeSetting(cfg *Config, mode ServerModeSetting) []string {
	var notes []string
	kind := normalizedProviderKind(mode)
	model := cleanSetting(mode.Model)
	if mode.Enabled != nil && !*mode.Enabled {
		cfg.General.VoiceAgentEnabled = false
		notes = append(notes, "server settings: Voice Agent disabled")
		return notes
	}
	switch kind {
	case "direct_provider":
		cfg.VoiceAgent.Provider = "gemini"
		cfg.Providers.Google.Enabled = true
		if model != "" {
			cfg.VoiceAgent.Model = model
		}
		notes = append(notes, "server settings: Voice Agent uses direct provider")
	case "cloud_provider":
		cfg.VoiceAgent.Provider = "cascaded"
		if strings.Contains(strings.ToLower(mode.ProfileID), "openrouter") {
			cfg.HuggingFace.Enabled = false
			cfg.Providers.OpenRouter.Enabled = true
			if model != "" {
				cfg.Providers.OpenRouter.AgentModel = model
			}
		} else {
			cfg.HuggingFace.Enabled = true
			if model != "" {
				cfg.HuggingFace.AgentModel = model
			}
		}
		notes = append(notes, "server settings: Voice Agent uses cloud fallback provider")
	case "local_provider":
		cfg.VoiceAgent.Provider = "cascaded"
		cfg.Providers.Ollama.Enabled = true
		if model != "" {
			cfg.Providers.Ollama.AgentModel = model
		}
		notes = append(notes, "server settings: Voice Agent uses local provider")
	default:
		cfg.VoiceAgent.Provider = "cascaded"
		cfg.LocalLLM.Enabled = true
		if model != "" {
			cfg.LocalLLM.AgentModel = normalizeServerLLMModel(model)
		}
		notes = append(notes, "server settings: Voice Agent uses built-in provider")
	}
	return notes
}

func validateServerModelSettings(settings ServerModelSettings) error {
	for name, value := range map[string]string{
		"server_auth.mode":  settings.ServerAuth.Mode,
		"server_auth.env":   settings.ServerAuth.BearerTokenEnv,
		"admin_auth.user":   settings.AdminAuth.Username,
		"admin_auth.hash":   settings.AdminAuth.PasswordHash,
		"stt.url":           settings.STT.URL,
		"stt.model":         settings.STT.Model,
		"llm.base_url":      settings.LLM.BaseURL,
		"llm.utility_model": settings.LLM.UtilityModel,
		"llm.assist_model":  settings.LLM.AssistModel,
		"llm.agent_model":   settings.LLM.AgentModel,
		"llm.hf_repo":       settings.LLM.HFRepo,
		"voice.provider":    settings.VoiceAgent.Provider,
		"voice.agent":       settings.VoiceAgent.AgentProfileID,
		"dictation.profile": settings.Modes.Dictation.ProfileID,
		"dictation.model":   settings.Modes.Dictation.Model,
		"assist.profile":    settings.Modes.Assist.ProfileID,
		"assist.model":      settings.Modes.Assist.Model,
		"voice.profile":     settings.Modes.VoiceAgent.ProfileID,
		"voice.model":       settings.Modes.VoiceAgent.Model,
	} {
		if len(value) > 512 {
			return fmt.Errorf("%s is too long", name)
		}
	}
	if settings.Dictation.Dictionary != nil && len(*settings.Dictation.Dictionary) > 8192 {
		return fmt.Errorf("dictation.dictionary is too long")
	}
	if settings.VoiceAgent.PromptTemplate != nil && len(*settings.VoiceAgent.PromptTemplate) > 8192 {
		return fmt.Errorf("voice_agent.prompt_template is too long")
	}
	if len(settings.ServerAuth.TokenValue) > 4096 {
		return fmt.Errorf("server_auth.token_value is too long")
	}
	if len(settings.AdminAuth.PasswordValue) > 4096 {
		return fmt.Errorf("admin_auth.password is too long")
	}
	if settings.AdminAuth.Enabled != nil && *settings.AdminAuth.Enabled && settings.AdminAuth.PasswordHash == "" && settings.AdminAuth.PasswordValue == "" {
		return fmt.Errorf("admin_auth.password is required when admin auth is enabled")
	}
	if (settings.AdminAuth.PasswordHash != "" || settings.AdminAuth.PasswordValue != "" || (settings.AdminAuth.Enabled != nil && *settings.AdminAuth.Enabled)) && settings.AdminAuth.Username == "" {
		return fmt.Errorf("admin_auth.username is required when an admin password is configured")
	}
	switch strings.ToLower(strings.TrimSpace(settings.ServerAuth.Mode)) {
	case "", ServerAuthModeManagedBearer, ServerAuthModeSelfManaged:
	default:
		return fmt.Errorf("server_auth.mode must be managed_bearer or self_managed")
	}
	if settings.ServerAuth.BearerTokenEnv != "" && !validEnvName(settings.ServerAuth.BearerTokenEnv) {
		return fmt.Errorf("server_auth.bearer_token_env must be a valid environment variable name")
	}
	for _, tool := range settings.Assist.EnabledTools {
		if !validServerToolID(tool) {
			return fmt.Errorf("assist.enabled_tools contains an invalid tool id")
		}
	}
	for name, value := range map[string]string{
		"stt.url":      settings.STT.URL,
		"llm.base_url": settings.LLM.BaseURL,
	} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
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
	}
	provider := strings.ToLower(strings.TrimSpace(settings.VoiceAgent.Provider))
	if provider != "" && provider != "cascaded" && provider != "gemini" && provider != "moshi" {
		return fmt.Errorf("voice_agent.provider must be cascaded, gemini, or moshi")
	}
	for name, value := range map[string]string{
		"dictation.provider_kind": settings.Modes.Dictation.ProviderKind,
		"assist.provider_kind":    settings.Modes.Assist.ProviderKind,
		"voice.provider_kind":     settings.Modes.VoiceAgent.ProviderKind,
	} {
		if !validServerProviderKind(value) {
			return fmt.Errorf("%s must be local_built_in, local_provider, cloud_provider, or direct_provider", name)
		}
	}
	for name, value := range map[string]string{
		"openai.env":      settings.Credentials.OpenAI.Env,
		"groq.env":        settings.Credentials.Groq.Env,
		"google.env":      settings.Credentials.Google.Env,
		"huggingface.env": settings.Credentials.HuggingFace.Env,
		"openrouter.env":  settings.Credentials.OpenRouter.Env,
	} {
		if value != "" && !validEnvName(value) {
			return fmt.Errorf("%s must be a valid environment variable name", name)
		}
	}
	for name, value := range map[string]string{
		"openai.value":      settings.Credentials.OpenAI.Value,
		"groq.value":        settings.Credentials.Groq.Value,
		"google.value":      settings.Credentials.Google.Value,
		"huggingface.value": settings.Credentials.HuggingFace.Value,
		"openrouter.value":  settings.Credentials.OpenRouter.Value,
	} {
		if len(value) > 4096 {
			return fmt.Errorf("%s is too long", name)
		}
	}
	return nil
}

func cleanSetting(value string) string {
	return strings.TrimSpace(value)
}

func normalizeServerMultiline(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func normalizeServerStringList(values []string) []string {
	if values == nil {
		return nil
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func modeSettingDefined(mode ServerModeSetting) bool {
	return mode.Enabled != nil ||
		cleanSetting(mode.ProviderKind) != "" ||
		cleanSetting(mode.ProfileID) != "" ||
		cleanSetting(mode.Model) != ""
}

func normalizedProviderKind(mode ServerModeSetting) string {
	if kind := strings.ToLower(cleanSetting(mode.ProviderKind)); kind != "" {
		return kind
	}
	profileID := strings.ToLower(cleanSetting(mode.ProfileID))
	switch {
	case strings.Contains(profileID, ".builtin.") || strings.Contains(profileID, ".local."):
		return "local_built_in"
	case strings.Contains(profileID, ".ollama."):
		return "local_provider"
	case strings.Contains(profileID, ".routed.") || strings.Contains(profileID, ".hf."):
		return "cloud_provider"
	case profileID != "":
		return "direct_provider"
	default:
		return ""
	}
}

func validServerProviderKind(value string) bool {
	switch strings.ToLower(cleanSetting(value)) {
	case "", "local_built_in", "local_provider", "cloud_provider", "direct_provider":
		return true
	default:
		return false
	}
}

func validEnvName(value string) bool {
	value = cleanSetting(value)
	if value == "" {
		return false
	}
	for i, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func validServerToolID(value string) bool {
	value = cleanSetting(value)
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func setCredentialEnv(envName, fallback, value string) {
	envName = cleanSetting(envName)
	if envName == "" {
		envName = fallback
	}
	_ = os.Setenv(envName, value)
}
