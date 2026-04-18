package main

import (
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
)

func defaultActiveProfiles(catalog models.Catalog) map[string]string {
	profiles := make(map[string]string)
	for _, modality := range []models.Modality{
		models.ModalitySTT,
		models.ModalityAssist,
		models.ModalityUtility,
		models.ModalityRealtimeVoice,
	} {
		if profile, ok := catalog.DefaultProfile(modality); ok {
			profiles[string(modality)] = profile.ID
		}
	}
	return profiles
}

func activeProfilesFromConfig(cfg *config.Config, catalog models.Catalog) map[string]string {
	profiles := make(map[string]string)
	if cfg == nil {
		return defaultActiveProfiles(catalog)
	}

	for _, mode := range []string{modeDictate, modeAssist, modeVoiceAgent} {
		if profile, ok := effectiveSelectedProfile(cfg, catalog, mode); ok {
			profiles[string(modalityForMode(mode))] = profile.ID
			continue
		}
		if profile, ok := activeProfileForModality(cfg, catalog, modalityForMode(mode)); ok {
			profiles[string(modalityForMode(mode))] = profile.ID
		}
	}

	for _, modality := range []models.Modality{
		models.ModalityUtility,
	} {
		if profile, ok := activeProfileForModality(cfg, catalog, modality); ok {
			profiles[string(modality)] = profile.ID
		}
	}

	return profiles
}

func activeProfileForModality(cfg *config.Config, catalog models.Catalog, modality models.Modality) (models.Profile, bool) {
	if modality == models.ModalitySTT && cfg.Local.Enabled && cfg.Routing.Strategy != "cloud-only" {
		for _, profile := range catalog.Profiles {
			if profile.Modality == models.ModalitySTT && profile.ExecutionMode == models.ExecutionModeLocal {
				return profile, true
			}
		}
	}

	for _, profile := range catalog.Profiles {
		if profile.Modality != modality {
			continue
		}
		if profileMatchesConfig(cfg, profile) {
			return profile, true
		}
	}

	return models.Profile{}, false
}

func profileMatchesConfig(cfg *config.Config, profile models.Profile) bool {
	switch profile.Modality {
	case models.ModalitySTT:
		return sttProfileMatchesConfig(cfg, profile)
	case models.ModalityUtility:
		return utilityProfileMatchesConfig(cfg, profile)
	case models.ModalityAssist:
		return assistProfileMatchesConfig(cfg, profile)
	case models.ModalityRealtimeVoice:
		return realtimeVoiceProfileMatchesConfig(cfg, profile)
	default:
		return false
	}
}

func sttProfileMatchesConfig(cfg *config.Config, profile models.Profile) bool {
	switch profile.ExecutionMode {
	case models.ExecutionModeLocal:
		return cfg.Local.Enabled && cfg.Routing.Strategy != "cloud-only"
	case models.ExecutionModeHFRouted:
		return cfg.HuggingFace.Enabled && cfg.HuggingFace.Model == profile.ModelID && profileCredentialAvailable(cfg, profile)
	case models.ExecutionModeOpenAI:
		return cfg.Providers.OpenAI.Enabled && cfg.Providers.OpenAI.STTModel == profile.ModelID && profileCredentialAvailable(cfg, profile)
	case models.ExecutionModeGroq:
		return cfg.Providers.Groq.Enabled && cfg.Providers.Groq.STTModel == profile.ModelID && profileCredentialAvailable(cfg, profile)
	case models.ExecutionModeGoogle:
		return cfg.Providers.Google.Enabled && cfg.Providers.Google.STTModel == profile.ModelID && profileCredentialAvailable(cfg, profile)
	default:
		return false
	}
}

func utilityProfileMatchesConfig(cfg *config.Config, profile models.Profile) bool {
	switch profile.ExecutionMode {
	case models.ExecutionModeOpenAI:
		return cfg.Providers.OpenAI.Enabled && cfg.Providers.OpenAI.UtilityModel == profile.ModelID && profileCredentialAvailable(cfg, profile)
	case models.ExecutionModeGroq:
		return cfg.Providers.Groq.Enabled && cfg.Providers.Groq.UtilityModel == profile.ModelID && profileCredentialAvailable(cfg, profile)
	case models.ExecutionModeGoogle:
		return cfg.Providers.Google.Enabled && cfg.Providers.Google.UtilityModel == profile.ModelID && profileCredentialAvailable(cfg, profile)
	case models.ExecutionModeHFRouted:
		return cfg.HuggingFace.Enabled && cfg.HuggingFace.UtilityModel == profile.ModelID && profileCredentialAvailable(cfg, profile)
	case models.ExecutionModeOllama:
		return cfg.Providers.Ollama.Enabled && cfg.Providers.Ollama.UtilityModel == profile.ModelID
	case models.ExecutionModeOpenRouter:
		return cfg.Providers.OpenRouter.Enabled && cfg.Providers.OpenRouter.UtilityModel == profile.ModelID && profileCredentialAvailable(cfg, profile)
	default:
		return false
	}
}

func assistProfileMatchesConfig(cfg *config.Config, profile models.Profile) bool {
	switch profile.ExecutionMode {
	case models.ExecutionModeOpenAI:
		return cfg.Providers.OpenAI.Enabled && cfg.Providers.OpenAI.AssistModel == profile.ModelID && profileCredentialAvailable(cfg, profile)
	case models.ExecutionModeGroq:
		return cfg.Providers.Groq.Enabled && cfg.Providers.Groq.AssistModel == profile.ModelID && profileCredentialAvailable(cfg, profile)
	case models.ExecutionModeGoogle:
		return cfg.Providers.Google.Enabled && cfg.Providers.Google.AssistModel == profile.ModelID && profileCredentialAvailable(cfg, profile)
	case models.ExecutionModeHFRouted:
		return cfg.HuggingFace.Enabled && cfg.HuggingFace.AssistModel == profile.ModelID && profileCredentialAvailable(cfg, profile)
	case models.ExecutionModeOllama:
		return cfg.Providers.Ollama.Enabled && cfg.Providers.Ollama.AssistModel == profile.ModelID
	case models.ExecutionModeOpenRouter:
		return cfg.Providers.OpenRouter.Enabled && cfg.Providers.OpenRouter.AssistModel == profile.ModelID && profileCredentialAvailable(cfg, profile)
	default:
		return false
	}
}

func realtimeVoiceProfileMatchesConfig(cfg *config.Config, profile models.Profile) bool {
	if !cfg.VoiceAgent.Enabled || cfg.VoiceAgent.Model != profile.ModelID {
		return false
	}
	return profileCredentialAvailable(cfg, profile)
}
