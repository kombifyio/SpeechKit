package main

import (
	"context"
	"fmt"
	"strings"

	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

type modeModelSelectionSnapshot struct {
	PrimaryProfileID  string `json:"primaryProfileId"`
	FallbackProfileID string `json:"fallbackProfileId"`
}

func modalityForMode(mode string) models.Modality {
	switch strings.TrimSpace(mode) {
	case modeDictate:
		return models.ModalitySTT
	case modeVoiceAgent:
		return models.ModalityRealtimeVoice
	default:
		return models.ModalityAssist
	}
}

func normalizeModeSelection(selection config.ModeModelSelection) config.ModeModelSelection {
	selection.PrimaryProfileID = strings.TrimSpace(selection.PrimaryProfileID)
	selection.FallbackProfileID = strings.TrimSpace(selection.FallbackProfileID)
	if selection.PrimaryProfileID != "" && selection.PrimaryProfileID == selection.FallbackProfileID {
		selection.FallbackProfileID = ""
	}
	return selection
}

func modeSelectionForMode(cfg *config.Config, mode string) config.ModeModelSelection {
	if cfg == nil {
		return config.ModeModelSelection{}
	}
	switch strings.TrimSpace(mode) {
	case modeDictate:
		return normalizeModeSelection(cfg.ModelSelection.Dictate)
	case modeVoiceAgent:
		return normalizeModeSelection(cfg.ModelSelection.VoiceAgent)
	default:
		return normalizeModeSelection(cfg.ModelSelection.Assist)
	}
}

func configuredModeModelSelections(cfg *config.Config, _ models.Catalog) map[string]modeModelSelectionSnapshot {
	return map[string]modeModelSelectionSnapshot{
		modeDictate: {
			PrimaryProfileID:  modeSelectionForMode(cfg, modeDictate).PrimaryProfileID,
			FallbackProfileID: modeSelectionForMode(cfg, modeDictate).FallbackProfileID,
		},
		modeAssist: {
			PrimaryProfileID:  modeSelectionForMode(cfg, modeAssist).PrimaryProfileID,
			FallbackProfileID: modeSelectionForMode(cfg, modeAssist).FallbackProfileID,
		},
		modeVoiceAgent: {
			PrimaryProfileID:  modeSelectionForMode(cfg, modeVoiceAgent).PrimaryProfileID,
			FallbackProfileID: modeSelectionForMode(cfg, modeVoiceAgent).FallbackProfileID,
		},
	}
}

func findCatalogProfile(catalog models.Catalog, profileID string) (models.Profile, bool) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return models.Profile{}, false
	}
	for _, profile := range catalog.Profiles {
		if profile.ID == profileID {
			return profile, true
		}
	}
	return models.Profile{}, false
}

func validateModeSelection(cfg *config.Config, catalog models.Catalog, mode string, selection config.ModeModelSelection) error {
	selection = normalizeModeSelection(selection)
	expectedModality := modalityForMode(mode)
	seen := map[string]bool{}

	for _, profileID := range []string{selection.PrimaryProfileID, selection.FallbackProfileID} {
		if profileID == "" {
			continue
		}
		if seen[profileID] {
			return fmt.Errorf("duplicate model profile %q for %s", profileID, mode)
		}
		seen[profileID] = true

		profile, ok := findCatalogProfile(catalog, profileID)
		if !ok {
			return fmt.Errorf("unknown %s profile %q", mode, profileID)
		}
		if profile.Modality != expectedModality {
			return fmt.Errorf("%s profile %q must use modality %s", mode, profileID, expectedModality)
		}
		if profile.ExecutionMode == models.ExecutionModeHFRouted && !config.ManagedHuggingFaceAvailableInBuild() {
			return errHFUnavailableBuild
		}
		if profile.ExecutionMode == models.ExecutionModeHFRouted && !profileCredentialAvailable(cfg, profile) {
			// Allow saving routed selections without a configured token/API key.
			continue
		}
	}

	return nil
}

