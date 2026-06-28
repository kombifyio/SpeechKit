package models

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
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

func TestDefaultCatalogRecommendsGemma4ForBuiltInAssist(t *testing.T) {
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
	if localBuiltIn.Name != "Gemma 4 E4B (Local Built-in)" {
		t.Fatalf("local built-in assist profile name = %q, want Gemma 4 E4B (Local Built-in)", localBuiltIn.Name)
	}
	if localBuiltIn.ModelID != speechkit.DefaultLocalBuiltInLLMModel {
		t.Fatalf("local built-in assist model ID = %q, want %q", localBuiltIn.ModelID, speechkit.DefaultLocalBuiltInLLMModel)
	}
	if !localBuiltIn.Recommended {
		t.Fatal("local built-in assist profile should be recommended")
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
		if got, want := internalProfile.Provider, frameworkProfile.Provider; got != want {
			t.Fatalf("%s provider = %q, want %q", frameworkProfile.ID, got, want)
		}
		if got, want := internalProfile.Lifecycle, string(frameworkProfile.Lifecycle); got != want {
			t.Fatalf("%s lifecycle = %q, want %q", frameworkProfile.ID, got, want)
		}
		if got, want := internalProfile.AuthRequirement, frameworkProfile.AuthRequirement; got != want {
			t.Fatalf("%s auth requirement = %q, want %q", frameworkProfile.ID, got, want)
		}
	}
}

func TestDefaultCatalogProfilesCarryRuntimeMetadata(t *testing.T) {
	catalog := DefaultCatalog()
	for _, profile := range catalog.Profiles {
		if profile.Provider == "" {
			t.Fatalf("%s missing canonical provider", profile.ID)
		}
		if got, want := profile.Provider, speechkit.NormalizeProviderID(profile.Provider); got != want {
			t.Fatalf("%s provider = %q, want canonical provider id %q", profile.ID, got, want)
		}
		if profile.AuthRequirement == "" {
			t.Fatalf("%s missing auth requirement", profile.ID)
		}
		if profile.Transport == "" {
			t.Fatalf("%s missing transport", profile.ID)
		}
	}

	utility, ok := findProfile(catalog, "utility.openai.gpt-5.4-mini")
	if !ok {
		t.Fatal("OpenAI utility profile missing")
	}
	if utility.Provider != "openai" || utility.AuthRequirement != speechkit.ProviderAuthAPIKey || utility.Transport != speechkit.ProviderTransportHTTPS {
		t.Fatalf("OpenAI utility metadata = provider=%q auth=%q transport=%q", utility.Provider, utility.AuthRequirement, utility.Transport)
	}
}

func TestDefaultCatalogCoversProviderOptionManifestProfiles(t *testing.T) {
	catalog := DefaultCatalog()
	profiles := map[string]Profile{}
	for _, profile := range catalog.Profiles {
		profiles[profile.ID] = profile
	}

	for _, manifest := range provideropts.DefaultManifests() {
		for _, profileID := range manifest.ProfileIDs {
			profile, ok := profiles[profileID]
			if !ok {
				t.Fatalf("%s/%s manifest references missing profile %q", manifest.Provider, manifest.Modality, profileID)
			}
			if got, want := profile.Provider, speechkit.NormalizeProviderID(manifest.Provider); got != want {
				t.Fatalf("%s provider = %q, want manifest provider %q", profileID, got, want)
			}
			if got, want := profile.Modality, modalityForManifest(manifest.Modality); got != want {
				t.Fatalf("%s modality = %q, want manifest modality %q", profileID, got, want)
			}
		}
	}
}

func TestDefaultCatalogKeepsRealtimeProviderMetadata(t *testing.T) {
	catalog := DefaultCatalog()
	profile, ok := findProfile(catalog, "realtime.assemblyai.voice-agent")
	if !ok {
		t.Fatal("AssemblyAI Voice Agent profile missing")
	}
	if profile.Provider != "assemblyai" || profile.Lifecycle != "ga" || profile.AuthRequirement != "api_key" || profile.Transport != "websocket" {
		t.Fatalf("AssemblyAI metadata = provider=%q lifecycle=%q auth=%q transport=%q", profile.Provider, profile.Lifecycle, profile.AuthRequirement, profile.Transport)
	}
	if !profile.HasCapability(CapabilityNativeKeyterms) || !profile.HasCapability(CapabilitySessionResume) {
		t.Fatalf("AssemblyAI capabilities = %#v, want native keyterms + resume", profile.Capabilities)
	}
	if len(profile.NativeOptions) == 0 || len(profile.SupportedLocales) == 0 || profile.EvidenceURL == "" {
		t.Fatalf("AssemblyAI registry metadata incomplete: native=%#v locales=%#v evidence=%q", profile.NativeOptions, profile.SupportedLocales, profile.EvidenceURL)
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

func findProfile(catalog Catalog, profileID string) (Profile, bool) {
	for _, profile := range catalog.Profiles {
		if profile.ID == profileID {
			return profile, true
		}
	}
	return Profile{}, false
}
