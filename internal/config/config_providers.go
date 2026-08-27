package config

// STT/LLM provider, local-runtime, VPS, HuggingFace, and routing
// configuration types.

type LocalConfig struct {
	Enabled   bool   `toml:"enabled"`
	Model     string `toml:"model"`
	ModelPath string `toml:"model_path"`
	Port      int    `toml:"port"`
	GPU       string `toml:"gpu"`
}

type LocalLLMConfig struct {
	Enabled      bool   `toml:"enabled"`
	BaseURL      string `toml:"base_url"`
	Model        string `toml:"model"`
	ModelPath    string `toml:"model_path"`
	Port         int    `toml:"port"`
	GPU          string `toml:"gpu"`
	UtilityModel string `toml:"utility_model"`
	AssistModel  string `toml:"assist_model"`
	AgentModel   string `toml:"agent_model"`
}

type VPSConfig struct {
	Enabled   bool   `toml:"enabled"`
	URL       string `toml:"url"`
	Model     string `toml:"model"`
	APIKeyEnv string `toml:"api_key_env"`
}

type HuggingFaceConfig struct {
	Enabled      bool   `toml:"enabled"`
	Model        string `toml:"model"`
	UtilityModel string `toml:"utility_model"`
	AssistModel  string `toml:"assist_model"`
	AgentModel   string `toml:"agent_model"`
	TokenEnv     string `toml:"token_env"`
}

type RoutingConfig struct {
	Strategy                string  `toml:"strategy"`
	PreferLocalUnderSeconds float64 `toml:"prefer_local_under_seconds"`
	ParallelCloud           bool    `toml:"parallel_cloud"`
	ReplaceOnBetter         bool    `toml:"replace_on_better"`
}

// ProvidersConfig groups all external provider configurations.
type ProvidersConfig struct {
	OpenAI     OpenAIProviderConfig     `toml:"openai"`
	Groq       GroqProviderConfig       `toml:"groq"`
	Google     GoogleProviderConfig     `toml:"google"`
	Deepgram   DeepgramProviderConfig   `toml:"deepgram"`
	AssemblyAI AssemblyAIProviderConfig `toml:"assemblyai"`
	Ollama     OllamaProviderConfig     `toml:"ollama"`
	OpenRouter OpenRouterProviderConfig `toml:"openrouter"`
	Cloudflare CloudflareProviderConfig `toml:"cloudflare"`
}

type OpenAIProviderConfig struct {
	Enabled       bool   `toml:"enabled"`
	APIKeyEnv     string `toml:"api_key_env"`
	STTModel      string `toml:"stt_model"`
	UtilityModel  string `toml:"utility_model"`
	AssistModel   string `toml:"assist_model"`
	AgentModel    string `toml:"agent_model"`
	TTSModel      string `toml:"tts_model"`
	TTSVoice      string `toml:"tts_voice"`
	RealtimeModel string `toml:"realtime_model"`
}

type GroqProviderConfig struct {
	Enabled      bool   `toml:"enabled"`
	APIKeyEnv    string `toml:"api_key_env"`
	STTModel     string `toml:"stt_model"`
	UtilityModel string `toml:"utility_model"`
	AssistModel  string `toml:"assist_model"`
	AgentModel   string `toml:"agent_model"`
}

type GoogleProviderConfig struct {
	Enabled                   bool   `toml:"enabled"`
	APIKeyEnv                 string `toml:"api_key_env"`
	STTAPIKeyEnv              string `toml:"stt_api_key_env"`
	STTCredentialsJSONEnv     string `toml:"stt_credentials_json_env"`
	ApplicationCredentialsEnv string `toml:"application_credentials_env"`
	STTModel                  string `toml:"stt_model"`
	UtilityModel              string `toml:"utility_model"`
	AssistModel               string `toml:"assist_model"`
	AgentModel                string `toml:"agent_model"`
	// Region is the Google Cloud region the customer's API key / project is
	// pinned to. Default "europe-west3" (Frankfurt) reflects the EU-enterprise
	// compliance posture. US customers should explicitly set "us-central1".
	//
	// IMPORTANT: this field feeds the byok.key_updated audit event and the
	// settings UI. It does NOT redirect API traffic — the Gemini Live endpoint
	// is a single global WebSocket (generativelanguage.googleapis.com). Actual
	// data residency is controlled at the Google Cloud project level. Both the
	// project region AND this field must match for the audit event to be
	// accurate. See docs/compliance/byok-gemini-region-pinning.md.
	Region string `toml:"region"`
}

