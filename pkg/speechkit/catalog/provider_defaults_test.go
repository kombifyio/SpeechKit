package catalog

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestProviderIDForProfileCoversEveryCatalogProfile(t *testing.T) {
	for _, profile := range DefaultProviderProfiles() {
		provider := ProviderIDForProfile(profile)
		if provider == "" {
			t.Fatalf("ProviderIDForProfile(%q) is empty", profile.ID)
		}
		if provider != NormalizeProviderID(provider) {
			t.Fatalf("ProviderIDForProfile(%q) = %q, want canonical provider id", profile.ID, provider)
		}
	}

	cases := map[string]string{
		"gemini":                                "google",
		"hf":                                    "huggingface",
		"open-router":                           "openrouter",
		"realtime.google.gemini-native-audio":   "google",
		"realtime.google.gemini-live-translate": "google",
		"stt.openai.gpt-4o-transcribe":          "openai",
		"stt.groq.whisper-large-v3-turbo":       "groq",
		"stt.deepgram.nova-3":                   "deepgram",
		"speaker.assemblyai.diarization":        "assemblyai",
		"tts.openedai.kokoro":                   "openedai",
		"utility.builtin.gemma4-e4b":            "local",
		"utility.routed.qwen35-9b":              "huggingface",
		"tts.routed.qwen3-tts-1.7b":             "huggingface",
		"utility.acme.custom-llm":               "acme",
	}
	for input, want := range cases {
		if got := NormalizeProviderID(input); got != want {
			t.Fatalf("NormalizeProviderID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestProviderIDForExecutionMode(t *testing.T) {
	cases := map[speechkit.ExecutionMode]string{
		speechkit.ExecutionModeLocal:          "local",
		speechkit.ExecutionModeSelfHostedHTTP: "selfhosted",
		speechkit.ExecutionModeHFRouted:       "huggingface",
		speechkit.ExecutionModeOpenAI:         "openai",
		speechkit.ExecutionModeGroq:           "groq",
		speechkit.ExecutionModeGoogle:         "google",
		speechkit.ExecutionModeDeepgram:       "deepgram",
		speechkit.ExecutionModeAssemblyAI:     "assemblyai",
		speechkit.ExecutionModeOllama:         "ollama",
		speechkit.ExecutionModeOpenRouter:     "openrouter",
	}
	for mode, want := range cases {
		if got := ProviderIDForExecutionMode(mode); got != want {
			t.Fatalf("ProviderIDForExecutionMode(%q) = %q, want %q", mode, got, want)
		}
	}
}

func TestProviderProfileDefaultsFillRuntimeMetadata(t *testing.T) {
	for _, profile := range DefaultProviderProfiles() {
		if profile.Provider == "" {
			t.Fatalf("profile %q missing canonical provider", profile.ID)
		}
		if profile.Provider != NormalizeProviderID(profile.Provider) {
			t.Fatalf("profile %q provider = %q, want canonical provider id", profile.ID, profile.Provider)
		}
		if profile.AuthRequirement == "" {
			t.Fatalf("profile %q missing auth requirement", profile.ID)
		}
		if profile.Transport == "" {
			t.Fatalf("profile %q missing transport", profile.ID)
		}
	}

}

func TestDefaultProviderDefaultsResolveCatalogProfiles(t *testing.T) {
	profiles := map[string]speechkit.ProviderProfile{}
	for _, profile := range DefaultProviderProfiles() {
		profiles[profile.ID] = profile
	}
	for _, providerDefault := range DefaultProviderDefaults() {
		profile, ok := profiles[providerDefault.ProfileID]
		if !ok {
			t.Fatalf("default %s/%s points at missing catalog profile %q", providerDefault.Provider, providerDefault.Mode, providerDefault.ProfileID)
		}
		if providerDefault.Provider != ProviderIDForProfile(profile) {
			t.Fatalf("default %q provider = %q, want %q", providerDefault.ProfileID, providerDefault.Provider, ProviderIDForProfile(profile))
		}
		if providerDefault.Mode != speechkit.NormalizeMode(profile.Mode) {
			t.Fatalf("default %q mode = %q, want %q", providerDefault.ProfileID, providerDefault.Mode, speechkit.NormalizeMode(profile.Mode))
		}
		if providerDefault.ModelID != profile.ModelID {
			t.Fatalf("default %q model = %q, want catalog model %q", providerDefault.ProfileID, providerDefault.ModelID, profile.ModelID)
		}
		if providerDefault.ProviderKind != profile.ProviderKind {
			t.Fatalf("default %q provider kind = %q, want %q", providerDefault.ProfileID, providerDefault.ProviderKind, profile.ProviderKind)
		}
	}
}

func TestGoogleProviderMatrixIncludesLiveTranslateWithoutChangingDefault(t *testing.T) {
	row, ok := FindProviderMatrixRow("google")
	if !ok {
		t.Fatal("google row missing")
	}
	var translate ProviderDefault
	for _, profile := range row.Profiles {
		if profile.ProfileID == "realtime.google.gemini-live-translate" {
			translate = profile
			break
		}
	}
	if translate.ProfileID == "" {
		t.Fatal("google row should include Gemini Live Translate profile")
	}
	if !translate.Experimental || !providerDefaultHasCapability(translate, speechkit.CapabilityTranslation) {
		t.Fatalf("translate profile metadata = experimental:%v capabilities:%v", translate.Experimental, translate.Capabilities)
	}
	if !stringsContain(translate.NativeOptions, "translation") {
		t.Fatalf("translate native options = %v, want translation", translate.NativeOptions)
	}

	defaultProfile, ok := FindProviderDefault("google", speechkit.ModeVoiceAgent)
	if !ok {
		t.Fatal("google voice agent default missing")
	}
	if defaultProfile.ProfileID != "realtime.google.gemini-native-audio" {
		t.Fatalf("google voice agent default = %q, want native dialogue profile", defaultProfile.ProfileID)
	}
	if defaultProfile.ModelID == translate.ModelID {
		t.Fatalf("google voice agent default model should not use translate-only model %q", translate.ModelID)
	}
}

func TestProviderMatrixCarriesProviderNativeOptions(t *testing.T) {
	row, ok := FindProviderMatrixRow("deepgram")
	if !ok {
		t.Fatal("deepgram row missing")
	}
	support, ok := row.Feature(ProviderFeatureDictation)
	if !ok {
		t.Fatal("deepgram dictation support missing")
	}
	for _, want := range []string{"keyterms", "punctuation", "speaker_diarization"} {
		if !stringsContain(support.NativeOptions, want) {
			t.Fatalf("deepgram dictation native options = %v, missing %q", support.NativeOptions, want)
		}
	}
}

func stringsContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
