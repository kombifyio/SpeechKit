package catalog

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestDefaultProviderCatalogSatisfiesV23Contracts(t *testing.T) {
	if err := ValidateDefaultCatalog(); err != nil {
		t.Fatalf("ValidateDefaultCatalog: %v", err)
	}
}

func TestDefaultProviderCatalogIDsAreCanonicalAndUnique(t *testing.T) {
	seen := map[string]speechkit.ProviderProfile{}
	for _, profile := range DefaultProviderProfiles() {
		if profile.ID == "" || profile.Name == "" {
			t.Fatalf("profile has incomplete identity: %#v", profile)
		}
		if speechkit.NormalizeMode(profile.Mode) == speechkit.ModeNone {
			t.Fatalf("profile %q has unsupported mode %q", profile.ID, profile.Mode)
		}
		if profile.ProviderKind == "" {
			t.Fatalf("profile %q missing provider kind", profile.ID)
		}
		if len(profile.Capabilities) == 0 {
			t.Fatalf("profile %q missing capabilities", profile.ID)
		}
		normalized := speechkit.NormalizeProviderProfileID(profile.ID)
		if normalized != profile.ID {
			t.Fatalf("catalog profile %q should already use canonical profile ID %q", profile.ID, normalized)
		}
		if prior, ok := seen[normalized]; ok {
			t.Fatalf("profiles %q and %q normalize to the same ID %q", prior.ID, profile.ID, normalized)
		}
		seen[normalized] = profile
	}
}

func TestDefaultProviderCatalogCapabilitiesStayInsideModeContracts(t *testing.T) {
	contracts := modeContractsByMode()
	for _, profile := range DefaultProviderProfiles() {
		mode := speechkit.NormalizeMode(profile.Mode)
		contract, ok := contracts[mode]
		if !ok {
			t.Fatalf("profile %q mode %q has no mode contract", profile.ID, mode)
		}
		for _, capability := range profile.Capabilities {
			if capabilitiesContain(contract.Forbidden, capability) {
				t.Fatalf("profile %q exposes forbidden capability %q for %q", profile.ID, capability, mode)
			}
			if !capabilitiesContain(contract.Allowed, capability) {
				t.Fatalf("profile %q exposes capability %q outside %q contract", profile.ID, capability, mode)
			}
		}
	}
}

func TestTTSProfilesAdvertiseTTSCapability(t *testing.T) {
	for _, profile := range ProfilesForMode(speechkit.ModeTTS) {
		if !profile.HasCapability(speechkit.CapabilityTTS) {
			t.Fatalf("tts profile %q missing CapabilityTTS", profile.ID)
		}
		// TTS mode is voice-output-only — must NOT expose STT / LLM /
		// realtime / tool-calling.
		for _, forbidden := range []speechkit.Capability{
			speechkit.CapabilityTranscription, speechkit.CapabilitySTT, speechkit.CapabilityLLM,
			speechkit.CapabilityRealtimeAudio, speechkit.CapabilityToolCalling,
		} {
			if profile.HasCapability(forbidden) {
				t.Errorf("tts profile %q exposes forbidden capability %q", profile.ID, forbidden)
			}
		}
		if err := speechkit.ValidateProfileForMode(profile, speechkit.ModeTTS); err != nil {
			t.Fatalf("tts profile %q invalid: %v", profile.ID, err)
		}
	}
}

func modeContractsByMode() map[speechkit.Mode]speechkit.ModeContract {
	contracts := map[speechkit.Mode]speechkit.ModeContract{}
	for _, contract := range speechkit.DefaultModeContracts() {
		contracts[speechkit.NormalizeMode(contract.Mode)] = contract
	}
	return contracts
}

func capabilitiesContain(values []speechkit.Capability, want speechkit.Capability) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestDictationProfilesStayTextOnly(t *testing.T) {
	for _, profile := range ProfilesForMode(speechkit.ModeDictation) {
		if profile.HasCapability(speechkit.CapabilityLLM) {
			t.Fatalf("dictation profile %q exposes LLM capability", profile.ID)
		}
		if profile.HasCapability(speechkit.CapabilityToolCalling) {
			t.Fatalf("dictation profile %q exposes tool-calling capability", profile.ID)
		}
		if err := speechkit.ValidateProfileForMode(profile, speechkit.ModeDictation); err != nil {
			t.Fatalf("dictation profile %q invalid: %v", profile.ID, err)
		}
	}
}

func TestDefaultModelRegistryRowsResolveToPublicCatalog(t *testing.T) {
	profiles := map[string]speechkit.ProviderProfile{}
	for _, profile := range DefaultProviderProfiles() {
		profiles[profile.ID] = profile
	}
	for _, descriptor := range DefaultModelRegistry() {
		if descriptor.ModelID == "" || descriptor.Provider == "" || descriptor.SourceURL == "" {
			t.Fatalf("invalid descriptor row: %#v", descriptor)
		}
		if descriptor.ProfileID == "" {
			continue
		}
		if _, ok := profiles[descriptor.ProfileID]; !ok {
			t.Fatalf("descriptor %s/%s references missing profile %q", descriptor.Provider, descriptor.ModelID, descriptor.ProfileID)
		}
	}
}