func effectiveSelectedProfile(cfg *config.Config, catalog models.Catalog, mode string) (models.Profile, bool) {
	selection := modeSelectionForMode(cfg, mode)
	for _, profileID := range []string{selection.PrimaryProfileID, selection.FallbackProfileID} {
		if profile, ok := findCatalogProfile(catalog, profileID); ok {
			return profile, true
		}
	}
	return models.Profile{}, false
}

func orderedSelectionFromProfile(cfg *config.Config, profile models.Profile) (appai.OrderedModelSelection, bool) {
	modelID := selectedModelIDForProfile(cfg, profile)
	if strings.TrimSpace(modelID) == "" {
		return appai.OrderedModelSelection{}, false
	}

	var provider string
	switch profile.ExecutionMode {
	case models.ExecutionModeLocal:
		provider = "local"
	case models.ExecutionModeGoogle:
		provider = "googleai"
	case models.ExecutionModeOpenAI:
		provider = "openai"
	case models.ExecutionModeGroq:
		provider = "groq"
	case models.ExecutionModeHFRouted:
		provider = "huggingface"
	case models.ExecutionModeOllama:
		provider = "ollama"
	case models.ExecutionModeOpenRouter:
		provider = "openrouter"
	default:
		return appai.OrderedModelSelection{}, false
	}

	return appai.OrderedModelSelection{
		Provider: provider,
		Model:    modelID,
	}, true
}

func selectedModelIDForProfile(cfg *config.Config, profile models.Profile) string {
	modelID := profile.ModelID
	if cfg == nil || profile.ExecutionMode != models.ExecutionModeLocal || profile.ProviderKind != models.ProviderKindLocalBuiltIn {
		return modelID
	}

	switch profile.Modality {
	case models.ModalityUtility:
		if configured := strings.TrimSpace(cfg.LocalLLM.UtilityModel); configured != "" {
			return configured
		}
	case models.ModalityAssist:
		if configured := strings.TrimSpace(cfg.LocalLLM.AssistModel); configured != "" {
			return configured
		}
	case models.ModalityRealtimeVoice:
		if configured := strings.TrimSpace(cfg.LocalLLM.AgentModel); configured != "" {
			return configured
		}
	}

	if configured := strings.TrimSpace(cfg.LocalLLM.Model); configured != "" {
		return configured
	}
	return modelID
}

func selectedModelSpecsForMode(cfg *config.Config, catalog models.Catalog, mode string) ([]appai.OrderedModelSelection, bool) {
	selection := modeSelectionForMode(cfg, mode)
	if selection.PrimaryProfileID == "" && selection.FallbackProfileID == "" {
		return nil, false
	}

	ordered := make([]appai.OrderedModelSelection, 0, 2)
	seen := map[string]bool{}
	for _, profileID := range []string{selection.PrimaryProfileID, selection.FallbackProfileID} {
		profile, ok := findCatalogProfile(catalog, profileID)
		if !ok {
			continue
		}
		spec, ok := orderedSelectionFromProfile(cfg, profile)
		if !ok {
			continue
		}
		key := spec.Provider + "/" + spec.Model
		if seen[key] {
			continue
		}
		seen[key] = true
		ordered = append(ordered, spec)
	}

	if len(ordered) == 0 {
		return nil, false
	}
	return ordered, true
}

