package config

import (
	"errors"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

const (
	OpenAIAPIKeyEnv     = "OPENAI_API_KEY"
	GroqAPIKeyEnv       = "GROQ_API_KEY"
	OpenRouterAPIKeyEnv = "OPENROUTER_API_KEY"
	// AzureAIAPIKeyEnv is the default env var for the Microsoft Foundry
	// (Azure AI) API key.
	AzureAIAPIKeyEnv = "AZURE_AI_API_KEY"

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
	provider = catalog.NormalizeProviderID(provider)
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

func cloneProviderRuntime(runtime ProviderRuntime) ProviderRuntime {
	runtime.SupportedModes = append([]framework.Mode(nil), runtime.SupportedModes...)
	return runtime
}
