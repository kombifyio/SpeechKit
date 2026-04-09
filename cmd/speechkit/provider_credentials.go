package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/secrets"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

type providerCredentialState struct {
	Provider        string `json:"provider"`
	Label           string `json:"label"`
	EnvName         string `json:"envName,omitempty"`
	Available       bool   `json:"available"`
	HasStoredSecret bool   `json:"hasStoredSecret"`
	Source          string `json:"source"`
}

func providerCredentialStates(cfg *config.Config) map[string]providerCredentialState {
	states := map[string]providerCredentialState{
		"openai":     namedProviderCredentialState("openai", "OpenAI", cfg.Providers.OpenAI.APIKeyEnv),
		"groq":       namedProviderCredentialState("groq", "Groq", cfg.Providers.Groq.APIKeyEnv),
		"google":     namedProviderCredentialState("google", "Google", cfg.Providers.Google.APIKeyEnv),
		"openrouter": namedProviderCredentialState("openrouter", "OpenRouter", cfg.Providers.OpenRouter.APIKeyEnv),
	}
	if config.ManagedHuggingFaceAvailableInBuild() {
		states["huggingface"] = huggingFaceCredentialState(cfg)
	}
	return states
}

func namedProviderCredentialState(provider, label, envName string) providerCredentialState {
	status, err := secrets.NamedSecretStatus(envName, func() string {
		return config.ResolveSecretFromEnvironmentOrDoppler(envName)
	})
	state := providerCredentialState{
		Provider: provider,
		Label:    label,
		EnvName:  strings.TrimSpace(envName),
		Source:   string(secrets.TokenSourceNone),
	}
	if err != nil {
		return state
	}
	state.HasStoredSecret = status.HasUserToken
	state.Source = string(status.ActiveSource)
	state.Available = status.ActiveSource != secrets.TokenSourceNone
	return state
}

func huggingFaceCredentialState(cfg *config.Config) providerCredentialState {
	status, err := secrets.HuggingFaceTokenStatus(func() string {
		return config.ResolveSecretFromEnvironmentOrDoppler(config.HuggingFaceTokenEnvName(cfg))
	})
	state := providerCredentialState{
		Provider: "huggingface",
		Label:    "Hugging Face",
		EnvName:  config.HuggingFaceTokenEnvName(cfg),
		Source:   string(secrets.TokenSourceNone),
	}
	if err != nil {
		return state
	}
	state.HasStoredSecret = status.HasUserToken || status.HasInstallToken
	state.Source = string(status.ActiveSource)
	state.Available = status.ActiveSource != secrets.TokenSourceNone
	return state
}

func profileCredentialAvailable(cfg *config.Config, profile models.Profile) bool {
	switch profile.ExecutionMode {
	case models.ExecutionModeLocal, models.ExecutionModeOllama:
		return true
	case models.ExecutionModeHFRouted:
		if !config.ManagedHuggingFaceAvailableInBuild() {
			return false
		}
		token, _, err := config.ResolveHuggingFaceToken(cfg)
		return err == nil && strings.TrimSpace(token) != ""
	case models.ExecutionModeOpenAI:
		return strings.TrimSpace(config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv)) != ""
	case models.ExecutionModeGroq:
		return strings.TrimSpace(config.ResolveSecret(cfg.Providers.Groq.APIKeyEnv)) != ""
	case models.ExecutionModeGoogle:
		return strings.TrimSpace(config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)) != ""
	case models.ExecutionModeOpenRouter:
		return strings.TrimSpace(config.ResolveSecret(cfg.Providers.OpenRouter.APIKeyEnv)) != ""
	default:
		return false
	}
}

func providerLabel(provider string) string {
	switch strings.TrimSpace(strings.ToLower(provider)) {
	case "openai":
		return "OpenAI"
	case "groq":
		return "Groq"
	case "google":
		return "Google"
	case "huggingface":
		return "Hugging Face"
	case "openrouter":
		return "OpenRouter"
	default:
		return "Provider"
	}
}

func providerSecretEnvName(cfg *config.Config, provider string) string {
	switch strings.TrimSpace(strings.ToLower(provider)) {
	case "openai":
		return cfg.Providers.OpenAI.APIKeyEnv
	case "groq":
		return cfg.Providers.Groq.APIKeyEnv
	case "google":
		return cfg.Providers.Google.APIKeyEnv
	case "openrouter":
		return cfg.Providers.OpenRouter.APIKeyEnv
	default:
		return ""
	}
}

