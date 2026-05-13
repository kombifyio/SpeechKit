package main

import (
	"github.com/kombifyio/SpeechKit/cmd/speechkit/internal/profiles"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
)

func TestFilteredModelCatalogExposesFourProviderKindsPerMode(t *testing.T) {
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	catalog := filteredModelCatalog()
	required := map[models.Modality][]models.ProviderKind{
		models.ModalitySTT: {
			models.ProviderKindLocalBuiltIn,
			models.ProviderKindLocalProvider,
			models.ProviderKindCloudProvider,
			models.ProviderKindDirectProvider,
		},
		models.ModalityAssist: {
			models.ProviderKindLocalBuiltIn,
			models.ProviderKindLocalProvider,
			models.ProviderKindCloudProvider,
			models.ProviderKindDirectProvider,
		},
		models.ModalityRealtimeVoice: {
			models.ProviderKindLocalBuiltIn,
			models.ProviderKindLocalProvider,
			models.ProviderKindCloudProvider,
			models.ProviderKindDirectProvider,
		},
	}

	for modality, providerKinds := range required {
		seen := map[models.ProviderKind]bool{}
		for _, profile := range catalog.Profiles {
			if profile.Modality != modality {
				continue
			}
			seen[profile.ProviderKind] = true
		}
		for _, providerKind := range providerKinds {
			if !seen[providerKind] {
				t.Fatalf("%s missing provider kind %q in filtered catalog", modality, providerKind)
			}
		}
	}
}

func TestFilteredModelCatalogKeepsCloudProviderGroupWhenHuggingFaceBuildUnavailable(t *testing.T) {
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("0")
	defer restoreBuild()

	catalog := filteredModelCatalog()
	for _, modality := range []models.Modality{
		models.ModalitySTT,
		models.ModalityAssist,
		models.ModalityRealtimeVoice,
	} {
		found := false
		for _, profile := range catalog.Profiles {
			if profile.Modality == modality && profile.ProviderKind == models.ProviderKindCloudProvider {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s missing cloud provider group when hugging face build is unavailable", modality)
		}
	}
}

func TestFilteredModelCatalogKeepsProviderVariants(t *testing.T) {
	catalog := filteredModelCatalog()

	for _, profile := range catalog.Profiles {
		if profile.ID != "stt.local.whispercpp" {
			continue
		}
		if profile.ProviderKind != models.ProviderKindLocalBuiltIn {
			t.Fatalf("local dictation provider kind = %q, want %q", profile.ProviderKind, models.ProviderKindLocalBuiltIn)
		}
		if len(profile.Variants) < 3 {
			t.Fatalf("local dictation variants = %d, want at least 3", len(profile.Variants))
		}
		return
	}

	t.Fatal("missing local dictation profile")
}

func TestFilteredModelCatalogExposesOptionalIntegrationProfiles(t *testing.T) {
	catalog := filteredModelCatalog()

	required := map[string]struct {
		modality      models.Modality
		providerKind  models.ProviderKind
		executionMode models.ExecutionMode
	}{
		"stt.google.chirp-3": {
			modality:      models.ModalitySTT,
			providerKind:  models.ProviderKindDirectProvider,
			executionMode: models.ExecutionModeGoogle,
		},
		"assist.google.gemini-2.5-flash": {
			modality:      models.ModalityAssist,
			providerKind:  models.ProviderKindDirectProvider,
			executionMode: models.ExecutionModeGoogle,
		},
		"assist.openrouter.gemini-2.5-flash": {
			modality:      models.ModalityAssist,
			providerKind:  models.ProviderKindCloudProvider,
			executionMode: models.ExecutionModeOpenRouter,
		},
		"stt.openrouter.whisper-1": {
			modality:      models.ModalitySTT,
			providerKind:  models.ProviderKindCloudProvider,
			executionMode: models.ExecutionModeOpenRouter,
		},
		"realtime.openrouter.gemini-2.5-flash-pipeline": {
			modality:      models.ModalityRealtimeVoice,
			providerKind:  models.ProviderKindCloudProvider,
			executionMode: models.ExecutionModeOpenRouter,
		},
		"assist.groq.llama-3.3-70b": {
			modality:      models.ModalityAssist,
			providerKind:  models.ProviderKindDirectProvider,
			executionMode: models.ExecutionModeGroq,
		},
		"utility.openrouter.llama-3.1-8b": {
			modality:      models.ModalityUtility,
			providerKind:  models.ProviderKindCloudProvider,
			executionMode: models.ExecutionModeOpenRouter,
		},
	}

	for profileID, expected := range required {
		profile, ok := profiles.FindCatalogProfile(catalog, profileID)
		if !ok {
			t.Fatalf("missing integration profile %q", profileID)
		}
		if profile.Modality != expected.modality {
			t.Fatalf("%s modality = %q, want %q", profileID, profile.Modality, expected.modality)
		}
		if profile.ProviderKind != expected.providerKind {
			t.Fatalf("%s provider kind = %q, want %q", profileID, profile.ProviderKind, expected.providerKind)
		}
		if profile.ExecutionMode != expected.executionMode {
			t.Fatalf("%s execution mode = %q, want %q", profileID, profile.ExecutionMode, expected.executionMode)
		}
	}
}

func TestBuiltInTranscribeAndAssistProfilesUseRuntimeSelectionLabels(t *testing.T) {
	catalog := filteredModelCatalog()

	required := map[string]struct {
		modality models.Modality
		name     string
		runtime  string
	}{
		"stt.local.whispercpp": {
			modality: models.ModalitySTT,
			name:     "Whisper.cpp (Local Built-in)",
			runtime:  "Whisper.cpp",
		},
		"assist.builtin.gemma4-e4b": {
			modality: models.ModalityAssist,
			name:     "Gemma 4 E4B (Local Built-in)",
			runtime:  "llama.cpp",
		},
	}
	for profileID, expected := range required {
		profile, ok := profiles.FindCatalogProfile(catalog, profileID)
		if !ok {
			t.Fatalf("missing built-in profile %q", profileID)
		}
		if profile.Modality != expected.modality {
			t.Fatalf("%s modality = %q, want %q", profileID, profile.Modality, expected.modality)
		}
		if profile.ProviderKind != models.ProviderKindLocalBuiltIn {
			t.Fatalf("%s provider kind = %q, want %q", profileID, profile.ProviderKind, models.ProviderKindLocalBuiltIn)
		}
		if profile.ExecutionMode != models.ExecutionModeLocal {
			t.Fatalf("%s execution mode = %q, want %q", profileID, profile.ExecutionMode, models.ExecutionModeLocal)
		}
		if profile.Name != expected.name {
			t.Fatalf("%s name = %q, want %q", profileID, profile.Name, expected.name)
		}
		if len(profile.Variants) == 0 {
			t.Fatalf("%s should expose concrete model variants behind the %s runtime", profileID, expected.runtime)
		}
	}
}
