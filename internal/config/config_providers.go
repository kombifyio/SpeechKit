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
	// IdleStopMinutes pauses the bundled model server after this many minutes
	// without a request so its memory is released; the next request wakes it.
	// 0 keeps the server running for the whole session.
	IdleStopMinutes int `toml:"idle_stop_minutes"`
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
	Foundry    FoundryProviderConfig    `toml:"foundry"`
}

// FoundryProviderConfig configures Microsoft Foundry (Azure AI Foundry) as a
// cloud provider. Foundry exposes an OpenAI-compatible v1 inference surface on
// the account host (https://<account>.services.ai.azure.com/openai/v1/), so
// STT, LLM, TTS, and Realtime all reuse the OpenAI-compatible request shapes.
//
// ProjectEndpoint is what the user copies from the Foundry portal
// (https://<account>.services.ai.azure.com/api/projects/<project>); the
// inference base URL is derived from its host. The `model` parameter of every
// Foundry request is a *deployment name* (not a model id), so each modality
// carries its own deployment override.
type FoundryProviderConfig struct {
	Enabled bool `toml:"enabled"`
	// ProjectEndpoint is the Foundry project endpoint from the portal, e.g.
	// https://<account>.services.ai.azure.com/api/projects/<project>.
	// A bare account endpoint (https://<account>.services.ai.azure.com) or an
	// Azure OpenAI resource endpoint (https://<resource>.openai.azure.com) is
	// accepted too — only the host is used for inference.
	ProjectEndpoint string `toml:"project_endpoint"`
	APIKeyEnv       string `toml:"api_key_env"`
	// AuthMode selects the credential the adapters send: "api_key" (the
	// resource key from APIKeyEnv) or "entra" (a short-lived token minted by
	// the signed-in Microsoft identity). Sign-in flips it to "entra"; the key
	// path stays selectable because some resources allow both.
	AuthMode string `toml:"auth_mode"`
	// EntraCredential picks how the token is obtained in "entra" mode:
	// "auto" (an Azure CLI session first, then the browser flow when a client
	// id exists), "azure_cli", "browser" or "device_code".
	EntraCredential string `toml:"entra_credential"`
	// EntraTenantID is the sign-in authority ("" = organizations, "common", or
	// a tenant id); it is also passed to `az login --tenant`.
	EntraTenantID string `toml:"entra_tenant_id"`
	// EntraClientID is a bring-your-own public-client app registration for the
	// browser and device-code flows. Empty falls back to the client id the
	// product build injects; the open-source build has none, so those two
	// flows report themselves unavailable and the Azure CLI path remains.
	EntraClientID string `toml:"entra_client_id"`
	// AzureCLIPath overrides Azure CLI detection with an explicit az.cmd path.
	AzureCLIPath string `toml:"azure_cli_path"`
	// AzureCLIProfile is "shared" (reuse the user's own az session, default)
	// or "isolated" (a SpeechKit-private AZURE_CONFIG_DIR so signing in never
	// changes the user's active az account or subscription).
	AzureCLIProfile string `toml:"azure_cli_profile"`
	// Deployment names per modality (Foundry `model` parameter). MAI speech
	// models are not deployments: an STT deployment starting with
	// "MAI-Transcribe" or a TTS deployment starting with "MAI-Voice" routes
	// the request to the resource's Azure Speech surface instead of the
	// OpenAI-compatible route (see STTEngine / TTSEngine).
	STTDeployment      string `toml:"stt_deployment"`
	UtilityDeployment  string `toml:"utility_deployment"`
	AssistDeployment   string `toml:"assist_deployment"`
	AgentDeployment    string `toml:"agent_deployment"`
	RealtimeDeployment string `toml:"realtime_deployment"`
	TTSDeployment      string `toml:"tts_deployment"`
	TTSVoice           string `toml:"tts_voice"`
	// STTStyle ("clean" or "verbatim") and STTDiarization apply to the
	// MAI-Transcribe fast-transcription path only.
	STTStyle       string `toml:"stt_style"`
	STTDiarization bool   `toml:"stt_diarization"`
	// TTSStyle is an optional mstts:express-as style for MAI voices.
	TTSStyle string `toml:"tts_style"`
	// Voice Live (Microsoft's managed realtime voice agent) settings: the
	// brain model, the Azure Speech voice and the input transcription model.
	// The Voice Live path is selected with [voice_agent] provider =
	// "foundry-voicelive"; the OpenAI-Realtime-on-Foundry path keeps using
	// RealtimeDeployment.
	VoiceLiveModel         string `toml:"voicelive_model"`
	VoiceLiveVoice         string `toml:"voicelive_voice"`
	VoiceLiveTranscription string `toml:"voicelive_transcription"`
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
	// LLMGatewayAgentModel is the gateway model registered for the Genkit
	// agent flows (tool-capable). It does NOT select the LLM of the AssemblyAI
	// Voice Agent realtime session: the Voice Agents WS API session config has
	// no model/LLM field (only agent_id binds a stored server-side agent) —
	// see assemblyAISessionUpdate in pkg/speechkit/voiceagent/live.
	// Defaulted while AssemblyAI is enabled so the gateway is always available
	// to agent/summary flows (DefaultAssemblyAILLMGatewayAgentModel).
	LLMGatewayAgentModel string `toml:"llm_gateway_agent_model"`
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
