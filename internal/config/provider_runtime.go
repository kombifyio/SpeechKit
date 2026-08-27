package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/secrets"
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
)

const (
	OpenAIAPIKeyEnv     = "OPENAI_API_KEY"
	GroqAPIKeyEnv       = "GROQ_API_KEY"
	OpenRouterAPIKeyEnv = "OPENROUTER_API_KEY"

	ProviderIntegrationCloudGateway = "cloud_gateway"
	ProviderIntegrationDirectAPI    = "direct_api"
	ProviderIntegrationLocal        = "local_provider"
)

var (
	ErrUnsupportedProvider           = errors.New("unsupported provider")
	ErrHuggingFaceUnavailableInBuild = errors.New("hugging face is not available in this build")
)

// ProviderRuntime describes host-side provider metadata that is intentionally
// outside the public framework catalog: UI labels, setup URLs, config toggles,
// credential env names, and integration grouping.
type ProviderRuntime struct {
	Provider           string
	DisplayName        string
	ProviderKind       framework.ProviderKind
	IntegrationKind    string
	CredentialTarget   string
	CredentialRequired bool
	SetupURL           string
	SupportedModes     []framework.Mode
	UserConfigurable   bool
}

type ProviderCredentialStatus struct {
	Provider        string
	Target          string
	Label           string
	EnvName         string
	Available       bool
	HasStoredSecret bool
	Source          string
}

func ProviderRuntimes() []ProviderRuntime {
	out := make([]ProviderRuntime, 0, len(providerRuntimeRegistry))
	for _, runtime := range providerRuntimeRegistry {
		out = append(out, cloneProviderRuntime(runtime))
	}
	return out
}

func UserConfigurableProviderRuntimes() []ProviderRuntime {
	var out []ProviderRuntime
	for _, runtime := range providerRuntimeRegistry {
		if runtime.UserConfigurable {
			out = append(out, cloneProviderRuntime(runtime))
		}
	}
	return out
}

func ProviderRuntimeFor(provider string) (ProviderRuntime, bool) {
	provider = framework.NormalizeProviderID(provider)
	for _, runtime := range providerRuntimeRegistry {
		if runtime.Provider == provider {
			return cloneProviderRuntime(runtime), true
		}
	}
	return ProviderRuntime{}, false
}

func ProviderLabel(providerOrCredentialTarget string) string {
	target := NormalizeProviderCredentialTarget(providerOrCredentialTarget)
	if target == "google_stt" {
		return "Google Speech-to-Text"
	}
	if runtime, ok := ProviderRuntimeFor(providerOrCredentialTarget); ok {
		return runtime.DisplayName
	}
	if target != "" {
		if runtime, ok := ProviderRuntimeFor(ProviderForCredentialTarget(target)); ok {
			return runtime.DisplayName
		}
	}
	return "Provider"
}

func NormalizeProviderCredentialTarget(target string) string {
	value := strings.ToLower(strings.TrimSpace(target))
	valueWithUnderscores := strings.ReplaceAll(value, "-", "_")
	switch valueWithUnderscores {
	case "":
		return ""
	case "google_stt", "stt_google", "google_speech_to_text", "google_cloud_stt":
		return "google_stt"
	}
	if strings.HasPrefix(value, "stt.google.") {
		return "google_stt"
	}
	provider := framework.NormalizeProviderID(target)
	switch provider {
	case "openai", "groq", "google", "deepgram", "assemblyai", "huggingface", "openrouter", "cloudflare":
		return provider
	default:
		return provider
	}
}

func ProviderForCredentialTarget(target string) string {
	switch NormalizeProviderCredentialTarget(target) {
	case "google_stt":
		return "google"
	default:
		return NormalizeProviderCredentialTarget(target)
	}
}

func ProviderCredentialTargets() []string {
	return []string{
		"openai",
		"groq",
		"google",
		"google_stt",
		"deepgram",
		"assemblyai",
		"huggingface",
		"openrouter",
		"cloudflare",
	}
}

