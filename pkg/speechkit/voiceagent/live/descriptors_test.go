package live

import "testing"

func TestDefaultProviderDescriptorsExposeV47Providers(t *testing.T) {
	descriptors := DefaultProviderDescriptors()
	byProvider := map[string]ProviderDescriptor{}
	for _, descriptor := range descriptors {
		byProvider[descriptor.Provider] = descriptor
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
		hasDefault := false
		for _, model := range descriptor.Models {
			if model.ModelID == "" || model.SourceURL == "" {
				t.Fatalf("%s has incomplete model descriptor: %+v", descriptor.Provider, model)
			}
			if model.Default {
				hasDefault = true
			}
		}
		if !hasDefault {
			t.Fatalf("%s missing default model descriptor", descriptor.Provider)
		}
	}

	for _, provider := range []string{"google", "deepgram", "assemblyai", "openai"} {
		if _, ok := byProvider[provider]; !ok {
			t.Fatalf("provider %q missing from descriptors", provider)
		}
	}
	if got := byProvider["assemblyai"].ProfileID; got != "realtime.assemblyai.voice-agent" {
		t.Fatalf("assemblyai profile = %q", got)
	}
	for provider, capability := range map[string]LiveCapabilityFlag{
		"assemblyai": LiveCapabilitySessionResume,
		"deepgram":   LiveCapabilityLanguageHints,
		"google":     LiveCapabilityReasoningEffort,
		"openai":     LiveCapabilityReasoningEffort,
	} {
		if !byProvider[provider].HasCapability(capability) {
			t.Fatalf("%s missing capability %s", provider, capability)
		}
	}
}
