package main

import (
	"context"
	"fmt"
	"github.com/kombifyio/SpeechKit/cmd/speechkit/internal/profiles"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
)

const (
	integrationKindCloudGateway = "cloud_gateway"
	integrationKindDirectAPI    = "direct_api"
	integrationKindLocal        = "local_provider"
)

type providerIntegrationState struct {
	Provider           string   `json:"provider"`
	Label              string   `json:"label"`
	Enabled            bool     `json:"enabled"`
	ProviderKind       string   `json:"providerKind"`
	IntegrationKind    string   `json:"integrationKind"`
	CredentialRequired bool     `json:"credentialRequired"`
	Available          bool     `json:"available"`
	HasStoredSecret    bool     `json:"hasStoredSecret"`
	Source             string   `json:"source"`
	EnvName            string   `json:"envName,omitempty"`
	SetupURL           string   `json:"setupUrl,omitempty"`
	SupportedModes     []string `json:"supportedModes"`
}

type providerIntegrationSpec struct {
	Provider           string
	Label              string
	ProviderKind       models.ProviderKind
	IntegrationKind    string
	CredentialRequired bool
	EnvName            string
	SetupURL           string
	SupportedModes     []string
}

func providerIntegrationStates(cfg *config.Config) map[string]providerIntegrationState {
	credentials := providerCredentialStates(cfg)
	specs := providerIntegrationSpecs(cfg)
	states := make(map[string]providerIntegrationState, len(specs))

	for _, spec := range specs {
		credential := credentials[spec.Provider]
		if credential.Provider == "" {
			credential = providerCredentialState{
				Provider: spec.Provider,
				Label:    spec.Label,
				EnvName:  spec.EnvName,
				Source:   "none",
			}
		}

		available := true
		if spec.CredentialRequired {
			available = credential.Available
		}
		states[spec.Provider] = providerIntegrationState{
			Provider:           spec.Provider,
			Label:              spec.Label,
			Enabled:            providerIntegrationEnabled(cfg, spec.Provider),
			ProviderKind:       string(spec.ProviderKind),
			IntegrationKind:    spec.IntegrationKind,
			CredentialRequired: spec.CredentialRequired,
			Available:          available,
			HasStoredSecret:    credential.HasStoredSecret,
			Source:             credential.Source,
			EnvName:            firstNonEmpty(credential.EnvName, spec.EnvName),
			SetupURL:           spec.SetupURL,
			SupportedModes:     append([]string(nil), spec.SupportedModes...),
		}
	}

	return states
}

func providerIntegrationSpecs(cfg *config.Config) []providerIntegrationSpec {
	return []providerIntegrationSpec{
		{
			Provider:           "huggingface",
			Label:              "Hugging Face",
			ProviderKind:       models.ProviderKindCloudProvider,
			IntegrationKind:    integrationKindCloudGateway,
			CredentialRequired: true,
			EnvName:            config.HuggingFaceTokenEnvName(cfg),
			SetupURL:           "https://huggingface.co/settings/tokens",
			SupportedModes:     []string{modeDictate, modeAssist, modeVoiceAgent},
		},
		{
			Provider:           "openai",
			Label:              "OpenAI",
			ProviderKind:       models.ProviderKindDirectProvider,
			IntegrationKind:    integrationKindDirectAPI,
			CredentialRequired: true,
			EnvName:            providerSecretEnvName(cfg, "openai"),
			SetupURL:           "https://platform.openai.com/api-keys",
			SupportedModes:     []string{modeDictate, modeAssist},
		},
		{
			Provider:           "google",
			Label:              "Gemini / Google AI",
			ProviderKind:       models.ProviderKindDirectProvider,
			IntegrationKind:    integrationKindDirectAPI,
			CredentialRequired: true,
			EnvName:            providerSecretEnvName(cfg, "google"),
			SetupURL:           "https://aistudio.google.com/apikey",
			SupportedModes:     []string{modeDictate, modeAssist, modeVoiceAgent},
		},
		{
			Provider:           "openrouter",
			Label:              "OpenRouter",
			ProviderKind:       models.ProviderKindCloudProvider,
			IntegrationKind:    integrationKindCloudGateway,
			CredentialRequired: true,
			EnvName:            providerSecretEnvName(cfg, "openrouter"),
			SetupURL:           "https://openrouter.ai/settings/keys",
			SupportedModes:     []string{modeDictate, modeAssist, modeVoiceAgent},
		},
		{
			Provider:           "ollama",
			Label:              "Ollama",
			ProviderKind:       models.ProviderKindLocalProvider,
			IntegrationKind:    integrationKindLocal,
			CredentialRequired: false,
			SetupURL:           "https://ollama.com/download",
			SupportedModes:     []string{modeDictate, modeAssist, modeVoiceAgent},
		},
		{
			Provider:           "groq",
			Label:              "Groq",
			ProviderKind:       models.ProviderKindDirectProvider,
			IntegrationKind:    integrationKindDirectAPI,
			CredentialRequired: true,
			EnvName:            providerSecretEnvName(cfg, "groq"),
			SetupURL:           "https://console.groq.com/keys",
			SupportedModes:     []string{modeDictate, modeAssist},
		},
	}
}