func ProviderCredentialEnvName(cfg *Config, target string) string {
	switch NormalizeProviderCredentialTarget(target) {
	case "openai":
		if cfg != nil && strings.TrimSpace(cfg.Providers.OpenAI.APIKeyEnv) != "" {
			return strings.TrimSpace(cfg.Providers.OpenAI.APIKeyEnv)
		}
		return OpenAIAPIKeyEnv
	case "groq":
		if cfg != nil && strings.TrimSpace(cfg.Providers.Groq.APIKeyEnv) != "" {
			return strings.TrimSpace(cfg.Providers.Groq.APIKeyEnv)
		}
		return GroqAPIKeyEnv
	case "google":
		if cfg != nil && strings.TrimSpace(cfg.Providers.Google.APIKeyEnv) != "" {
			return strings.TrimSpace(cfg.Providers.Google.APIKeyEnv)
		}
		return GoogleAIAPIKeyEnv
	case "google_stt":
		return GoogleSTTAPIKeyEnvName(cfg)
	case "deepgram":
		if cfg != nil && strings.TrimSpace(cfg.Providers.Deepgram.APIKeyEnv) != "" {
			return strings.TrimSpace(cfg.Providers.Deepgram.APIKeyEnv)
		}
		return DeepgramAPIKeyEnv
	case "assemblyai":
		if cfg != nil && strings.TrimSpace(cfg.Providers.AssemblyAI.APIKeyEnv) != "" {
			return strings.TrimSpace(cfg.Providers.AssemblyAI.APIKeyEnv)
		}
		return AssemblyAIAPIKeyEnv
	case "huggingface":
		return HuggingFaceTokenEnvName(cfg)
	case "openrouter":
		if cfg != nil && strings.TrimSpace(cfg.Providers.OpenRouter.APIKeyEnv) != "" {
			return strings.TrimSpace(cfg.Providers.OpenRouter.APIKeyEnv)
		}
		return OpenRouterAPIKeyEnv
	case "cloudflare":
		if cfg != nil && strings.TrimSpace(cfg.Providers.Cloudflare.APITokenEnv) != "" {
			return strings.TrimSpace(cfg.Providers.Cloudflare.APITokenEnv)
		}
		return CloudflareAIGatewayAuthTokenEnv
	default:
		return ""
	}
}

func SetProviderCredentialEnvName(cfg *Config, target, envName string) error {
	if cfg == nil {
		return nil
	}
	envName = strings.TrimSpace(envName)
	switch NormalizeProviderCredentialTarget(target) {
	case "openai":
		cfg.Providers.OpenAI.APIKeyEnv = envName
	case "groq":
		cfg.Providers.Groq.APIKeyEnv = envName
	case "google":
		cfg.Providers.Google.APIKeyEnv = envName
	case "google_stt":
		cfg.Providers.Google.STTAPIKeyEnv = envName
	case "deepgram":
		cfg.Providers.Deepgram.APIKeyEnv = envName
	case "assemblyai":
		cfg.Providers.AssemblyAI.APIKeyEnv = envName
	case "huggingface":
		cfg.HuggingFace.TokenEnv = envName
	case "openrouter":
		cfg.Providers.OpenRouter.APIKeyEnv = envName
	case "cloudflare":
		cfg.Providers.Cloudflare.APITokenEnv = envName
	default:
		return fmt.Errorf("%w %q", ErrUnsupportedProvider, target)
	}
	return nil
}

func ResolveProviderCredentialValue(cfg *Config, target string) (string, string, error) {
	target = NormalizeProviderCredentialTarget(target)
	switch target {
	case "":
		return "", "", nil
	case "huggingface":
		token, _, err := ResolveHuggingFaceToken(cfg)
		return strings.TrimSpace(token), HuggingFaceTokenEnvName(cfg), err
	case "google_stt":
		key, envName := ResolveGoogleSTTKey(cfg)
		return strings.TrimSpace(key), strings.TrimSpace(envName), nil
	case "deepgram":
		key, envName := ResolveDeepgramKey(cfg)
		return strings.TrimSpace(key), strings.TrimSpace(envName), nil
	case "assemblyai":
		key, envName := ResolveAssemblyAIKey(cfg)
		return strings.TrimSpace(key), strings.TrimSpace(envName), nil
	case "cloudflare":
		envName := ProviderCredentialEnvName(cfg, "cloudflare")
		value := strings.TrimSpace(ResolveSecret(envName))
		if value == "" {
			value = strings.TrimSpace(ResolveSecret(CloudflareAPITokenEnv))
			if value != "" {
				envName = CloudflareAPITokenEnv
			}
		}
		return value, envName, nil
	default:
		envName := ProviderCredentialEnvName(cfg, target)
		if envName == "" {
			return "", "", fmt.Errorf("%w %q", ErrUnsupportedProvider, target)
		}
		return strings.TrimSpace(ResolveSecret(envName)), envName, nil
	}
}

