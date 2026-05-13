package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/cmd/speechkit/internal/profiles"
	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

// validateModeSelection enforces that the selected profile IDs refer
// to known catalog entries with the correct modality and (for routed
// providers) that the install has the required credentials. Lives in
// package main because it consumes profileCredentialAvailable and
// errHFUnavailableBuild from elsewhere in main; will join the
// profiles package once those helpers also move.
func validateModeSelection(cfg *config.Config, catalog models.Catalog, mode string, selection config.ModeModelSelection) error {
	selection = profiles.NormalizeModeSelection(selection)
	expectedModality := profiles.ModalityForMode(mode)
	seen := map[string]bool{}

	for _, profileID := range []string{selection.PrimaryProfileID, selection.FallbackProfileID} {
		if profileID == "" {
			continue
		}
		if seen[profileID] {
			return fmt.Errorf("duplicate model profile %q for %s", profileID, mode)
		}
		seen[profileID] = true

		profile, ok := profiles.FindCatalogProfile(catalog, profileID)
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

// selectedModelSpecsForMode resolves the ordered (provider, model)
// pairs that the Genkit AI runtime should bind for the given mode.
// Stays in package main because localBuiltInLLMProfileReady lives in
// app_init.go (alongside the rest of the local-LLM bootstrap surface).
func selectedModelSpecsForMode(cfg *config.Config, catalog models.Catalog, mode string) ([]appai.OrderedModelSelection, bool) {
	selection := profiles.ModeSelectionForMode(cfg, mode)
	if selection.PrimaryProfileID == "" && selection.FallbackProfileID == "" {
		return nil, false
	}

	ordered := make([]appai.OrderedModelSelection, 0, 2)
	seen := map[string]bool{}
	for _, profileID := range []string{selection.PrimaryProfileID, selection.FallbackProfileID} {
		profile, ok := profiles.FindCatalogProfile(catalog, profileID)
		if !ok {
			continue
		}
		if !localBuiltInLLMProfileReady(cfg, profile) {
			continue
		}
		spec, ok := profiles.OrderedSelectionFromProfile(cfg, profile)
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

// applySelectedVoiceAgentProfile mutates the live Config to reflect
// the currently-selected Voice Agent profile. Stays in main because
// selectedLocalBuiltInLLMModelID lives in model_profiles.go.
func applySelectedVoiceAgentProfile(cfg *config.Config, catalog models.Catalog) {
	if cfg == nil {
		return
	}

	selection := profiles.ModeSelectionForMode(cfg, modeVoiceAgent)
	if selection.PrimaryProfileID == "" && selection.FallbackProfileID == "" {
		return
	}

	primary, primaryOK := profiles.FindCatalogProfile(catalog, selection.PrimaryProfileID)
	fallback, fallbackOK := profiles.FindCatalogProfile(catalog, selection.FallbackProfileID)
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
	case models.ExecutionModeOpenRouter:
		cfg.Providers.OpenRouter.Enabled = true
		cfg.Providers.OpenRouter.AgentModel = primary.ModelID
		cfg.VoiceAgent.PipelineFallback = true
	case models.ExecutionModeOllama:
		cfg.Providers.Ollama.Enabled = true
		if cfg.Providers.Ollama.BaseURL == "" {
			cfg.Providers.Ollama.BaseURL = "http://localhost:11434"
		}
		cfg.Providers.Ollama.AgentModel = primary.ModelID
		cfg.VoiceAgent.PipelineFallback = true
	case models.ExecutionModeLocal:
		modelID := selectedLocalBuiltInLLMModelID(cfg, primary.ModelID)
		cfg.LocalLLM.Enabled = true
		if cfg.LocalLLM.BaseURL == "" {
			cfg.LocalLLM.BaseURL = config.DefaultLocalLLMBaseURL
		}
		if cfg.LocalLLM.Port == 0 {
			cfg.LocalLLM.Port = 8082
		}
		if cfg.LocalLLM.Model == "" || cfg.LocalLLM.Model == primary.ModelID {
			cfg.LocalLLM.Model = modelID
		}
		cfg.LocalLLM.AgentModel = modelID
		cfg.VoiceAgent.PipelineFallback = true
	default:
		cfg.VoiceAgent.PipelineFallback = fallbackOK
	}
}

// syncConfiguredSTTRouter wires the live router instance to match the
// current config. Stays in package main because it pulls in the
// concrete provider constructors (configuredVPSProvider etc.) that
// live in provider_integrations.go.
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
	if provider := configuredOpenRouterProvider(cfg); provider != nil {
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
