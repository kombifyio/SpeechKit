package catalog

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestDefaultProviderMatrixStandardizesKnownProviders(t *testing.T) {
	rows := DefaultProviderMatrix()
	if len(rows) < 10 {
		t.Fatalf("provider matrix rows = %d, want at least 10", len(rows))
	}

	for _, provider := range []string{
		"local",
		"ollama",
		"huggingface",
		"openrouter",
		"openai",
		"google",
		"deepgram",
		"assemblyai",
		"groq",
		"openedai",
	} {
		row, ok := FindProviderMatrixRow(provider)
		if !ok {
			t.Fatalf("provider %q missing from default matrix", provider)
		}
		if row.DisplayName == "" || len(row.Profiles) == 0 {
			t.Fatalf("provider row %q incomplete: %+v", provider, row)
		}
	}

	profileCount := 0
	for _, row := range rows {
		if row.Provider == "" || row.DisplayName == "" {
			t.Fatalf("provider row has empty identity: %+v", row)
		}
		if len(row.Features) != len(providerFeatureOrder) {
			t.Fatalf("%s feature count = %d, want %d", row.Provider, len(row.Features), len(providerFeatureOrder))
		}
		for _, profile := range row.Profiles {
			profileCount++
			if profile.Provider != row.Provider {
				t.Fatalf("%s profile %q provider = %q, want row provider", row.Provider, profile.ProfileID, profile.Provider)
			}
			if profile.ProfileID == "" || profile.DisplayName == "" || profile.Support == "" {
				t.Fatalf("profile default incomplete: %+v", profile)
			}
			if profile.AuthRequirement == "" || profile.Transport == "" {
				t.Fatalf("profile default %q missing auth/transport metadata: auth=%q transport=%q", profile.ProfileID, profile.AuthRequirement, profile.Transport)
			}
			if profile.CredentialRequired && profile.CredentialTarget == "" {
				t.Fatalf("profile default %q requires credentials without a credential target", profile.ProfileID)
			}
			if !profile.CredentialRequired && profile.CredentialTarget != "" {
				t.Fatalf("profile default %q has credential target %q despite credentialRequired=false", profile.ProfileID, profile.CredentialTarget)
			}
			if speechkit.NormalizeMode(profile.Mode) == speechkit.ModeNone {
				t.Fatalf("profile default %q has invalid mode %q", profile.ProfileID, profile.Mode)
			}
		}
	}
	if profileCount != len(DefaultProviderProfiles()) {
		t.Fatalf("matrix profiles = %d, want catalog profiles %d", profileCount, len(DefaultProviderProfiles()))
	}
}

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

	cases := []struct {
		profileID string
		auth      string
		transport string
		target    string
	}{
		{"stt.local.whispercpp", ProviderAuthHostDependencies, ProviderTransportLocal, ""},
		{"stt.routed.whisper-large-v3", ProviderAuthToken, ProviderTransportHTTPS, "huggingface"},
		{"stt.google.latest-long", ProviderAuthAPIKey, ProviderTransportHTTPS, "google_stt"},
		{"realtime.builtin.pipeline", ProviderAuthHostDependencies, ProviderTransportPipeline, ""},
		{"realtime.openai.gpt-realtime-2", ProviderAuthAPIKey, ProviderTransportWebSocket, "openai"},
		{"tts.openedai.kokoro", ProviderAuthOptionalAPIKey, ProviderTransportHTTP, ""},
	}
	profiles := map[string]speechkit.ProviderProfile{}
	for _, profile := range DefaultProviderProfiles() {
		profiles[profile.ID] = profile
	}
	for _, tc := range cases {
		profile, ok := profiles[tc.profileID]
		if !ok {
			t.Fatalf("missing profile %q", tc.profileID)
		}
		if profile.AuthRequirement != tc.auth || profile.Transport != tc.transport {
			t.Fatalf("%s metadata = auth %q transport %q, want auth %q transport %q", tc.profileID, profile.AuthRequirement, profile.Transport, tc.auth, tc.transport)
		}
		if target := ProviderCredentialTarget(profile); target != tc.target {
			t.Fatalf("%s credential target = %q, want %q", tc.profileID, target, tc.target)
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

func TestDefaultProviderDefaultsSelectsOneProfilePerProviderMode(t *testing.T) {
	defaults := DefaultProviderDefaults()
	if len(defaults) == 0 {
		t.Fatal("default provider defaults are empty")
	}

	seen := map[string]ProviderDefault{}
	for _, profile := range defaults {
		key := profile.Provider + "/" + string(profile.Mode)
		if prior, ok := seen[key]; ok {
			t.Fatalf("duplicate provider default for %s: %s and %s", key, prior.ProfileID, profile.ProfileID)
		}
		seen[key] = profile
	}

	cases := []struct {
		provider string
		mode     speechkit.Mode
		wantID   string
	}{
		{"local", speechkit.ModeDictation, "stt.local.whispercpp"},
		{"local", speechkit.ModeTTS, "tts.local.kokoro-82m"},
		{"hf", speechkit.ModeDictation, "stt.routed.whisper-large-v3"},
		{"openai", speechkit.ModeDictation, "stt.openai.gpt-4o-transcribe"},
		{"openai", speechkit.ModeTTS, "tts.openai.tts-1-hd"},
		{"deepgram", speechkit.ModeDictation, "stt.deepgram.nova-3"},
		{"assembly-ai", speechkit.ModeVoiceAgent, "realtime.assemblyai.voice-agent"},
	}
	for _, tc := range cases {
		profile, ok := FindProviderDefault(tc.provider, tc.mode)
		if !ok {
			t.Fatalf("FindProviderDefault(%q, %q) did not resolve", tc.provider, tc.mode)
		}
		if profile.ProfileID != tc.wantID {
			t.Fatalf("FindProviderDefault(%q, %q) = %q, want %q", tc.provider, tc.mode, profile.ProfileID, tc.wantID)
		}
	}
}

func TestDefaultProviderMatrixFeatureSupport(t *testing.T) {
	assertFeatureSupport(t, "deepgram", ProviderFeatureDictationStreaming, ProviderSupportNative, "stt.deepgram.nova-3")
	assertFeatureSupport(t, "deepgram", ProviderFeatureRealtimeVoice, ProviderSupportNative, "realtime.deepgram.voice-agent")
	assertFeatureSupport(t, "deepgram", ProviderFeatureTTS, ProviderSupportNative, "tts.deepgram.aura-2")
	assertFeatureSupport(t, "huggingface", ProviderFeatureDictation, ProviderSupportRouted, "stt.routed.whisper-large-v3")
	assertFeatureSupport(t, "huggingface", ProviderFeatureRealtimeVoice, ProviderSupportCascaded, "realtime.hf.qwen35-27b")
	assertFeatureSupport(t, "openrouter", ProviderFeatureAssist, ProviderSupportRouted, "assist.openrouter.gemini-2.5-flash")
	assertFeatureSupport(t, "openai", ProviderFeatureDictation, ProviderSupportNative, "stt.openai.gpt-4o-transcribe")
	assertFeatureSupport(t, "openai", ProviderFeatureRealtimeVoice, ProviderSupportNative, "realtime.openai.gpt-realtime-2")
	assertFeatureSupport(t, "google", ProviderFeatureRealtimeVoice, ProviderSupportNative, "realtime.google.gemini-native-audio")
	assertFeatureSupport(t, "assemblyai", ProviderFeatureSpeakerDiarization, ProviderSupportNative, "stt.assemblyai.universal-diarization")
	assertFeatureSupport(t, "groq", ProviderFeatureTTS, ProviderSupportUnsupported, "")
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

func assertFeatureSupport(t *testing.T, provider string, feature ProviderFeature, wantSupport ProviderSupportKind, wantProfile string) {
	t.Helper()
	row, ok := FindProviderMatrixRow(provider)
	if !ok {
		t.Fatalf("provider %q missing", provider)
	}
	support, ok := row.Feature(feature)
	if !ok {
		t.Fatalf("provider %q feature %q missing", provider, feature)
	}
	if support.Support != wantSupport {
		t.Fatalf("%s %s support = %q, want %q: %+v", provider, feature, support.Support, wantSupport, support)
	}
	if wantProfile != "" && support.ProfileID != wantProfile {
		t.Fatalf("%s %s profile = %q, want %q", provider, feature, support.ProfileID, wantProfile)
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