func ProviderCredentialTargetForProfile(profile framework.ProviderProfile) string {
	if !framework.ProviderProfileRequiresCredential(profile) {
		return ""
	}
	return framework.ProviderCredentialTarget(profile)
}

func ResolveProviderCredentialValueForProfile(cfg *Config, profile framework.ProviderProfile) (string, string, error) {
	target := ProviderCredentialTargetForProfile(profile)
	if target == "" {
		return "", "", nil
	}
	return ResolveProviderCredentialValue(cfg, target)
}

func ProviderCredentialAvailable(cfg *Config, target string) bool {
	target = NormalizeProviderCredentialTarget(target)
	if target == "" {
		return true
	}
	value, _, err := ResolveProviderCredentialValue(cfg, target)
	return err == nil && strings.TrimSpace(value) != ""
}

func ProviderCredentialAvailableForProfile(cfg *Config, profile framework.ProviderProfile) bool {
	return ProviderCredentialAvailable(cfg, ProviderCredentialTargetForProfile(profile))
}

func ProviderCredentialStatuses(cfg *Config) []ProviderCredentialStatus {
	targets := ProviderCredentialTargets()
	out := make([]ProviderCredentialStatus, 0, len(targets))
	for _, target := range targets {
		out = append(out, ProviderCredentialStatusFor(cfg, target))
	}
	return out
}

func ProviderCredentialStatusFor(cfg *Config, target string) ProviderCredentialStatus {
	target = NormalizeProviderCredentialTarget(target)
	status := ProviderCredentialStatus{
		Target:   target,
		Provider: ProviderForCredentialTarget(target),
		Label:    ProviderLabel(target),
		EnvName:  ProviderCredentialEnvName(cfg, target),
		Source:   string(secrets.TokenSourceNone),
	}
	if target == "" {
		return status
	}

	var tokenStatus secrets.TokenStatus
	var err error
	switch target {
	case "huggingface":
		tokenStatus, err = secrets.HuggingFaceTokenStatus(func() string {
			return ResolveSecretFromEnvironmentOrDoppler(HuggingFaceTokenEnvName(cfg))
		})
	default:
		envName := ProviderCredentialEnvName(cfg, target)
		tokenStatus, err = secrets.NamedSecretStatus(envName, func() string {
			return ResolveSecretFromEnvironmentOrDoppler(envName)
		})
	}
	if err != nil {
		return status
	}
	status.HasStoredSecret = tokenStatus.HasUserToken || tokenStatus.HasInstallToken
	status.Source = string(tokenStatus.ActiveSource)
	status.Available = tokenStatus.ActiveSource != secrets.TokenSourceNone

	if !status.Available {
		if value, envName, err := ResolveProviderCredentialValue(cfg, target); err == nil && strings.TrimSpace(value) != "" {
			status.Available = true
			status.Source = string(secrets.TokenSourceEnv)
			if strings.TrimSpace(envName) != "" {
				status.EnvName = strings.TrimSpace(envName)
			}
		}
	}
	return status
}

func ProviderEnabledForProfile(cfg *Config, profile framework.ProviderProfile) bool {
	return ProviderEnabled(cfg, framework.ProviderIDForProfile(profile), framework.NormalizeMode(profile.Mode))
}