func applySelectedVoiceAgentProfile(cfg *config.Config, catalog models.Catalog) {
	if cfg == nil {
		return
	}

	selection := modeSelectionForMode(cfg, modeVoiceAgent)
	if selection.PrimaryProfileID == "" && selection.FallbackProfileID == "" {
		return
	}

	primary, primaryOK := findCatalogProfile(catalog, selection.PrimaryProfileID)
	fallback, fallbackOK := findCatalogProfile(catalog, selection.FallbackProfileID)
	if !primaryOK {
		primary = fallback
		primaryOK = fallbackOK
		fallback = models.Profile{}
		fallbackOK = false
	}
	if !primaryOK {
		return
	}

	cfg.VoiceAgent.Enabled = true
	cfg.VoiceAgent.Model = primary.ModelID
	if fallbackOK {
		cfg.VoiceAgent.FallbackModel = fallback.ModelID
	} else {
		cfg.VoiceAgent.FallbackModel = ""
	}

	switch primary.ExecutionMode {
	case models.ExecutionModeGoogle:
		cfg.VoiceAgent.PipelineFallback = fallbackOK && fallback.ExecutionMode != models.ExecutionModeGoogle
		if cfg.HuggingFace.AgentModel == primary.ModelID {
			cfg.HuggingFace.AgentModel = ""
		}
	case models.ExecutionModeHFRouted:
		cfg.HuggingFace.Enabled = true
		cfg.HuggingFace.AgentModel = primary.ModelID
		cfg.VoiceAgent.PipelineFallback = true
	case models.ExecutionModeOllama:
		cfg.Providers.Ollama.Enabled = true
		if cfg.Providers.Ollama.BaseURL == "" {
			cfg.Providers.Ollama.BaseURL = "http://localhost:11434"
		}
		cfg.Providers.Ollama.AgentModel = primary.ModelID
		cfg.VoiceAgent.PipelineFallback = true
	case models.ExecutionModeLocal:
		cfg.LocalLLM.Enabled = true
		if cfg.LocalLLM.BaseURL == "" {
			cfg.LocalLLM.BaseURL = config.DefaultLocalLLMBaseURL
		}
		if cfg.LocalLLM.Port == 0 {
			cfg.LocalLLM.Port = 8082
		}
		cfg.LocalLLM.AgentModel = primary.ModelID
		cfg.VoiceAgent.PipelineFallback = true
	default:
		cfg.VoiceAgent.PipelineFallback = fallbackOK
	}
}

func syncConfiguredSTTRouter(ctx context.Context, cfg *config.Config, state *appState, sttRouter *router.Router) {
	targetRouter := sttRouter
	if targetRouter == nil && state != nil {
		targetRouter = state.sttRouter
	}
	if targetRouter == nil || cfg == nil {
		return
	}

	targetRouter.Strategy = router.Strategy(cfg.Routing.Strategy)
	targetRouter.PreferLocalUnderSecs = cfg.Routing.PreferLocalUnderSeconds
	targetRouter.ParallelCloud = cfg.Routing.ParallelCloud
	targetRouter.ReplaceOnBetter = cfg.Routing.ReplaceOnBetter

	syncConfiguredLocalProvider(ctx, cfg, state, targetRouter)

	var cloudProviders []stt.STTProvider
	if config.ManagedHuggingFaceAvailableInBuild() && cfg.HuggingFace.Enabled {
		token, _, err := config.ResolveHuggingFaceToken(cfg)
		if err == nil && strings.TrimSpace(token) != "" {
			cloudProviders = append(cloudProviders, newHuggingFaceProvider(cfg.HuggingFace.Model, token))
		}
	}
	if provider := configuredVPSProvider(cfg); provider != nil {
		cloudProviders = append(cloudProviders, provider)
	}
	if provider := configuredOllamaSTTProvider(cfg); provider != nil {
		cloudProviders = append(cloudProviders, provider)
	}
	if provider := configuredGroqProvider(cfg); provider != nil {
		cloudProviders = append(cloudProviders, provider)
	}
	if provider := configuredOpenAIProvider(cfg); provider != nil {
		cloudProviders = append(cloudProviders, provider)
	}
	if provider := configuredGoogleProvider(cfg); provider != nil {
		cloudProviders = append(cloudProviders, provider)
	}
	targetRouter.SetCloudProviders(cloudProviders)
}
