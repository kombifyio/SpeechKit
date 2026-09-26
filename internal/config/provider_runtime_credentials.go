package config

import (
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/secrets"
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

type ProviderCredentialStatus struct {
	Provider        string
	Target          string
	Label           string
	EnvName         string
	Available       bool
	HasStoredSecret bool
	Source          string
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
	provider := catalog.NormalizeProviderID(target)
	switch provider {
	case "openai", "groq", "google", "deepgram", "assemblyai", "huggingface", "openrouter", "cloudflare", "foundry":
		return provider
	case "foundry-voicelive", "foundry-realtime", "microsoft-foundry":
		// Every Foundry surface (OpenAI-compatible, Voice Live, Azure Speech)
		// authenticates with the one resource key from [providers.foundry].
		return "foundry"
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
		"foundry",
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
	case "foundry":
		if cfg != nil && strings.TrimSpace(cfg.Providers.Foundry.APIKeyEnv) != "" {
			return strings.TrimSpace(cfg.Providers.Foundry.APIKeyEnv)
		}
		return AzureAIAPIKeyEnv
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
	case "foundry":
		cfg.Providers.Foundry.APIKeyEnv = envName
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
	if !catalog.ProviderProfileRequiresCredential(profile) {
		return ""
	}
	return catalog.ProviderCredentialTarget(profile)
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