func ProviderEnabled(cfg *Config, provider string, mode framework.Mode) bool {
	if cfg == nil {
		return true
	}
	switch framework.NormalizeProviderID(provider) {
	case "local":
		switch framework.NormalizeMode(mode) {
		case framework.ModeDictation:
			return cfg.Local.Enabled
		case framework.ModeAssist:
			return cfg.LocalLLM.Enabled
		case framework.ModeVoiceAgent:
			return cfg.VoiceAgent.Enabled
		default:
			return true
		}
	case "ollama":
		return cfg.Providers.Ollama.Enabled
	case "huggingface":
		return cfg.HuggingFace.Enabled
	case "openai":
		return cfg.Providers.OpenAI.Enabled
	case "groq":
		return cfg.Providers.Groq.Enabled
	case "google":
		return cfg.Providers.Google.Enabled
	case "deepgram":
		return cfg.Providers.Deepgram.Enabled
	case "assemblyai":
		return cfg.Providers.AssemblyAI.Enabled
	case "cloudflare":
		return cfg.Providers.Cloudflare.Enabled
	case "openrouter":
		return cfg.Providers.OpenRouter.Enabled
	case "openedai", "selfhosted":
		return true
	default:
		return true
	}
}

func SetProviderEnabled(cfg *Config, provider string, enabled bool) error {
	if cfg == nil {
		return fmt.Errorf("settings unavailable")
	}
	provider = framework.NormalizeProviderID(provider)
	switch provider {
	case "huggingface":
		cfg.HuggingFace.Enabled = enabled
		if enabled && strings.TrimSpace(cfg.HuggingFace.Model) == "" {
			cfg.HuggingFace.Model = "openai/whisper-large-v3-turbo"
		}
	case "openai":
		cfg.Providers.OpenAI.Enabled = enabled
	case "groq":
		cfg.Providers.Groq.Enabled = enabled
	case "google":
		cfg.Providers.Google.Enabled = enabled
	case "openrouter":
		cfg.Providers.OpenRouter.Enabled = enabled
		if enabled && strings.TrimSpace(cfg.Providers.OpenRouter.STTModel) == "" {
			cfg.Providers.OpenRouter.STTModel = "openai/whisper-1"
		}
	case "ollama":
		cfg.Providers.Ollama.Enabled = enabled
		if enabled && strings.TrimSpace(cfg.Providers.Ollama.BaseURL) == "" {
			cfg.Providers.Ollama.BaseURL = "http://localhost:11434"
		}
	case "deepgram":
		cfg.Providers.Deepgram.Enabled = enabled
		if enabled && strings.TrimSpace(cfg.Providers.Deepgram.STTModel) == "" {
			cfg.Providers.Deepgram.STTModel = "nova-3"
		}
	case "assemblyai":
		cfg.Providers.AssemblyAI.Enabled = enabled
		if enabled {
			EnableAlwaysOnLLM(cfg)
		}
	case "cloudflare":
		cfg.Providers.Cloudflare.Enabled = enabled
		if enabled {
			EnableAlwaysOnLLM(cfg)
		}
	default:
		return fmt.Errorf("%w %q", ErrUnsupportedProvider, provider)
	}
	return nil
}

func cloneProviderRuntime(runtime ProviderRuntime) ProviderRuntime {
	runtime.SupportedModes = append([]framework.Mode(nil), runtime.SupportedModes...)
	return runtime
}