func providerIntegrationEnabled(cfg *config.Config, provider string) bool {
	if cfg == nil {
		return false
	}
	provider = normalizeProviderKey(provider)
	switch provider {
	case "huggingface":
		return cfg.HuggingFace.Enabled || selectedProviderInConfig(cfg, provider)
	case "openai":
		return cfg.Providers.OpenAI.Enabled || selectedProviderInConfig(cfg, provider)
	case "groq":
		return cfg.Providers.Groq.Enabled || selectedProviderInConfig(cfg, provider)
	case "google":
		return cfg.Providers.Google.Enabled || selectedProviderInConfig(cfg, provider)
	case "openrouter":
		return cfg.Providers.OpenRouter.Enabled || selectedProviderInConfig(cfg, provider)
	case "ollama":
		return cfg.Providers.Ollama.Enabled || selectedProviderInConfig(cfg, provider)
	default:
		return false
	}
}

func selectedProviderInConfig(cfg *config.Config, provider string) bool {
	if cfg == nil {
		return false
	}
	catalog := filteredModelCatalog()
	for _, selection := range []config.ModeModelSelection{
		cfg.ModelSelection.Dictate,
		cfg.ModelSelection.Assist,
		cfg.ModelSelection.VoiceAgent,
	} {
		for _, profileID := range []string{selection.PrimaryProfileID, selection.FallbackProfileID} {
			profile, ok := profiles.FindCatalogProfile(catalog, profileID)
			if !ok {
				continue
			}
			if providerForProfile(profile) == provider {
				return true
			}
		}
	}
	return false
}

func updateProviderIntegration(ctx context.Context, cfgPath, provider string, enabled bool, cfg *config.Config, state *appState, sttRouter *router.Router) (string, error) {
	provider = normalizeProviderKey(provider)
	if provider == "" {
		return "", fmt.Errorf("provider is required")
	}
	if err := setProviderIntegrationEnabled(cfg, provider, enabled); err != nil {
		return "", err
	}
	if !enabled {
		resetSelectionsForDisabledProvider(cfg, provider)
	}
	if err := refreshProviderRuntimes(ctx, cfg, state, sttRouter); err != nil {
		return "", err
	}
	if strings.TrimSpace(cfgPath) != "" {
		if err := config.Save(cfgPath, cfg); err != nil {
			return "", err
		}
	}
	if enabled {
		return fmt.Sprintf("%s integration enabled", providerLabel(provider)), nil
	}
	return fmt.Sprintf("%s integration disabled", providerLabel(provider)), nil
}

func setProviderIntegrationEnabled(cfg *config.Config, provider string, enabled bool) error {
	if cfg == nil {
		return fmt.Errorf("settings unavailable")
	}
	switch normalizeProviderKey(provider) {
	case "huggingface":
		if enabled && !config.ManagedHuggingFaceAvailableInBuild() {
			return errHFUnavailableBuild
		}
		cfg.HuggingFace.Enabled = enabled
		if enabled && strings.TrimSpace(cfg.HuggingFace.Model) == "" {
			cfg.HuggingFace.Model = "openai/whisper-large-v3"
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
	default:
		return fmt.Errorf("unsupported provider %q", provider)
	}
	return nil
}

func resetSelectionsForDisabledProvider(cfg *config.Config, provider string) {
	if cfg == nil {
		return
	}
	resetSelectionForDisabledProvider(&cfg.ModelSelection.Dictate, provider, config.DefaultDictatePrimaryProfileID)
	resetSelectionForDisabledProvider(&cfg.ModelSelection.Assist, provider, config.DefaultAssistPrimaryProfileID)
	resetSelectionForDisabledProvider(&cfg.ModelSelection.VoiceAgent, provider, config.DefaultVoiceAgentPrimaryProfileID)
}

func resetSelectionForDisabledProvider(selection *config.ModeModelSelection, provider, defaultPrimaryProfileID string) {
	if selection == nil {
		return
	}
	catalog := filteredModelCatalog()
	primaryDisabled := profileIDBelongsToProvider(catalog, selection.PrimaryProfileID, provider)
	fallbackDisabled := profileIDBelongsToProvider(catalog, selection.FallbackProfileID, provider)
	if primaryDisabled {
		selection.PrimaryProfileID = defaultPrimaryProfileID
		selection.FallbackProfileID = ""
		if strings.TrimSpace(selection.ModeSource) == "" {
			selection.ModeSource = config.ModeSourceLocal
		}
		return
	}
	if fallbackDisabled {
		selection.FallbackProfileID = ""
	}
}

func profileIDBelongsToProvider(catalog models.Catalog, profileID, provider string) bool {
	profile, ok := profiles.FindCatalogProfile(catalog, profileID)
	return ok && providerForProfile(profile) == provider
}

func providerForProfile(profile models.Profile) string {
	switch profile.ExecutionMode {
	case models.ExecutionModeHFRouted:
		return "huggingface"
	case models.ExecutionModeOpenAI:
		return "openai"
	case models.ExecutionModeGroq:
		return "groq"
	case models.ExecutionModeGoogle:
		return "google"
	case models.ExecutionModeOpenRouter:
		return "openrouter"
	case models.ExecutionModeOllama:
		return "ollama"
	default:
		return ""
	}
}

func parseSettingsBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func normalizeProviderKey(provider string) string {
	return strings.TrimSpace(strings.ToLower(provider))
}
