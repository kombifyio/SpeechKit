// Package profiles provides pure model-profile selection helpers — the
// part of the legacy cmd/speechkit model_selection_helpers.go that
// depends only on config and the model catalog (no *appState, no
// network, no Wails surface).
//
// Functions that orchestrate the STT router or validate against
// provider credentials stay in package main for now because they pull
// in subsystems earmarked for later C2 PRs.
package profiles

import (
	"strings"

	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/config"
	desktopruntime "github.com/kombifyio/SpeechKit/internal/desktop/runtime"
	"github.com/kombifyio/SpeechKit/internal/models"
)

// ModeModelSelectionSnapshot is the JSON-friendly view of a mode's
// active profile selection (primary + optional fallback).
type ModeModelSelectionSnapshot struct {
	PrimaryProfileID  string `json:"primaryProfileId"`
	FallbackProfileID string `json:"fallbackProfileId"`
}

// ModalityForMode maps a runtime mode name ("dictate", "assist",
// "voice_agent") to the corresponding model modality.
func ModalityForMode(mode string) models.Modality {
	switch strings.TrimSpace(mode) {
	case desktopruntime.ModeDictate:
		return models.ModalitySTT
	case desktopruntime.ModeVoiceAgent:
		return models.ModalityRealtimeVoice
	default:
		return models.ModalityAssist
	}
}

// NormalizeModeSelection trims whitespace from both profile IDs and
// drops the fallback if it is identical to the primary.
func NormalizeModeSelection(selection config.ModeModelSelection) config.ModeModelSelection {
	selection.PrimaryProfileID = strings.TrimSpace(selection.PrimaryProfileID)
	selection.FallbackProfileID = strings.TrimSpace(selection.FallbackProfileID)
	if selection.PrimaryProfileID != "" && selection.PrimaryProfileID == selection.FallbackProfileID {
		selection.FallbackProfileID = ""
	}
	return selection
}

// ModeSelectionForMode returns the normalized profile selection
// stored in config for the given runtime mode.
func ModeSelectionForMode(cfg *config.Config, mode string) config.ModeModelSelection {
	if cfg == nil {
		return config.ModeModelSelection{}
	}
	switch strings.TrimSpace(mode) {
	case desktopruntime.ModeDictate:
		return NormalizeModeSelection(cfg.ModelSelection.Dictate)
	case desktopruntime.ModeVoiceAgent:
		return NormalizeModeSelection(cfg.ModelSelection.VoiceAgent)
	default:
		return NormalizeModeSelection(cfg.ModelSelection.Assist)
	}
}

// SetModeSelectionForProfile writes the given profile as the primary
// selection for whichever runtime mode matches the profile's modality.
func SetModeSelectionForProfile(cfg *config.Config, profile models.Profile) {
	if cfg == nil {
		return
	}
	selection := NormalizeModeSelection(config.ModeModelSelection{
		PrimaryProfileID: profile.ID,
	})
	switch profile.Modality {
	case models.ModalitySTT:
		cfg.ModelSelection.Dictate = selection
	case models.ModalityAssist:
		cfg.ModelSelection.Assist = selection
	case models.ModalityRealtimeVoice:
		cfg.ModelSelection.VoiceAgent = selection
	default:
	}
}

// ConfiguredModeModelSelections returns the snapshot for every
// runtime mode in a stable map suitable for serialization.
func ConfiguredModeModelSelections(cfg *config.Config, _ models.Catalog) map[string]ModeModelSelectionSnapshot {
	return map[string]ModeModelSelectionSnapshot{
		desktopruntime.ModeDictate: {
			PrimaryProfileID:  ModeSelectionForMode(cfg, desktopruntime.ModeDictate).PrimaryProfileID,
			FallbackProfileID: ModeSelectionForMode(cfg, desktopruntime.ModeDictate).FallbackProfileID,
		},
		desktopruntime.ModeAssist: {
			PrimaryProfileID:  ModeSelectionForMode(cfg, desktopruntime.ModeAssist).PrimaryProfileID,
			FallbackProfileID: ModeSelectionForMode(cfg, desktopruntime.ModeAssist).FallbackProfileID,
		},
		desktopruntime.ModeVoiceAgent: {
			PrimaryProfileID:  ModeSelectionForMode(cfg, desktopruntime.ModeVoiceAgent).PrimaryProfileID,
			FallbackProfileID: ModeSelectionForMode(cfg, desktopruntime.ModeVoiceAgent).FallbackProfileID,
		},
	}
}

// FindCatalogProfile looks up a profile by ID in the catalog. The
// boolean is false for empty IDs and unknown profiles.
func FindCatalogProfile(catalog models.Catalog, profileID string) (models.Profile, bool) {
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

// EffectiveSelectedProfile returns the profile that should actually be
// used for the given mode, preferring the primary selection and
// falling back to the fallback if the primary is missing from the
// catalog.
func EffectiveSelectedProfile(cfg *config.Config, catalog models.Catalog, mode string) (models.Profile, bool) {
	selection := ModeSelectionForMode(cfg, mode)
	for _, profileID := range []string{selection.PrimaryProfileID, selection.FallbackProfileID} {
		if profile, ok := FindCatalogProfile(catalog, profileID); ok {
			return profile, true
		}
	}
	return models.Profile{}, false
}

// OrderedSelectionFromProfile maps a profile to the Genkit-flavoured
// (provider, model) pair the AI runtime expects. Returns false for
// unsupported execution modes.
func OrderedSelectionFromProfile(cfg *config.Config, profile models.Profile) (appai.OrderedModelSelection, bool) {
	modelID := SelectedModelIDForProfile(cfg, profile)
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

// SelectedModelIDForProfile resolves the actual model ID to run for a
// profile, accounting for user-configured local-LLM model overrides
// when the profile is a local built-in.
func SelectedModelIDForProfile(cfg *config.Config, profile models.Profile) string {
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
	default:
	}

	if configured := strings.TrimSpace(cfg.LocalLLM.Model); configured != "" {
		return configured
	}
	return modelID
}
