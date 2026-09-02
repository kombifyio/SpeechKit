package live

import (
	"testing"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

func TestDefaultProviderDescriptorsExposeV47Providers(t *testing.T) {
	descriptors := DefaultProviderDescriptors()
	byProfile := map[string]ProviderDescriptor{}
	providers := map[string]bool{}
	for _, descriptor := range descriptors {
		byProfile[descriptor.ProfileID] = descriptor
		providers[descriptor.Provider] = true
		if descriptor.ProfileID == "" {
			t.Fatalf("%s missing profile id", descriptor.Provider)
		}
		if descriptor.AuthRequirement == "" || descriptor.Transport == "" || descriptor.EvidenceURL == "" {
			t.Fatalf("%s missing public provider metadata: %+v", descriptor.Provider, descriptor)
		}
		if len(descriptor.Models) == 0 {
			t.Fatalf("%s missing model descriptors", descriptor.Provider)
		}
		if len(descriptor.NativeOptions) == 0 {
			t.Fatalf("%s missing native option descriptors", descriptor.Provider)
		}
		for _, model := range descriptor.Models {
			if model.ModelID == "" || model.SourceURL == "" {
				t.Fatalf("%s has incomplete model descriptor: %+v", descriptor.Provider, model)
			}
		}
		if _, ok := descriptor.DefaultModel(); !ok {
			t.Fatalf("%s missing selectable model descriptor", descriptor.Provider)
		}
	}

	for _, provider := range []string{"google", "deepgram", "assemblyai", "openai"} {
		if !providers[provider] {
			t.Fatalf("provider %q missing from descriptors", provider)
		}
	}
	if got := byProfile["realtime.assemblyai.voice-agent"].ProfileID; got != "realtime.assemblyai.voice-agent" {
		t.Fatalf("assemblyai profile = %q", got)
	}
	for profileID, capability := range map[string]LiveCapabilityFlag{
		"realtime.assemblyai.voice-agent":       LiveCapabilitySessionResume,
		"realtime.deepgram.voice-agent":         LiveCapabilityLanguageHints,
		"realtime.google.gemini-native-audio":   LiveCapabilityReasoningEffort,
		"realtime.google.gemini-live-translate": LiveCapabilityTranslation,
		"realtime.openai.gpt-realtime-2":        LiveCapabilityReasoningEffort,
	} {
		if !byProfile[profileID].HasCapability(capability) {
			t.Fatalf("%s missing capability %s", profileID, capability)
		}
	}
}

func TestDefaultProviderDescriptorsResolveThroughFrameworkCatalog(t *testing.T) {
	profiles := map[string]framework.ProviderProfile{}
	for _, profile := range catalog.DefaultProviderProfiles() {
		profiles[profile.ID] = profile
	}

	for _, descriptor := range DefaultProviderDescriptors() {
		profile, ok := profiles[descriptor.ProfileID]
		if !ok {
			t.Fatalf("%s descriptor references missing framework profile %q", descriptor.Provider, descriptor.ProfileID)
		}
		if framework.NormalizeMode(profile.Mode) != framework.ModeVoiceAgent {
			t.Fatalf("%s profile mode = %q, want voice_agent", descriptor.ProfileID, profile.Mode)
		}
		if profile.Provider != descriptor.Provider {
			t.Fatalf("%s profile provider = %q, want descriptor provider %q", descriptor.ProfileID, profile.Provider, descriptor.Provider)
		}
		if profile.AuthRequirement != descriptor.AuthRequirement || profile.Transport != descriptor.Transport || profile.EvidenceURL != descriptor.EvidenceURL {
			t.Fatalf("%s framework metadata diverges from descriptor: profile=%+v descriptor=%+v", descriptor.ProfileID, profile, descriptor)
		}
		if !profile.HasCapability(framework.CapabilityRealtimeAudio) {
			t.Fatalf("%s framework profile missing realtime audio capability", descriptor.ProfileID)
		}

		byProfile, ok := FindProviderDescriptor(descriptor.ProfileID)
		if !ok || byProfile.Provider != descriptor.Provider || byProfile.ProfileID != descriptor.ProfileID {
			t.Fatalf("FindProviderDescriptor(%q) = %+v, %v", descriptor.ProfileID, byProfile, ok)
		}

		cfg, ok := DefaultLiveConfigForProvider(descriptor.ProfileID)
		if !ok {
			t.Fatalf("DefaultLiveConfigForProvider(%q) did not resolve", descriptor.ProfileID)
		}
		model, ok := descriptor.DefaultModel()
		if !ok {
			t.Fatalf("%s descriptor has no default model", descriptor.Provider)
		}
		if cfg.Provider != descriptor.Provider || cfg.ProfileID != descriptor.ProfileID || cfg.Model != model.ModelID {
			t.Fatalf("default config for %s = %+v, want provider/profile/model %s/%s/%s", descriptor.Provider, cfg, descriptor.Provider, descriptor.ProfileID, model.ModelID)
		}
	}

	byProvider, ok := FindProviderDescriptor("google")
	if !ok || byProvider.ProfileID != "realtime.google.gemini-native-audio" {
		t.Fatalf("FindProviderDescriptor(google) = %+v, %v; want general Gemini Live descriptor", byProvider, ok)
	}
}

func TestSessionCapabilitiesForProviderUsesDescriptorAndDefensiveCopy(t *testing.T) {
	caps := SessionCapabilitiesForProvider("gemini")
	if caps.Provider != "google" || caps.ProfileID != "realtime.google.gemini-native-audio" || caps.Model == "" {
		t.Fatalf("session capabilities = %+v, want normalized google descriptor", caps)
	}
	if !liveCapabilityFlagsContain(caps.Capabilities, LiveCapabilityRealtimeAudio) {
		t.Fatalf("session capabilities = %+v, want realtime audio", caps.Capabilities)
	}

	caps.Capabilities[0] = LiveCapabilityMedicalDomain
	again := SessionCapabilitiesForProvider("google")
	if len(again.Capabilities) == 0 || again.Capabilities[0] == LiveCapabilityMedicalDomain {
		t.Fatalf("session capabilities should return a defensive descriptor copy: %+v", again.Capabilities)
	}

	unknown := SessionCapabilitiesForProvider("custom_provider")
	if unknown.Provider != "custom-provider" || unknown.ProfileID != "" || len(unknown.Capabilities) != 0 {
		t.Fatalf("unknown provider capabilities = %+v, want normalized empty descriptor", unknown)
	}
}

func liveCapabilityFlagsContain(values []LiveCapabilityFlag, want LiveCapabilityFlag) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
