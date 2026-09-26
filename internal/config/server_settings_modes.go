package config

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

func applyServerModeProviderSettings(cfg *Config, modes ServerModeProviderSettings) []string {
	var notes []string
	if modeSettingDefined(modes.Dictation) {
		notes = append(notes, applyDictationModeSetting(cfg, modes.Dictation)...)
	}
	if modeSettingDefined(modes.Assist) {
		notes = append(notes, applyAssistModeSetting(cfg, modes.Assist)...)
	}
	if modeSettingDefined(modes.VoiceAgent) {
		notes = append(notes, applyVoiceAgentModeSetting(cfg, modes.VoiceAgent)...)
	}
	return notes
}

func applyDictationModeSetting(cfg *Config, mode ServerModeSetting) []string {
	var notes []string
	kind := normalizedProviderKind(mode)
	model := cleanSetting(mode.Model)
	if mode.Enabled != nil && !*mode.Enabled {
		cfg.General.DictateEnabled = false
		notes = append(notes, "server settings: Dictation disabled")
		return notes
	}
	switch kind {
	case "direct_provider":
		cfg.Local.Enabled = false
		cfg.Routing.Strategy = "cloud-only"
		cfg.VPS.Enabled = false
		applyDirectDictationProvider(cfg, providerForModeSetting(mode), model)
		notes = append(notes, "server settings: Dictation uses direct provider")
	case "cloud_provider":
		cfg.Local.Enabled = false
		cfg.Routing.Strategy = "cloud-only"
		cfg.VPS.Enabled = false
		if providerForModeSetting(mode) == "openrouter" {
			cfg.HuggingFace.Enabled = false
			cfg.Providers.OpenRouter.Enabled = true
			if model != "" {
				cfg.Providers.OpenRouter.STTModel = model
			}
		} else {
			cfg.HuggingFace.Enabled = true
			if model != "" {
				cfg.HuggingFace.Model = model
			}
		}
		notes = append(notes, "server settings: Dictation uses cloud provider")
	case "local_provider":
		cfg.Local.Enabled = true
		cfg.Routing.Strategy = "local-only"
		cfg.VPS.Enabled = false
		cfg.Providers.Ollama.Enabled = true
		if model != "" {
			cfg.Providers.Ollama.STTModel = model
		}
		notes = append(notes, "server settings: Dictation uses local provider")
	default:
		cfg.Local.Enabled = false
		cfg.Routing.Strategy = "cloud-only"
		cfg.VPS.Enabled = true
		if model != "" {
			cfg.VPS.Model = model
		}
		notes = append(notes, "server settings: Dictation uses built-in provider")
	}
	return notes
}

func applyAssistModeSetting(cfg *Config, mode ServerModeSetting) []string {
	var notes []string
	kind := normalizedProviderKind(mode)
	model := cleanSetting(mode.Model)
	if mode.Enabled != nil && !*mode.Enabled {
		cfg.General.AssistEnabled = false
		notes = append(notes, "server settings: Assist disabled")
		return notes
	}
	switch kind {
	case "direct_provider":
		cfg.LocalLLM.Enabled = false
		switch providerForModeSetting(mode) {
		case "openrouter":
			cfg.Providers.OpenRouter.Enabled = true
			if model != "" {
				cfg.Providers.OpenRouter.AssistModel = model
			}
		case "google":
			// Google is a voice provider (STT, TTS, Gemini Live) only; SpeechKit
			// has no Google Assist LLM adapter.
			notes = append(notes, "server settings: Google has no Assist adapter; leaving Assist on the remaining providers")
		case "groq":
			cfg.Providers.Groq.Enabled = true
			if model != "" {
				cfg.Providers.Groq.AssistModel = model
			}
		default:
			cfg.Providers.OpenAI.Enabled = true
			if model != "" {
				cfg.Providers.OpenAI.AssistModel = model
			}
		}
		notes = append(notes, "server settings: Assist uses direct provider")
	case "cloud_provider":
		cfg.LocalLLM.Enabled = false
		if providerForModeSetting(mode) == "openrouter" {
			cfg.HuggingFace.Enabled = false
			cfg.Providers.OpenRouter.Enabled = true
			if model != "" {
				cfg.Providers.OpenRouter.AssistModel = model
			}
		} else {
			cfg.HuggingFace.Enabled = true
			if model != "" {
				cfg.HuggingFace.AssistModel = model
			}
		}
		notes = append(notes, "server settings: Assist uses cloud provider")
	case "local_provider":
		cfg.LocalLLM.Enabled = false
		cfg.Providers.Ollama.Enabled = true
		if model != "" {
			cfg.Providers.Ollama.AssistModel = model
		}
		notes = append(notes, "server settings: Assist uses local provider")
	default:
		cfg.LocalLLM.Enabled = true
		if model != "" {
			model = normalizeServerLLMModel(model)
			cfg.LocalLLM.UtilityModel = model
			cfg.LocalLLM.AssistModel = model
		}
		notes = append(notes, "server settings: Assist uses built-in provider")
	}
	return notes
}