var providerRuntimeRegistry = []ProviderRuntime{
	{
		Provider:         "local",
		DisplayName:      "Local Built-in",
		ProviderKind:     framework.ProviderKindLocalBuiltIn,
		IntegrationKind:  ProviderIntegrationLocal,
		SupportedModes:   []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeVoiceAgent, framework.ModeTTS},
		UserConfigurable: false,
	},
	{
		Provider:         "ollama",
		DisplayName:      "Ollama",
		ProviderKind:     framework.ProviderKindLocalProvider,
		IntegrationKind:  ProviderIntegrationLocal,
		SetupURL:         "https://ollama.com/download",
		SupportedModes:   []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeVoiceAgent},
		UserConfigurable: true,
	},
	{
		Provider:           "huggingface",
		DisplayName:        "Hugging Face",
		ProviderKind:       framework.ProviderKindCloudProvider,
		IntegrationKind:    ProviderIntegrationCloudGateway,
		CredentialTarget:   "huggingface",
		CredentialRequired: true,
		SetupURL:           "https://huggingface.co/settings/tokens",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeVoiceAgent, framework.ModeTTS},
		UserConfigurable:   true,
	},
	{
		Provider:           "openrouter",
		DisplayName:        "OpenRouter",
		ProviderKind:       framework.ProviderKindCloudProvider,
		IntegrationKind:    ProviderIntegrationCloudGateway,
		CredentialTarget:   "openrouter",
		CredentialRequired: true,
		SetupURL:           "https://openrouter.ai/settings/keys",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeVoiceAgent},
		UserConfigurable:   true,
	},
	{
		Provider:           "openai",
		DisplayName:        "OpenAI",
		ProviderKind:       framework.ProviderKindDirectProvider,
		IntegrationKind:    ProviderIntegrationDirectAPI,
		CredentialTarget:   "openai",
		CredentialRequired: true,
		SetupURL:           "https://platform.openai.com/api-keys",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeTTS},
		UserConfigurable:   true,
	},
	{
		Provider:           "google",
		DisplayName:        "Gemini / Google AI",
		ProviderKind:       framework.ProviderKindDirectProvider,
		IntegrationKind:    ProviderIntegrationDirectAPI,
		CredentialTarget:   "google",
		CredentialRequired: true,
		SetupURL:           "https://aistudio.google.com/apikey",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeVoiceAgent, framework.ModeTTS},
		UserConfigurable:   true,
	},
	{
		Provider:           "groq",
		DisplayName:        "Groq",
		ProviderKind:       framework.ProviderKindDirectProvider,
		IntegrationKind:    ProviderIntegrationDirectAPI,
		CredentialTarget:   "groq",
		CredentialRequired: true,
		SetupURL:           "https://console.groq.com/keys",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeAssist},
		UserConfigurable:   true,
	},
	{
		Provider:           "deepgram",
		DisplayName:        "Deepgram",
		ProviderKind:       framework.ProviderKindDirectProvider,
		IntegrationKind:    ProviderIntegrationDirectAPI,
		CredentialTarget:   "deepgram",
		CredentialRequired: true,
		SetupURL:           "https://console.deepgram.com/",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeVoiceAgent, framework.ModeTTS},
		UserConfigurable:   true,
	},
	{
		Provider:           "assemblyai",
		DisplayName:        "AssemblyAI",
		ProviderKind:       framework.ProviderKindDirectProvider,
		IntegrationKind:    ProviderIntegrationDirectAPI,
		CredentialTarget:   "assemblyai",
		CredentialRequired: true,
		SetupURL:           "https://www.assemblyai.com/app/account",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeVoiceAgent},
		UserConfigurable:   true,
	},
	{
		Provider:           "cloudflare",
		DisplayName:        "Cloudflare AI Gateway",
		ProviderKind:       framework.ProviderKindCloudProvider,
		IntegrationKind:    ProviderIntegrationCloudGateway,
		CredentialTarget:   "cloudflare",
		CredentialRequired: true,
		SetupURL:           "https://developers.cloudflare.com/ai-gateway/get-started/",
		SupportedModes:     []framework.Mode{framework.ModeAssist, framework.ModeVoiceAgent},
		UserConfigurable:   true,
	},
	{
		Provider:         "openedai",
		DisplayName:      "OpenAI-compatible local",
		ProviderKind:     framework.ProviderKindLocalProvider,
		IntegrationKind:  ProviderIntegrationLocal,
		SupportedModes:   []framework.Mode{framework.ModeTTS},
		UserConfigurable: false,
	},
	{
		Provider:         "selfhosted",
		DisplayName:      "Self-hosted HTTP",
		ProviderKind:     framework.ProviderKindLocalProvider,
		IntegrationKind:  ProviderIntegrationLocal,
		SupportedModes:   []framework.Mode{framework.ModeDictation},
		UserConfigurable: false,
	},
}
