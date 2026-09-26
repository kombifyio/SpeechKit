package catalog

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// NormalizeProviderID maps a provider alias or a "<mode>.<provider>.<model>"
// profile id to its canonical provider id. Profile ids are reduced to their
// provider segment first, so every mode (stt, assist, utility, realtime, tts,
// speaker) shares one alias table; third-party providers pass through as-is.
func NormalizeProviderID(provider string) string {
	value := strings.ToLower(strings.TrimSpace(provider))
	value = strings.ReplaceAll(value, "_", "-")
	if value == "" {
		return ""
	}
	if segment, ok := providerSegmentFromProfileID(value); ok {
		value = segment
	}
	switch value {
	case "builtin", "local-built-in", "local":
		return "local"
	case "hf", "hf-routed", "routed", "hugging-face", "huggingface":
		return "huggingface"
	case "open-router", "openrouter":
		return "openrouter"
	case "google-ai", "google-cloud", "gemini", "gemini-live", "google":
		return "google"
	case "assembly-ai", "assemblyai":
		return "assemblyai"
	case "openai-compatible", "openedai-speech", "openedai":
		return "openedai"
	default:
		return value
	}
}

var profileIDModePrefixes = []string{"stt.", "assist.", "utility.", "realtime.", "tts.", "speaker."}

func providerSegmentFromProfileID(value string) (string, bool) {
	for _, prefix := range profileIDModePrefixes {
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		rest := value[len(prefix):]
		provider, _, found := strings.Cut(rest, ".")
		if !found || provider == "" {
			return "", false
		}
		return provider, true
	}
	return "", false
}

// ProviderIDForProfile resolves the canonical provider id of profile: the
// explicit Provider field first, then its ID (a "<mode>.<provider>.<model>"
// profile id reduced to the provider segment, or a bare provider alias), then
// the id implied by its ExecutionMode. It returns "" when none of them names
// a provider.
func ProviderIDForProfile(profile speechkit.ProviderProfile) string {
	if provider := NormalizeProviderID(profile.Provider); provider != "" {
		return provider
	}
	if provider := NormalizeProviderID(profile.ID); provider != "" && !strings.Contains(provider, ".") {
		return provider
	}
	return ProviderIDForExecutionMode(profile.ExecutionMode)
}

// ProviderIDForExecutionMode maps a runtime execution mode to the canonical
// provider id it implies ("local", "selfhosted", "huggingface", "openai",
// "groq", "deepgram", "assemblyai", "ollama", "openrouter" or "foundry");
// "" for a mode that does not imply a single provider.
func ProviderIDForExecutionMode(mode speechkit.ExecutionMode) string {
	switch mode {
	case speechkit.ExecutionModeLocal:
		return "local"
	case speechkit.ExecutionModeSelfHostedHTTP:
		return "selfhosted"
	case speechkit.ExecutionModeHFRouted:
		return "huggingface"
	case speechkit.ExecutionModeOpenAI:
		return "openai"
	case speechkit.ExecutionModeGroq:
		return "groq"
	case speechkit.ExecutionModeGoogle:
		return "google"
	case speechkit.ExecutionModeDeepgram:
		return "deepgram"
	case speechkit.ExecutionModeAssemblyAI:
		return "assemblyai"
	case speechkit.ExecutionModeOllama:
		return "ollama"
	case speechkit.ExecutionModeOpenRouter:
		return "openrouter"
	case speechkit.ExecutionModeFoundry:
		return "foundry"
	default:
		return ""
	}
}

func providerDisplayName(provider string) string {
	switch NormalizeProviderID(provider) {
	case "local":
		return "Local Built-in"
	case "ollama":
		return "Ollama"
	case "huggingface":
		return "Hugging Face"
	case "openrouter":
		return "OpenRouter"
	case "openai":
		return "OpenAI"
	case "google":
		return "Google"
	case "deepgram":
		return "Deepgram"
	case "assemblyai":
		return "AssemblyAI"
	case "groq":
		return "Groq"
	case "foundry":
		return "Microsoft Foundry"
	case "foundry-voicelive":
		return "Microsoft Foundry Voice Live"
	case "cloudflare":
		return "Cloudflare"
	case "piper":
		return "Piper"
	case "openedai":
		return "OpenAI-compatible local"
	case "selfhosted":
		return "Self-hosted HTTP"
	default:
		return strings.TrimSpace(provider)
	}
}

func providerOrderIndex(provider string) int {
	order := []string{
		"local",
		"ollama",
		"huggingface",
		"openrouter",
		"openai",
		"google",
		"deepgram",
		"assemblyai",
		"foundry",
		"foundry-voicelive",
		"groq",
		"cloudflare",
		"piper",
		"openedai",
		"selfhosted",
	}
	provider = NormalizeProviderID(provider)
	for i, candidate := range order {
		if candidate == provider {
			return i
		}
	}
	return len(order)
}
