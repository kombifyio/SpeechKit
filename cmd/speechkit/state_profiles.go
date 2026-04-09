package main

import (
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
)

func defaultActiveProfiles(catalog models.Catalog) map[string]string {
	profiles := make(map[string]string)
	for _, modality := range []models.Modality{
		models.ModalitySTT,
		models.ModalityAgent,
		models.ModalityUtility,
	} {
		if profile, ok := catalog.DefaultProfile(modality); ok {
			profiles[string(modality)] = profile.ID
		}
	}
	return profiles
}

func activeProfilesFromConfig(cfg *config.Config, catalog models.Catalog) map[string]string {
	profiles := defaultActiveProfiles(catalog)
	if cfg == nil {
		return profiles
	}

	for _, profile := range catalog.Profiles {
		if !profileMatchesConfig(cfg, profile) {
			continue
		}
		profiles[string(profile.Modality)] = profile.ID
	}

	return profiles
}

func profileMatchesConfig(cfg *config.Config, profile models.Profile) bool {
	switch profile.Modality {
	case models.ModalitySTT:
		return sttProfileMatchesConfig(cfg, profile)
	case models.ModalityUtility:
		return utilityProfileMatchesConfig(cfg, profile)
	case models.ModalityAgent:
		return agentProfileMatchesConfig(cfg, profile)
	default:
		return false
	}
}

func sttProfileMatchesConfig(cfg *config.Config, profile models.Profile) bool {
	switch profile.ExecutionMode {
	case models.ExecutionModeLocal:
		return cfg.Local.Enabled && cfg.Routing.Strategy != "cloud-only"
	case models.ExecutionModeHFRouted:
		return cfg.HuggingFace.Enabled && cfg.HuggingFace.Model == profile.ModelID
	case models.ExecutionModeOpenAI:
		return cfg.Providers.OpenAI.Enabled && cfg.Providers.OpenAI.STTModel == profile.ModelID
	case models.ExecutionModeGroq:
		return cfg.Providers.Groq.Enabled && cfg.Providers.Groq.STTModel == profile.ModelID
	case models.ExecutionModeGoogle:
		return cfg.Providers.Google.Enabled && cfg.Providers.Google.STTModel == profile.ModelID
	default:
		return false
	}
}

func utilityProfileMatchesConfig(cfg *config.Config, profile models.Profile) bool {
	switch profile.ExecutionMode {
	case models.ExecutionModeOpenAI:
		return cfg.Providers.OpenAI.Enabled && cfg.Providers.OpenAI.UtilityModel == profile.ModelID
	case models.ExecutionModeGroq:
		return cfg.Providers.Groq.Enabled && cfg.Providers.Groq.UtilityModel == profile.ModelID
	case models.ExecutionModeGoogle:
		return cfg.Providers.Google.Enabled && cfg.Providers.Google.UtilityModel == profile.ModelID
	case models.ExecutionModeHFRouted:
		return cfg.HuggingFace.Enabled && cfg.HuggingFace.UtilityModel == profile.ModelID
	case models.ExecutionModeOllama:
		return cfg.Providers.Ollama.Enabled && cfg.Providers.Ollama.UtilityModel == profile.ModelID
	default:
		return false
	}
}

func agentProfileMatchesConfig(cfg *config.Config, profile models.Profile) bool {
	switch profile.ExecutionMode {
	case models.ExecutionModeOpenAI:
		return cfg.Providers.OpenAI.Enabled && cfg.Providers.OpenAI.AgentModel == profile.ModelID
	case models.ExecutionModeGroq:
		return cfg.Providers.Groq.Enabled && cfg.Providers.Groq.AgentModel == profile.ModelID
	case models.ExecutionModeGoogle:
		return cfg.Providers.Google.Enabled && cfg.Providers.Google.AgentModel == profile.ModelID
	case models.ExecutionModeHFRouted:
		return cfg.HuggingFace.Enabled && cfg.HuggingFace.AgentModel == profile.ModelID
	case models.ExecutionModeOllama:
		return cfg.Providers.Ollama.Enabled && cfg.Providers.Ollama.AgentModel == profile.ModelID
	default:
		return false
	}
}