type DeepgramProviderConfig struct {
	Enabled                  bool   `toml:"enabled"`
	APIKeyEnv                string `toml:"api_key_env"`
	STTModel                 string `toml:"stt_model"`
	STTLanguage              string `toml:"stt_language"`
	STTSmartFormat           bool   `toml:"stt_smart_format"`
	STTDictation             bool   `toml:"stt_dictation"`
	STTFillerWords           bool   `toml:"stt_filler_words"`
	STTNumerals              bool   `toml:"stt_numerals"`
	STTDetectLanguage        bool   `toml:"stt_detect_language"`
	STTUseVocabularyKeyterms bool   `toml:"stt_use_vocabulary_keyterms"`
	STTKeyterms              string `toml:"stt_keyterms"`
	STTEndpointingMs         int    `toml:"stt_endpointing_ms"`
	DiarizationModel         string `toml:"diarization_model"`
}

type AssemblyAIProviderConfig struct {
	Enabled          bool   `toml:"enabled"`
	APIKeyEnv        string `toml:"api_key_env"`
	STTModels        string `toml:"stt_models"`
	StreamingModel   string `toml:"streaming_model"`
	StreamingBaseURL string `toml:"streaming_base_url"`
	// SyncBaseURL overrides the synchronous Universal-3.5 Pro endpoint.
	// Empty uses the global https://sync.assemblyai.com; set the regional
	// https://sync.us.assemblyai.com or https://sync.eu.assemblyai.com for
	// data-zone pinning.
	SyncBaseURL string `toml:"sync_base_url"`
	// DisableSync forces every transcription through the classic async
	// upload+poll flow even for short clips the sync endpoint could serve.
	DisableSync bool `toml:"disable_sync"`
	// LLM Gateway is AssemblyAI's OpenAI-compatible chat endpoint. The same
	// ASSEMBLYAI_API_KEY authenticates STT and LLM. Empty base URL uses the
	// public US gateway; set llm_gateway.eu.assemblyai.com for EU residency.
	LLMGatewayBaseURL      string `toml:"llm_gateway_base_url"`
	LLMGatewayUtilityModel string `toml:"llm_gateway_utility_model"`
	LLMGatewayAssistModel  string `toml:"llm_gateway_assist_model"`
	LLMGatewayAgentModel   string `toml:"llm_gateway_agent_model"`
	// StreamingLLM attaches LLM Gateway to Universal-3.5 Pro realtime turns
	// (live cleanup / per-turn rewrite). Forced on while AssemblyAI is
	// enabled so Assist, summaries, and live rewrite never start without a
	// native model.
	StreamingLLM bool `toml:"streaming_llm"`
}

// CloudflareProviderConfig is the Workers AI / AI Gateway LLM backend.
type CloudflareProviderConfig struct {
	Enabled      bool   `toml:"enabled"`
	AccountID    string `toml:"account_id"`
	AccountIDEnv string `toml:"account_id_env"`
	APITokenEnv  string `toml:"api_token_env"`
	GatewayID    string `toml:"gateway_id"`
	GatewayIDEnv string `toml:"gateway_id_env"`
	UtilityModel string `toml:"utility_model"`
	AssistModel  string `toml:"assist_model"`
	AgentModel   string `toml:"agent_model"`
}

type OllamaProviderConfig struct {
	Enabled      bool   `toml:"enabled"`
	BaseURL      string `toml:"base_url"`
	STTModel     string `toml:"stt_model"`
	UtilityModel string `toml:"utility_model"`
	AssistModel  string `toml:"assist_model"`
	AgentModel   string `toml:"agent_model"`
}

type OpenRouterProviderConfig struct {
	Enabled      bool   `toml:"enabled"`
	APIKeyEnv    string `toml:"api_key_env"`
	STTModel     string `toml:"stt_model"`
	UtilityModel string `toml:"utility_model"`
	AssistModel  string `toml:"assist_model"`
	AgentModel   string `toml:"agent_model"`
}
