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
