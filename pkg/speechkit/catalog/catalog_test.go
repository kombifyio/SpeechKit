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

func TestEveryModeExposesFourProviderKinds(t *testing.T) {
	want := []speechkit.ProviderKind{
		speechkit.ProviderKindLocalBuiltIn,
		speechkit.ProviderKindLocalProvider,
		speechkit.ProviderKindCloudProvider,
		speechkit.ProviderKindDirectProvider,
	}

	for _, mode := range []speechkit.Mode{speechkit.ModeDictation, speechkit.ModeAssist, speechkit.ModeVoiceAgent, speechkit.ModeTTS} {
		got := ProviderKindsForMode(mode)
		if len(got) != len(want) {
			t.Fatalf("%s provider kinds = %#v, want %#v", mode, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s provider kind[%d] = %q, want %q", mode, i, got[i], want[i])
			}
		}
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

func TestDefaultProviderCatalogHasConcreteProfileForEachModeKind(t *testing.T) {
	profilesByModeKind := map[speechkit.Mode]map[speechkit.ProviderKind]int{}
	for _, profile := range DefaultProviderProfiles() {
		mode := speechkit.NormalizeMode(profile.Mode)
		if profilesByModeKind[mode] == nil {
			profilesByModeKind[mode] = map[speechkit.ProviderKind]int{}
		}
		profilesByModeKind[mode][profile.ProviderKind]++
	}

	for _, mode := range []speechkit.Mode{speechkit.ModeDictation, speechkit.ModeAssist, speechkit.ModeVoiceAgent, speechkit.ModeTTS} {
		for _, kind := range []speechkit.ProviderKind{
			speechkit.ProviderKindLocalBuiltIn,
			speechkit.ProviderKindLocalProvider,
			speechkit.ProviderKindCloudProvider,
			speechkit.ProviderKindDirectProvider,
		} {
			if profilesByModeKind[mode][kind] == 0 {
				t.Fatalf("mode %q has no concrete %q provider profile", mode, kind)
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

func TestNativeDictationStreamCapabilityIsExplicitlyImplemented(t *testing.T) {
	profiles := map[string]speechkit.ProviderProfile{}
	for _, profile := range ProfilesForMode(speechkit.ModeDictation) {
		profiles[profile.ID] = profile
	}

	if !profiles["stt.deepgram.nova-3"].HasCapability(speechkit.CapabilityNativeDictationStream) {
		t.Fatal("stt.deepgram.nova-3 must advertise native dictation streaming")
	}
	// AssemblyAI implements DictationStreamProvider via the Universal-3.5 Pro
	// realtime session (assemblyai_streaming.go StartDictationStream).
	if !profiles["stt.assemblyai.universal"].HasCapability(speechkit.CapabilityNativeDictationStream) {
		t.Fatal("stt.assemblyai.universal must advertise native dictation streaming")
	}
	for _, profileID := range []string{
		"stt.local.whispercpp",
		"stt.routed.whisper-large-v3",
		"stt.openrouter.whisper-1",
		"stt.openai.gpt-4o-transcribe",
		"stt.openai.whisper-1",
		"stt.google.latest-long",
		"stt.groq.whisper-large-v3-turbo",
	} {
		profile, ok := profiles[profileID]
		if !ok {
			t.Fatalf("missing dictation profile %q", profileID)
		}
		if profile.HasCapability(speechkit.CapabilityNativeDictationStream) {
			t.Fatalf("%s advertises native dictation streaming without an implemented adapter", profileID)
		}
	}
}

func TestLocalBuiltInDictationAllowsMultipleVariants(t *testing.T) {
	for _, profile := range ProfilesForMode(speechkit.ModeDictation) {
		if profile.ProviderKind != speechkit.ProviderKindLocalBuiltIn {
			continue
		}
		if len(profile.Variants) < 2 {
			t.Fatalf("local built-in dictation variants = %d, want multiple variants", len(profile.Variants))
		}
		return
	}
	t.Fatal("local built-in dictation profile missing")
}

func TestLocalBuiltInAssistUsesConcreteGemmaGGUFModel(t *testing.T) {
	for _, profile := range ProfilesForMode(speechkit.ModeAssist) {
		if profile.ProviderKind != speechkit.ProviderKindLocalBuiltIn {
			continue
		}
		if got := profile.ModelID; got != DefaultLocalBuiltInLLMModel {
			t.Fatalf("local built-in assist model ID = %q, want %q", got, DefaultLocalBuiltInLLMModel)
		}
		return
	}
	t.Fatal("local built-in assist profile missing")
}

func TestV47RealtimeProviderCatalogUsesCurrentModelBaselines(t *testing.T) {
	profiles := map[string]speechkit.ProviderProfile{}
	for _, profile := range ProfilesForMode(speechkit.ModeVoiceAgent) {
		profiles[profile.ID] = profile
	}

	if got := profiles["realtime.google.gemini-native-audio"].ModelID; got != ModelGemini31FlashLivePreview {
		t.Fatalf("Gemini Live model = %q, want %q", got, ModelGemini31FlashLivePreview)
	}
	if got := profiles["realtime.google.gemini-live-translate"].ModelID; got != ModelGemini35LiveTranslatePreview {
		t.Fatalf("Gemini Live Translate model = %q, want %q", got, ModelGemini35LiveTranslatePreview)
	}
	if !profiles["realtime.google.gemini-live-translate"].HasCapability(speechkit.CapabilityTranslation) {
		t.Fatal("Gemini Live Translate profile missing translation capability")
	}
	if got := profiles["realtime.deepgram.voice-agent"].ModelID; got != ModelDeepgramFluxGeneralMulti+"+aura-2" {
		t.Fatalf("Deepgram Voice Agent model = %q, want Flux listen baseline", got)
	}
	for _, id := range []string{
		"realtime.assemblyai.voice-agent",
		"realtime.openai.gpt-realtime-2",
	} {
		profile, ok := profiles[id]
		if !ok {
			t.Fatalf("%s missing from realtime voice catalog", id)
		}
		if !profile.HasCapability(speechkit.CapabilityRealtimeAudio) {
			t.Fatalf("%s missing realtime audio capability", id)
		}
	}
}

func TestRealtimeProviderCatalogCarriesInterchangeabilityMetadata(t *testing.T) {
	cases := map[string]struct {
		provider     string
		lifecycle    speechkit.ModelLifecycle
		capabilities []speechkit.Capability
	}{
		"realtime.google.gemini-native-audio": {
			provider:     "google",
			lifecycle:    speechkit.ModelLifecyclePreview,
			capabilities: []speechkit.Capability{speechkit.CapabilityNativeContextPrompt, speechkit.CapabilityReasoningEffort},
		},
		"realtime.google.gemini-live-translate": {
			provider:     "google",
			lifecycle:    speechkit.ModelLifecyclePreview,
			capabilities: []speechkit.Capability{speechkit.CapabilityTranslation, speechkit.CapabilityTranscript},
		},
		"realtime.deepgram.voice-agent": {
			provider:     "deepgram",
			lifecycle:    speechkit.ModelLifecycleGA,
			capabilities: []speechkit.Capability{speechkit.CapabilityNativeKeyterms, speechkit.CapabilityLanguageHints},
		},
		"realtime.assemblyai.voice-agent": {
			provider:     "assemblyai",
			lifecycle:    speechkit.ModelLifecycleGA,
			capabilities: []speechkit.Capability{speechkit.CapabilityNativeKeyterms, speechkit.CapabilitySessionResume},
		},
		"realtime.openai.gpt-realtime-2": {
			provider:     "openai",
			lifecycle:    speechkit.ModelLifecycleGA,
			capabilities: []speechkit.Capability{speechkit.CapabilityReasoningEffort, speechkit.CapabilityTranscriptionOnly},
		},
	}

	profiles := map[string]speechkit.ProviderProfile{}
	for _, profile := range ProfilesForMode(speechkit.ModeVoiceAgent) {
		profiles[profile.ID] = profile
	}
	for profileID, tc := range cases {
		profile, ok := profiles[profileID]
		if !ok {
			t.Fatalf("missing realtime profile %q", profileID)
		}
		if profile.Provider != tc.provider {
			t.Fatalf("%s provider = %q, want %q", profileID, profile.Provider, tc.provider)
		}
		if profile.Lifecycle != tc.lifecycle {
			t.Fatalf("%s lifecycle = %q, want %q", profileID, profile.Lifecycle, tc.lifecycle)
		}
		if profile.AuthRequirement != "api_key" || profile.Transport != "websocket" || profile.EvidenceURL == "" {
			t.Fatalf("%s metadata incomplete: auth=%q transport=%q evidence=%q", profileID, profile.AuthRequirement, profile.Transport, profile.EvidenceURL)
		}
		if len(profile.NativeOptions) == 0 {
			t.Fatalf("%s native options missing", profileID)
		}
		for _, capability := range tc.capabilities {
			if !profile.HasCapability(capability) {
				t.Fatalf("%s missing capability %q", profileID, capability)
			}
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
