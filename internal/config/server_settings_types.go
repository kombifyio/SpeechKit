package config

const (
	ServerSettingsPathEnv            = "SPEECHKIT_SERVER_SETTINGS_PATH"
	ServerSettingsWriteEnv           = "SPEECHKIT_SERVER_SETTINGS_WRITE"
	ServerOnboardingUIEnv            = "SPEECHKIT_SERVER_ONBOARDING_UI"
	ServerAssistantUIEnv             = "SPEECHKIT_SERVER_ASSISTANT_UI"
	ServerOperatorUIPublicEnv        = "SPEECHKIT_SERVER_OPERATOR_UI_PUBLIC"
	ServerDetailedReadinessPublicEnv = "SPEECHKIT_SERVER_DETAILED_READINESS_PUBLIC"

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
	Deepgram    ServerProviderCredentialSettings `json:"deepgram,omitempty"`
	AssemblyAI  ServerProviderCredentialSettings `json:"assemblyai,omitempty"`
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
