package speechkit

import (
	"os"
	"strings"
	"testing"
)

func TestDefaultProviderCatalogSatisfiesV23Contracts(t *testing.T) {
	if err := ValidateDefaultCatalog(); err != nil {
		t.Fatalf("ValidateDefaultCatalog: %v", err)
	}
}

func TestEveryModeExposesFourProviderKinds(t *testing.T) {
	want := []ProviderKind{
		ProviderKindLocalBuiltIn,
		ProviderKindLocalProvider,
		ProviderKindCloudProvider,
		ProviderKindDirectProvider,
	}

	for _, mode := range []Mode{ModeDictation, ModeAssist, ModeVoiceAgent, ModeTTS} {
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
	seen := map[string]ProviderProfile{}
	for _, profile := range DefaultProviderProfiles() {
		if profile.ID == "" || profile.Name == "" {
			t.Fatalf("profile has incomplete identity: %#v", profile)
		}
		if NormalizeMode(profile.Mode) == ModeNone {
			t.Fatalf("profile %q has unsupported mode %q", profile.ID, profile.Mode)
		}
		if profile.ProviderKind == "" {
			t.Fatalf("profile %q missing provider kind", profile.ID)
		}
		if len(profile.Capabilities) == 0 {
			t.Fatalf("profile %q missing capabilities", profile.ID)
		}
		normalized := NormalizeProviderProfileID(profile.ID)
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
		mode := NormalizeMode(profile.Mode)
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
	profilesByModeKind := map[Mode]map[ProviderKind]int{}
	for _, profile := range DefaultProviderProfiles() {
		mode := NormalizeMode(profile.Mode)
		if profilesByModeKind[mode] == nil {
			profilesByModeKind[mode] = map[ProviderKind]int{}
		}
		profilesByModeKind[mode][profile.ProviderKind]++
	}

	for _, mode := range []Mode{ModeDictation, ModeAssist, ModeVoiceAgent, ModeTTS} {
		for _, kind := range []ProviderKind{
			ProviderKindLocalBuiltIn,
			ProviderKindLocalProvider,
			ProviderKindCloudProvider,
			ProviderKindDirectProvider,
		} {
			if profilesByModeKind[mode][kind] == 0 {
				t.Fatalf("mode %q has no concrete %q provider profile", mode, kind)
			}
		}
	}
}

func TestTTSProfilesAdvertiseTTSCapability(t *testing.T) {
	for _, profile := range ProfilesForMode(ModeTTS) {
		if !profile.HasCapability(CapabilityTTS) {
			t.Fatalf("tts profile %q missing CapabilityTTS", profile.ID)
		}
		// TTS mode is voice-output-only — must NOT expose STT / LLM /
		// realtime / tool-calling.
		for _, forbidden := range []Capability{
			CapabilityTranscription, CapabilitySTT, CapabilityLLM,
			CapabilityRealtimeAudio, CapabilityToolCalling,
		} {
			if profile.HasCapability(forbidden) {
				t.Errorf("tts profile %q exposes forbidden capability %q", profile.ID, forbidden)
			}
		}
		if err := ValidateProfileForMode(profile, ModeTTS); err != nil {
			t.Fatalf("tts profile %q invalid: %v", profile.ID, err)
		}
	}
}

func modeContractsByMode() map[Mode]ModeContract {
	contracts := map[Mode]ModeContract{}
	for _, contract := range DefaultModeContracts() {
		contracts[NormalizeMode(contract.Mode)] = contract
	}
	return contracts
}

func capabilitiesContain(values []Capability, want Capability) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestNormalizeMode_TTSAliases(t *testing.T) {
	for _, alias := range []string{"tts", "voice_output", "speak", "speech"} {
		if got := NormalizeMode(Mode(alias)); got != ModeTTS {
			t.Errorf("NormalizeMode(%q) = %q, want ModeTTS", alias, got)
		}
	}
}

func TestDictationProfilesStayTextOnly(t *testing.T) {
	for _, profile := range ProfilesForMode(ModeDictation) {
		if profile.HasCapability(CapabilityLLM) {
			t.Fatalf("dictation profile %q exposes LLM capability", profile.ID)
		}
		if profile.HasCapability(CapabilityToolCalling) {
			t.Fatalf("dictation profile %q exposes tool-calling capability", profile.ID)
		}
		if err := ValidateProfileForMode(profile, ModeDictation); err != nil {
			t.Fatalf("dictation profile %q invalid: %v", profile.ID, err)
		}
	}
}

func TestNativeDictationStreamCapabilityIsExplicitlyImplemented(t *testing.T) {
	profiles := map[string]ProviderProfile{}
	for _, profile := range ProfilesForMode(ModeDictation) {
		profiles[profile.ID] = profile
	}

	if !profiles["stt.deepgram.nova-3"].HasCapability(CapabilityNativeDictationStream) {
		t.Fatal("stt.deepgram.nova-3 must advertise native dictation streaming")
	}
	// AssemblyAI implements DictationStreamProvider via the Universal-3.5 Pro
	// realtime session (assemblyai_streaming.go StartDictationStream).
	if !profiles["stt.assemblyai.universal"].HasCapability(CapabilityNativeDictationStream) {
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
		if profile.HasCapability(CapabilityNativeDictationStream) {
			t.Fatalf("%s advertises native dictation streaming without an implemented adapter", profileID)
		}
	}
}

func TestLocalBuiltInDictationAllowsMultipleVariants(t *testing.T) {
	for _, profile := range ProfilesForMode(ModeDictation) {
		if profile.ProviderKind != ProviderKindLocalBuiltIn {
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
	for _, profile := range ProfilesForMode(ModeAssist) {
		if profile.ProviderKind != ProviderKindLocalBuiltIn {
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
	profiles := map[string]ProviderProfile{}
	for _, profile := range ProfilesForMode(ModeVoiceAgent) {
		profiles[profile.ID] = profile
	}

	if got := profiles["realtime.google.gemini-native-audio"].ModelID; got != ModelGemini31FlashLivePreview {
		t.Fatalf("Gemini Live model = %q, want %q", got, ModelGemini31FlashLivePreview)
	}
	if got := profiles["realtime.google.gemini-live-translate"].ModelID; got != ModelGemini35LiveTranslatePreview {
		t.Fatalf("Gemini Live Translate model = %q, want %q", got, ModelGemini35LiveTranslatePreview)
	}
	if !profiles["realtime.google.gemini-live-translate"].HasCapability(CapabilityTranslation) {
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
		if !profile.HasCapability(CapabilityRealtimeAudio) {
			t.Fatalf("%s missing realtime audio capability", id)
		}
	}
}

func TestRealtimeProviderCatalogCarriesInterchangeabilityMetadata(t *testing.T) {
	cases := map[string]struct {
		provider     string
		lifecycle    ModelLifecycle
		capabilities []Capability
	}{
		"realtime.google.gemini-native-audio": {
			provider:     "google",
			lifecycle:    ModelLifecyclePreview,
			capabilities: []Capability{CapabilityNativeContextPrompt, CapabilityReasoningEffort},
		},
		"realtime.google.gemini-live-translate": {
			provider:     "google",
			lifecycle:    ModelLifecyclePreview,
			capabilities: []Capability{CapabilityTranslation, CapabilityTranscript},
		},
		"realtime.deepgram.voice-agent": {
			provider:     "deepgram",
			lifecycle:    ModelLifecycleGA,
			capabilities: []Capability{CapabilityNativeKeyterms, CapabilityLanguageHints},
		},
		"realtime.assemblyai.voice-agent": {
			provider:     "assemblyai",
			lifecycle:    ModelLifecycleGA,
			capabilities: []Capability{CapabilityNativeKeyterms, CapabilitySessionResume},
		},
		"realtime.openai.gpt-realtime-2": {
			provider:     "openai",
			lifecycle:    ModelLifecycleGA,
			capabilities: []Capability{CapabilityReasoningEffort, CapabilityTranscriptionOnly},
		},
	}

	profiles := map[string]ProviderProfile{}
	for _, profile := range ProfilesForMode(ModeVoiceAgent) {
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
	profiles := map[string]ProviderProfile{}
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

func TestFrameworkCatalogDoesNotImportDesktopInternals(t *testing.T) {
	body, err := os.ReadFile("catalog.go")
	if err != nil {
		t.Fatalf("read catalog.go: %v", err)
	}
	if strings.Contains(string(body), "/internal/") ||
		strings.Contains(string(body), "kombify-SpeechKit/internal/") {
		t.Fatal("catalog.go imports desktop internals; pkg/speechkit must own the public framework catalog")
	}
}

func TestPublicSDKDoesNotImportInternalPackages(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read pkg/speechkit: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if strings.Contains(string(body), "/internal/") ||
			strings.Contains(string(body), "kombify-SpeechKit/internal/") {
			t.Fatalf("%s imports desktop internals; pkg/speechkit must remain externally embeddable", entry.Name())
		}
	}
}