func saveProviderCredential(ctx context.Context, provider, secret string, cfg *config.Config, sttRouter *router.Router) (string, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	secret = strings.TrimSpace(secret)
	if provider == "" {
		return "", fmt.Errorf("provider is required")
	}
	if secret == "" {
		return "", fmt.Errorf("credential is required")
	}

	if provider == "huggingface" {
		if !config.ManagedHuggingFaceAvailableInBuild() {
			return "", errHFUnavailableBuild
		}
		if err := secrets.SetUserHuggingFaceToken(secret); err != nil {
			return "", err
		}
		cfg.HuggingFace.Enabled = true
		if strings.TrimSpace(cfg.HuggingFace.Model) == "" {
			cfg.HuggingFace.Model = "openai/whisper-large-v3"
		}
		if sttRouter != nil {
			if err := refreshHuggingFaceProvider(ctx, cfg, sttRouter, true); err != nil && err != errMissingHuggingFaceToken {
				return "", err
			}
		}
		return "Hugging Face token saved", nil
	}

	envName := providerSecretEnvName(cfg, provider)
	if strings.TrimSpace(envName) == "" {
		return "", fmt.Errorf("unsupported provider %q", provider)
	}
	if err := secrets.SetNamedSecret(envName, secret); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s API key saved", providerLabel(provider)), nil
}

func clearProviderCredential(ctx context.Context, provider string, cfg *config.Config, sttRouter *router.Router) (string, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		return "", fmt.Errorf("provider is required")
	}

	if provider == "huggingface" {
		if !config.ManagedHuggingFaceAvailableInBuild() {
			return "", errHFUnavailableBuild
		}
		if err := secrets.ClearUserHuggingFaceToken(); err != nil {
			return "", err
		}
		if sttRouter != nil && cfg.HuggingFace.Enabled {
			if err := refreshHuggingFaceProvider(ctx, cfg, sttRouter, true); err == errMissingHuggingFaceToken {
				sttRouter.SetHuggingFace(nil)
			} else if err != nil {
				return "", err
			}
		}
		return "Hugging Face token cleared", nil
	}

	envName := providerSecretEnvName(cfg, provider)
	if strings.TrimSpace(envName) == "" {
		return "", fmt.Errorf("unsupported provider %q", provider)
	}
	if err := secrets.ClearNamedSecret(envName); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s API key cleared", providerLabel(provider)), nil
}

func testProviderCredential(ctx context.Context, provider, secret string, cfg *config.Config) (string, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	secret = strings.TrimSpace(secret)
	if provider == "" {
		return "", fmt.Errorf("provider is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	testCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if secret == "" {
		resolved, err := resolveProviderCredential(provider, cfg)
		if err != nil {
			return "", err
		}
		secret = resolved
	}
	if secret == "" {
		return "", fmt.Errorf("%s credential is missing", providerLabel(provider))
	}

	var providerClient stt.STTProvider
	switch provider {
	case "huggingface":
		if !config.ManagedHuggingFaceAvailableInBuild() {
			return "", errHFUnavailableBuild
		}
		modelID := strings.TrimSpace(cfg.HuggingFace.Model)
		if modelID == "" {
			modelID = "openai/whisper-large-v3"
		}
		providerClient = newHuggingFaceProvider(modelID, secret)
	case "openai":
		providerClient = stt.NewOpenAICompatibleProvider("openai", "https://api.openai.com", secret, cfg.Providers.OpenAI.STTModel)
	case "groq":
		providerClient = stt.NewOpenAICompatibleProvider("groq", "https://api.groq.com/openai", secret, cfg.Providers.Groq.STTModel)
	case "google":
		providerClient = stt.NewGoogleSTTProvider(secret, cfg.Providers.Google.STTModel)
	default:
		return "", fmt.Errorf("unsupported provider %q", provider)
	}

	if err := providerClient.Health(testCtx); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s credential verified", providerLabel(provider)), nil
}

func resolveProviderCredential(provider string, cfg *config.Config) (string, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "huggingface" {
		token, _, err := config.ResolveHuggingFaceToken(cfg)
		return strings.TrimSpace(token), err
	}
	envName := providerSecretEnvName(cfg, provider)
	if strings.TrimSpace(envName) == "" {
		return "", fmt.Errorf("unsupported provider %q", provider)
	}
	return strings.TrimSpace(config.ResolveSecret(envName)), nil
}
