package models

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestDefaultCatalogExposesFourProviderKindsPerUserMode(t *testing.T) {
	catalog := DefaultCatalog()
	requiredKinds := []ProviderKind{
		ProviderKindLocalBuiltIn,
		ProviderKindLocalProvider,
		ProviderKindCloudProvider,
		ProviderKindDirectProvider,
	}
	modeRequirements := map[Modality][]Capability{
		ModalitySTT:           {CapabilityTranscription},
		ModalityAssist:        {CapabilityLLM},
		ModalityRealtimeVoice: {CapabilitySessionSummary},
	}

	for modality, requiredCapabilities := range modeRequirements {
		byKind := map[ProviderKind][]Profile{}
		for _, profile := range catalog.Profiles {
			if profile.Modality != modality {
				continue
			}
			if profile.ProviderKind == "" {
				t.Fatalf("%s profile %q has no provider kind", modality, profile.ID)
			}
			for _, capability := range requiredCapabilities {
				if !profile.HasCapability(capability) {
					t.Fatalf("%s profile %q lacks required capability %q", modality, profile.ID, capability)
				}
			}
			byKind[profile.ProviderKind] = append(byKind[profile.ProviderKind], profile)
		}

		for _, kind := range requiredKinds {
			if len(byKind[kind]) == 0 {
				t.Fatalf("%s has no profile for provider kind %q", modality, kind)
			}
		}
	}
}

func TestDefaultCatalogKeepsMultipleBuiltInDictationVariants(t *testing.T) {
	catalog := DefaultCatalog()

	var localBuiltIn Profile
	for _, profile := range catalog.Profiles {
		if profile.Modality == ModalitySTT && profile.ProviderKind == ProviderKindLocalBuiltIn {
			localBuiltIn = profile
			break
		}
	}
	if localBuiltIn.ID == "" {
		t.Fatal("missing local built-in dictation profile")
	}
	if localBuiltIn.ID != "stt.local.whispercpp" {
		t.Fatalf("local built-in dictation profile ID = %q, want stt.local.whispercpp", localBuiltIn.ID)
	}
	if localBuiltIn.Name != "Whisper.cpp (Local Built-in)" {
		t.Fatalf("local built-in dictation profile name = %q, want Whisper.cpp (Local Built-in)", localBuiltIn.Name)
	}
	if localBuiltIn.ModelID != "whisper.cpp" {
		t.Fatalf("local built-in dictation model ID = %q, want whisper.cpp", localBuiltIn.ModelID)
	}
	if len(localBuiltIn.Variants) < 3 {
		t.Fatalf("local built-in dictation variants = %d, want at least 3", len(localBuiltIn.Variants))
	}
	for _, variant := range localBuiltIn.Variants {
		if variant.ID == "" || variant.ModelID == "" || variant.Name == "" {
			t.Fatalf("incomplete local built-in dictation variant: %+v", variant)
		}
	}
}

func TestDefaultCatalogKeepsAssistBuiltInOnLlamaCpp(t *testing.T) {
	catalog := DefaultCatalog()

	var localBuiltIn Profile
	for _, profile := range catalog.Profiles {
		if profile.Modality == ModalityAssist && profile.ProviderKind == ProviderKindLocalBuiltIn {
			localBuiltIn = profile
			break
		}
	}
	if localBuiltIn.ID == "" {
		t.Fatal("missing local built-in assist profile")
	}
	if localBuiltIn.ID != "assist.builtin.gemma4-e4b" {
		t.Fatalf("local built-in assist profile ID = %q, want assist.builtin.gemma4-e4b", localBuiltIn.ID)
	}
	if localBuiltIn.Name != "llama.cpp (Local Built-in)" {
		t.Fatalf("local built-in assist profile name = %q, want llama.cpp (Local Built-in)", localBuiltIn.Name)
	}
}

func TestAssistProfilesExposeUtilityToolCapability(t *testing.T) {
	catalog := DefaultCatalog()
	for _, profile := range catalog.Profiles {
		if profile.Modality != ModalityAssist {
			continue
		}
		if !profile.HasCapability(CapabilityToolCalling) {
			t.Fatalf("assist profile %s missing %s capability", profile.ID, CapabilityToolCalling)
		}
	}
}

func TestDefaultCatalogAdaptsStrictProfilesFromFrameworkCatalog(t *testing.T) {
	catalog := DefaultCatalog()
	frameworkProfiles := speechkit.DefaultProviderProfiles()

	for _, frameworkProfile := range frameworkProfiles {
		internalProfile, ok := findProfile(catalog, frameworkProfile.ID)
		if !ok {
			t.Fatalf("internal catalog missing framework profile %q", frameworkProfile.ID)
		}
		if got, want := internalProfile.ModelID, frameworkProfile.ModelID; got != want {
			t.Fatalf("%s model ID = %q, want %q", frameworkProfile.ID, got, want)
		}
		if got, want := string(internalProfile.ProviderKind), string(frameworkProfile.ProviderKind); got != want {
			t.Fatalf("%s provider kind = %q, want %q", frameworkProfile.ID, got, want)
		}
		if got, want := string(internalProfile.ExecutionMode), string(frameworkProfile.ExecutionMode); got != want {
			t.Fatalf("%s execution mode = %q, want %q", frameworkProfile.ID, got, want)
		}
	}
}

func findProfile(catalog Catalog, profileID string) (Profile, bool) {
	for _, profile := range catalog.Profiles {
		if profile.ID == profileID {
			return profile, true
		}
	}
	return Profile{}, false
}
