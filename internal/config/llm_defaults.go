package config

import "strings"

const (
	DefaultAssemblyAISTTModels              = "universal-3-5-pro,universal-2"
	DefaultAssemblyAIStreamingModel         = "universal-3-5-pro"
	DefaultAssemblyAILLMGatewayBaseURL      = "https://llm-gateway.assemblyai.com/v1"
	DefaultAssemblyAILLMGatewayUtilityModel = "qwen3.5-4b-32k-fast"
	DefaultAssemblyAILLMGatewayAssistModel  = "qwen3-32B"
	// DefaultAssemblyAILLMGatewayAgentModel powers the Genkit agent flows via
	// the gateway. gemini-2.5-flash is on the gateway's documented model list
	// and supports tool/function calling, which the agent tier requires
	// (https://www.assemblyai.com/docs/llm-gateway/available-models).
	DefaultAssemblyAILLMGatewayAgentModel  = "gemini-2.5-flash"
	DefaultCloudflareAIGatewayUtilityModel = "@cf/meta/llama-3.2-3b-instruct"
	DefaultCloudflareAIGatewayAssistModel  = "@cf/meta/llama-3.1-8b-instruct-fast"
	CloudflareAIGatewayAuthTokenEnv        = "CLOUDFLARE_AI_GATEWAY_AUTH_TOKEN"
	CloudflareAccountIDEnv                 = "CLOUDFLARE_ACCOUNT_ID"
	CloudflareAIGatewayIDEnv               = "CLOUDFLARE_AI_GATEWAY_ID"
	CloudflareAPITokenEnv                  = "CLOUDFLARE_API_TOKEN"
)

// ApplyAssemblyAILLMDefaults fills Universal-3.5 Pro and LLM Gateway slots
// whenever AssemblyAI is enabled, and keeps streaming LLM on so Assist /
// summaries never start without a native model.
func ApplyAssemblyAILLMDefaults(cfg *Config) {
	if cfg == nil || !cfg.Providers.AssemblyAI.Enabled {
		return
	}
	a := &cfg.Providers.AssemblyAI
	if strings.TrimSpace(a.STTModels) == "" || assemblyAINeedsFlagshipUpgrade(a.STTModels) {
		a.STTModels = DefaultAssemblyAISTTModels
	}
	if streamingNeedsFlagshipUpgrade(a.StreamingModel) {
		a.StreamingModel = DefaultAssemblyAIStreamingModel
	}
	if strings.TrimSpace(a.LLMGatewayBaseURL) == "" {
		a.LLMGatewayBaseURL = DefaultAssemblyAILLMGatewayBaseURL
	}
	if strings.TrimSpace(a.LLMGatewayUtilityModel) == "" {
		a.LLMGatewayUtilityModel = DefaultAssemblyAILLMGatewayUtilityModel
	}
	if strings.TrimSpace(a.LLMGatewayAssistModel) == "" {
		a.LLMGatewayAssistModel = DefaultAssemblyAILLMGatewayAssistModel
	}
	if strings.TrimSpace(a.LLMGatewayAgentModel) == "" {
		a.LLMGatewayAgentModel = DefaultAssemblyAILLMGatewayAgentModel
	}
	a.StreamingLLM = true
}

func assemblyAINeedsFlagshipUpgrade(models string) bool {
	trimmed := strings.TrimSpace(models)
	if trimmed == "" {
		return true
	}
	if strings.Contains(trimmed, "universal-3-5-pro") {
		return false
	}
	return strings.Contains(trimmed, "universal-3-pro") || strings.Contains(trimmed, "u3-rt-pro")
}

func streamingNeedsFlagshipUpgrade(model string) bool {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return true
	}
	switch trimmed {
	case "universal-3-pro", "u3-rt-pro", "u3-pro":
		return true
	default:
		return false
	}
}

// ApplyCloudflareAIGatewayDefaults fills the small Workers AI models used
// through Cloudflare AI Gateway. Does not enable the provider; callers do
// that when credentials or kombify Cloud are present.
func ApplyCloudflareAIGatewayDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	c := &cfg.Providers.Cloudflare
	if strings.TrimSpace(c.APITokenEnv) == "" {
		c.APITokenEnv = CloudflareAIGatewayAuthTokenEnv
	}
	if strings.TrimSpace(c.AccountIDEnv) == "" {
		c.AccountIDEnv = CloudflareAccountIDEnv
	}
	if strings.TrimSpace(c.GatewayIDEnv) == "" {
		c.GatewayIDEnv = CloudflareAIGatewayIDEnv
	}
	if strings.TrimSpace(c.UtilityModel) == "" {
		c.UtilityModel = DefaultCloudflareAIGatewayUtilityModel
	}
	if strings.TrimSpace(c.AssistModel) == "" {
		c.AssistModel = DefaultCloudflareAIGatewayAssistModel
	}
}

// EnableAlwaysOnLLM keeps a native LLM available whenever AssemblyAI is
// enabled, Cloudflare credentials resolve, or the device is connected to
// kombify Cloud. Call this on load and after provider / cloud toggles.
func EnableAlwaysOnLLM(cfg *Config) {
	if cfg == nil {
		return
	}
	ApplyAssemblyAILLMDefaults(cfg)
	ApplyCloudflareAIGatewayDefaults(cfg)
	if KombifyCloudConnected(cfg) || CloudflareAIGatewayReady(cfg) {
		cfg.Providers.Cloudflare.Enabled = true
	}
}

func ResolveCloudflareAccountID(cfg *Config) string {
	if cfg != nil {
		if id := strings.TrimSpace(cfg.Providers.Cloudflare.AccountID); id != "" {
			return id
		}
		envName := strings.TrimSpace(cfg.Providers.Cloudflare.AccountIDEnv)
		if envName == "" {
			envName = CloudflareAccountIDEnv
		}
		if id := strings.TrimSpace(ResolveSecret(envName)); id != "" {
			return id
		}
	}
	return strings.TrimSpace(ResolveSecret(CloudflareAccountIDEnv))
}

func ResolveCloudflareGatewayID(cfg *Config) string {
	if cfg != nil {
		if id := strings.TrimSpace(cfg.Providers.Cloudflare.GatewayID); id != "" {
			return id
		}
		envName := strings.TrimSpace(cfg.Providers.Cloudflare.GatewayIDEnv)
		if envName == "" {
			envName = CloudflareAIGatewayIDEnv
		}
		if id := strings.TrimSpace(ResolveSecret(envName)); id != "" {
			return id
		}
	}
	if id := strings.TrimSpace(ResolveSecret(CloudflareAIGatewayIDEnv)); id != "" {
		return id
	}
	return "default"
}

func CloudflareAIGatewayReady(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	token, _, _ := ResolveProviderCredentialValue(cfg, "cloudflare")
	return strings.TrimSpace(token) != "" && ResolveCloudflareAccountID(cfg) != ""
}

func KombifyCloudConnected(cfg *Config) bool {
	if cfg == nil || !cfg.ServerConnection.Enabled {
		return false
	}
	url := strings.ToLower(strings.TrimSpace(cfg.ServerConnection.URL))
	return strings.Contains(url, "speechkit.kombify.io") || strings.Contains(url, "api.kombify.io")
}