func applyVoiceAgentModeSetting(cfg *Config, mode ServerModeSetting) []string {
	var notes []string
	kind := normalizedProviderKind(mode)
	model := cleanSetting(mode.Model)
	if mode.Enabled != nil && !*mode.Enabled {
		cfg.General.VoiceAgentEnabled = false
		notes = append(notes, "server settings: Voice Agent disabled")
		return notes
	}
	switch kind {
	case "direct_provider":
		provider := directVoiceAgentProviderForProfile(mode.ProfileID)
		switch provider {
		case "openai":
			cfg.VoiceAgent.Provider = "openai"
			cfg.Providers.OpenAI.Enabled = true
			if model != "" {
				cfg.Providers.OpenAI.RealtimeModel = model
			}
		case "deepgram":
			cfg.VoiceAgent.Provider = "deepgram"
			cfg.Providers.Deepgram.Enabled = true
			if model != "" {
				cfg.VoiceAgent.Model = model
			}
		case "assemblyai":
			cfg.VoiceAgent.Provider = "assemblyai"
			cfg.Providers.AssemblyAI.Enabled = true
			if model != "" {
				cfg.VoiceAgent.Model = model
			}
		case "gemini":
			cfg.VoiceAgent.Provider = "gemini"
			cfg.Providers.Google.Enabled = true
			if model != "" {
				cfg.VoiceAgent.Model = model
			}
		default:
			cfg.VoiceAgent.Provider = "openai"
			cfg.Providers.OpenAI.Enabled = true
			if model != "" {
				cfg.Providers.OpenAI.RealtimeModel = model
			}
		}
		notes = append(notes, "server settings: Voice Agent uses "+cfg.VoiceAgent.Provider+" direct provider")
	case "cloud_provider":
		cfg.VoiceAgent.Provider = "cascaded"
		if providerForModeSetting(mode) == "openrouter" {
			cfg.HuggingFace.Enabled = false
			cfg.Providers.OpenRouter.Enabled = true
			if model != "" {
				cfg.Providers.OpenRouter.AgentModel = model
			}
		} else {
			cfg.HuggingFace.Enabled = true
			if model != "" {
				cfg.HuggingFace.AgentModel = model
			}
		}
		notes = append(notes, "server settings: Voice Agent uses cloud fallback provider")
	case "local_provider":
		cfg.VoiceAgent.Provider = "cascaded"
		cfg.Providers.Ollama.Enabled = true
		if model != "" {
			cfg.Providers.Ollama.AgentModel = model
		}
		notes = append(notes, "server settings: Voice Agent uses local provider")
	default:
		cfg.VoiceAgent.Provider = "cascaded"
		cfg.LocalLLM.Enabled = true
		if model != "" {
			cfg.LocalLLM.AgentModel = normalizeServerLLMModel(model)
		}
		notes = append(notes, "server settings: Voice Agent uses built-in provider")
	}
	return notes
}

func directVoiceAgentProviderForProfile(profileID string) string {
	switch providerForProfileID(profileID) {
	case "openai", "deepgram", "assemblyai":
		return providerForProfileID(profileID)
	case "google":
		return "gemini"
	default:
		return "openai"
	}
}

func providerForModeSetting(mode ServerModeSetting) string {
	provider := providerForProfileID(mode.ProfileID)
	if provider != "" {
		return provider
	}
	switch normalizedProviderKind(mode) {
	case "local_provider":
		return "ollama"
	case "cloud_provider":
		return "huggingface"
	case "direct_provider":
		return "openai"
	default:
		return "local"
	}
}

func providerForProfileID(profileID string) string {
	provider := catalog.NormalizeProviderID(profileID)
	if strings.Contains(provider, ".") {
		return ""
	}
	return provider
}

func applyDirectDictationProvider(cfg *Config, provider, model string) {
	cfg.HuggingFace.Enabled = false
	switch provider {
	case "groq":
		cfg.Providers.Groq.Enabled = true
		if model != "" {
			cfg.Providers.Groq.STTModel = model
		}
	case "google":
		cfg.Providers.Google.Enabled = true
		if model != "" {
			cfg.Providers.Google.STTModel = model
		}
	case "deepgram":
		cfg.Providers.Deepgram.Enabled = true
		if model != "" {
			cfg.Providers.Deepgram.STTModel = model
		}
	case "assemblyai":
		cfg.Providers.AssemblyAI.Enabled = true
		if model != "" {
			cfg.Providers.AssemblyAI.STTModels = model
		}
	case "openrouter":
		cfg.Providers.OpenRouter.Enabled = true
		if model != "" {
			cfg.Providers.OpenRouter.STTModel = model
		}
	default:
		cfg.Providers.OpenAI.Enabled = true
		if model != "" {
			cfg.Providers.OpenAI.STTModel = model
		}
	}
}
