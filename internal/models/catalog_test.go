package models

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

func TestAssistProfilesExposeUtilityToolCapability(t *testing.T) {
	hostCatalog := DefaultCatalog()
	for _, profile := range hostCatalog.Profiles {
		if profile.Modality != ModalityAssist {
			continue
		}
		if !profile.HasCapability(CapabilityToolCalling) {
			t.Fatalf("assist profile %s missing %s capability", profile.ID, CapabilityToolCalling)
		}
	}
}

// The host catalog is the framework catalog plus host-only support entries.
// Before the profile types collapsed into one, a field-by-field parity test
// guarded the converter between them; there is no converter left to guard, so
// what remains worth asserting is that nothing the framework ships gets lost
// on the way through.
func TestDefaultCatalogKeepsEveryFrameworkProfile(t *testing.T) {
	hostCatalog := DefaultCatalog()
	for _, frameworkProfile := range catalog.DefaultProviderProfiles() {
		if _, ok := findProfile(hostCatalog, frameworkProfile.ID); !ok {
			t.Errorf("host catalog dropped framework profile %q", frameworkProfile.ID)
		}
	}
}

func TestDefaultCatalogProfilesCarryRuntimeMetadata(t *testing.T) {
	hostCatalog := DefaultCatalog()
	for _, profile := range hostCatalog.Profiles {
		if profile.Provider == "" {
			t.Fatalf("%s missing canonical provider", profile.ID)
		}
		if got, want := profile.Provider, catalog.NormalizeProviderID(profile.Provider); got != want {
			t.Fatalf("%s provider = %q, want canonical provider id %q", profile.ID, got, want)
		}
		if profile.AuthRequirement == "" {
			t.Fatalf("%s missing auth requirement", profile.ID)
		}
		if profile.Transport == "" {
			t.Fatalf("%s missing transport", profile.ID)
		}
	}

	utility, ok := findProfile(hostCatalog, "utility.openai.gpt-5.4-mini")
	if !ok {
		t.Fatal("OpenAI utility profile missing")
	}
	if utility.Provider != "openai" || utility.AuthRequirement != catalog.ProviderAuthAPIKey || utility.Transport != catalog.ProviderTransportHTTPS {
		t.Fatalf("OpenAI utility metadata = provider=%q auth=%q transport=%q", utility.Provider, utility.AuthRequirement, utility.Transport)
	}
}

func TestDefaultCatalogCoversProviderOptionManifestProfiles(t *testing.T) {
	hostCatalog := DefaultCatalog()
	profiles := map[string]Profile{}
	for _, profile := range hostCatalog.Profiles {
		profiles[profile.ID] = profile
	}

	for _, manifest := range provideropts.DefaultManifests() {
		for _, profileID := range manifest.ProfileIDs {
			profile, ok := profiles[profileID]
			if !ok {
				t.Fatalf("%s/%s manifest references missing profile %q", manifest.Provider, manifest.Modality, profileID)
			}
			if got, want := profile.Provider, catalog.NormalizeProviderID(manifest.Provider); got != want {
				t.Fatalf("%s provider = %q, want manifest provider %q", profileID, got, want)
			}
			if got, want := profile.Modality, modalityForManifest(manifest.Modality); got != want {
				t.Fatalf("%s modality = %q, want manifest modality %q", profileID, got, want)
			}
		}
	}
}

func modalityForManifest(modality string) Modality {
	switch modality {
	case provideropts.ModalitySTT:
		return ModalitySTT
	case provideropts.ModalityTTS:
		return ModalityTTS
	case provideropts.ModalityVoiceAgent:
		return ModalityRealtimeVoice
	default:
		return Modality(modality)
	}
}

func findProfile(hostCatalog Catalog, profileID string) (Profile, bool) {
	for _, profile := range hostCatalog.Profiles {
		if profile.ID == profileID {
			return profile, true
		}
	}
	return Profile{}, false
}
