package config

import (
	"fmt"
	"strings"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

func ProviderEnabledForProfile(cfg *Config, profile framework.ProviderProfile) bool {
	return ProviderEnabled(cfg, catalog.ProviderIDForProfile(profile), framework.NormalizeMode(profile.Mode))
}

func ProviderEnabled(cfg *Config, provider string, mode framework.Mode) bool {
	if cfg == nil {
		return true
	}
	switch catalog.NormalizeProviderID(provider) {
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
	case "foundry", "foundry-voicelive":
		return cfg.Providers.Foundry.Enabled
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
	provider = catalog.NormalizeProviderID(provider)
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
	case "foundry", "foundry-voicelive":
		cfg.Providers.Foundry.Enabled = enabled
		if enabled {
			EnableAlwaysOnLLM(cfg)
		}
	default:
		return fmt.Errorf("%w %q", ErrUnsupportedProvider, provider)
	}
	return nil
}
