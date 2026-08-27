package stt_test

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

func TestBuildReturnsProviderPerExecutionMode(t *testing.T) {
	cases := []struct {
		mode     speechkit.ExecutionMode
		wantName string
	}{
		{speechkit.ExecutionModeHFRouted, "huggingface"},
		{speechkit.ExecutionModeOpenAI, "openai"},
		{speechkit.ExecutionModeGroq, "groq"},
		{speechkit.ExecutionModeGoogle, "google"},
		{speechkit.ExecutionModeDeepgram, "deepgram"},
		{speechkit.ExecutionModeAssemblyAI, "assemblyai"},
		{speechkit.ExecutionModeOpenRouter, "openrouter"},
		{speechkit.ExecutionModeOllama, "ollama"},
	}

	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			name, provider, err := stt.Build(stt.BuildSpec{
				ExecutionMode: tc.mode,
				ModelID:       "test-model",
				APIKey:        "key",
				Token:         "token",
			})
			if err != nil {
				t.Fatalf("Build(%s) returned error: %v", tc.mode, err)
			}
			if name != tc.wantName {
				t.Errorf("Build(%s) name = %q, want %q", tc.mode, name, tc.wantName)
			}
			if provider == nil {
				t.Fatalf("Build(%s) returned nil provider", tc.mode)
			}
			// The registry name must agree with the provider's own identity.
			if provider.Name() != tc.wantName {
				t.Errorf("Build(%s) provider.Name() = %q, want %q", tc.mode, provider.Name(), tc.wantName)
			}
		})
	}
}

func TestBuildAcceptsCanonicalProviderID(t *testing.T) {
	name, provider, err := stt.Build(stt.BuildSpec{
		Provider: "deepgram",
		ModelID:  "nova-3",
		APIKey:   "key",
	})
	if err != nil {
		t.Fatalf("Build(provider=deepgram) returned error: %v", err)
	}
	if name != "deepgram" {
		t.Fatalf("Build(provider=deepgram) name = %q, want deepgram", name)
	}
	if provider == nil || provider.Name() != "deepgram" {
		t.Fatalf("Build(provider=deepgram) provider = %v", provider)
	}
}

func TestBuildAcceptsProfileIDsAsProviderSelectors(t *testing.T) {
	cases := []struct {
		name       string
		providerID string
		modelID    string
		wantName   string
	}{
		{
			name:       "openai current stt profile",
			providerID: "stt.openai.gpt-4o-transcribe",
			modelID:    "gpt-4o-mini-transcribe",
			wantName:   "openai",
		},
		{
			name:       "groq turbo profile",
			providerID: "stt.groq.whisper-large-v3-turbo",
			modelID:    "whisper-large-v3",
			wantName:   "groq",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, provider, err := stt.Build(stt.BuildSpec{
				Provider: tc.providerID,
				ModelID:  tc.modelID,
				APIKey:   "key",
			})
			if err != nil {
				t.Fatalf("Build(provider=%s) returned error: %v", tc.providerID, err)
			}
			if name != tc.wantName {
				t.Fatalf("Build(provider=%s) name = %q, want %q", tc.providerID, name, tc.wantName)
			}
			if provider == nil || provider.Name() != tc.wantName {
				t.Fatalf("provider identity mismatch for %s", tc.providerID)
			}
		})
	}
}

func TestBuildGoogleForwardsStreamingCredentialEnvNames(t *testing.T) {
	_, provider, err := stt.Build(stt.BuildSpec{
		Provider:                        "stt.google.latest-long",
		ModelID:                         "latest_long",
		APIKey:                          "key",
		GoogleStreamingCredentialsEnv:   "CUSTOM_GOOGLE_STT_JSON",
		GoogleApplicationCredentialsEnv: "CUSTOM_GOOGLE_APPLICATION_CREDENTIALS",
	})
	if err != nil {
		t.Fatalf("Build(google) returned error: %v", err)
	}
	google, ok := provider.(*stt.GoogleSTTProvider)
	if !ok {
		t.Fatalf("Build(google) returned %T, want *stt.GoogleSTTProvider", provider)
	}
	if google.Model != "latest_long" {
		t.Fatalf("google model = %q, want latest_long", google.Model)
	}
	if google.STTCredentialsJSONEnv != "CUSTOM_GOOGLE_STT_JSON" {
		t.Fatalf("google STT credential env = %q", google.STTCredentialsJSONEnv)
	}
	if google.ApplicationCredentialsEnv != "CUSTOM_GOOGLE_APPLICATION_CREDENTIALS" {
		t.Fatalf("google application credentials env = %q", google.ApplicationCredentialsEnv)
	}
}

func TestBuildLocalIsHostManaged(t *testing.T) {
	// ExecutionModeLocal is owned by the host (whisper.cpp subprocess) and is
	// intentionally not constructed by the registry.
	_, _, err := stt.Build(stt.BuildSpec{ExecutionMode: speechkit.ExecutionModeLocal, ModelID: "whisper"})
	if err == nil {
		t.Fatal("Build(local) = nil error, want unsupported-mode error")
	}
}

func TestBuildUnsupportedModeErrors(t *testing.T) {
	_, provider, err := stt.Build(stt.BuildSpec{ExecutionMode: speechkit.ExecutionMode("nonsense")})
	if err == nil {
		t.Fatal("Build(nonsense) = nil error, want error")
	}
	if provider != nil {
		t.Errorf("Build(nonsense) provider = %v, want nil", provider)
	}
}

func TestBuildDeepgramDiarizationOverride(t *testing.T) {
	_, provider, err := stt.Build(stt.BuildSpec{
		ExecutionMode:    speechkit.ExecutionModeDeepgram,
		ModelID:          "nova-3",
		APIKey:           "key",
		DiarizationModel: "custom-diar",
	})
	if err != nil {
		t.Fatalf("Build(deepgram) error: %v", err)
	}
	dg, ok := provider.(*stt.DeepgramProvider)
	if !ok {
		t.Fatalf("Build(deepgram) returned %T, want *stt.DeepgramProvider", provider)
	}
	if dg.DiarizationModel != "custom-diar" {
		t.Errorf("DiarizationModel = %q, want %q", dg.DiarizationModel, "custom-diar")
	}
}

func TestBuildDeepgramOptions(t *testing.T) {
	_, provider, err := stt.Build(stt.BuildSpec{
		ExecutionMode: speechkit.ExecutionModeDeepgram,
		ModelID:       "nova-3",
		APIKey:        "key",
		Deepgram: stt.DeepgramOptions{
			Configured:            true,
			SmartFormat:           false,
			UseVocabularyKeyterms: false,
			LanguageOverride:      "multi",
			EndpointingMs:         100,
		},
	})
	if err != nil {
		t.Fatalf("Build(deepgram) error: %v", err)
	}
	dg, ok := provider.(*stt.DeepgramProvider)
	if !ok {
		t.Fatalf("Build(deepgram) returned %T, want *stt.DeepgramProvider", provider)
	}
	if dg.SmartFormat || dg.UseVocabularyKeyterms {
		t.Errorf("Deepgram boolean options = smart:%v keyterms:%v, want false/false", dg.SmartFormat, dg.UseVocabularyKeyterms)
	}
	if dg.LanguageOverride != "multi" || dg.EndpointingMs != 100 {
		t.Errorf("Deepgram options = language:%q endpointing:%d", dg.LanguageOverride, dg.EndpointingMs)
	}
}

func TestBuildOllamaDefaultsBaseURL(t *testing.T) {
	_, provider, err := stt.Build(stt.BuildSpec{
		ExecutionMode: speechkit.ExecutionModeOllama,
		ModelID:       "gemma",
	})
	if err != nil {
		t.Fatalf("Build(ollama) error: %v", err)
	}
	if provider == nil || provider.Name() != "ollama" {
		t.Fatalf("Build(ollama) returned unexpected provider %v", provider)
	}
}

// fakeExtraProvider is a minimal STTProvider for BuildRouter/Register tests.
type fakeExtraProvider struct {
	name string
}

func (f *fakeExtraProvider) Transcribe(_ context.Context, _ []byte, _ stt.TranscribeOpts) (*stt.Result, error) {
	return &stt.Result{Text: "fake transcript", Provider: f.name}, nil
}
func (f *fakeExtraProvider) Name() string                   { return f.name }
func (f *fakeExtraProvider) Health(_ context.Context) error { return nil }

func TestBuildRouterNothingEnabledReturnsNotOK(t *testing.T) {
	router, ok, _ := stt.BuildRouter(stt.RouterConfig{Strategy: stt.StrategyDynamic}, stt.EnabledProviders{})
	if ok {
		t.Fatal("BuildRouter with nothing enabled: ok = true, want false")
	}
	if router != nil {
		t.Fatal("BuildRouter with nothing enabled: router != nil, want nil")
	}
}

func TestBuildRouterRoutesToExtraProvider(t *testing.T) {
	fake := &fakeExtraProvider{name: "faketest"}
	router, ok, _ := stt.BuildRouter(
		stt.RouterConfig{Strategy: stt.StrategyCloudOnly},
		stt.EnabledProviders{Extra: []stt.STTProvider{fake}},
	)
	if !ok {
		t.Fatal("BuildRouter with one Extra provider: ok = false, want true")
	}
	if router == nil {
		t.Fatal("BuildRouter with one Extra provider: router = nil")
	}

	res, err := router.Route(context.Background(), []byte("pcm"), 1.0, stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if res.Provider != "faketest" {
		t.Fatalf("Route provider = %q, want faketest", res.Provider)
	}

	providers := router.Providers()
	if len(providers) != 1 || providers[0].Name() != "faketest" {
		t.Fatalf("Providers() = %v, want the single extra provider", providers)
	}
}

func TestRegisterDuplicateIDErrors(t *testing.T) {
	build := func(spec stt.BuildSpec) (stt.STTProvider, error) {
		return &fakeExtraProvider{name: "registertest"}, nil
	}
	if err := stt.Register("registertest-dup", "registertest-dup", build); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := stt.Register("registertest-dup", "registertest-dup", build); err == nil {
		t.Fatal("second Register with the same id: err = nil, want duplicate-id error")
	}
	if err := stt.Register("deepgram", "deepgram", build); err == nil {
		t.Fatal("Register over a built-in id: err = nil, want duplicate-id error")
	}
}

func TestRegisteredProviderIsConstructibleViaBuild(t *testing.T) {
	var gotSpec stt.BuildSpec
	if err := stt.Register("registertest-build", "registertest-build", func(spec stt.BuildSpec) (stt.STTProvider, error) {
		gotSpec = spec
		return &fakeExtraProvider{name: "registertest-build"}, nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	name, provider, err := stt.Build(stt.BuildSpec{Provider: "registertest-build", ModelID: "m1", APIKey: "k"})
	if err != nil {
		t.Fatalf("Build(registered): %v", err)
	}
	if name != "registertest-build" {
		t.Fatalf("Build(registered) name = %q", name)
	}
	if provider == nil || provider.Name() != "registertest-build" {
		t.Fatalf("Build(registered) provider = %v", provider)
	}
	if gotSpec.ModelID != "m1" || gotSpec.APIKey != "k" {
		t.Fatalf("registered builder received spec %+v, want ModelID=m1 APIKey=k", gotSpec)
	}
}

func TestRegisterRejectsInvalidInput(t *testing.T) {
	if err := stt.Register("", "empty", func(stt.BuildSpec) (stt.STTProvider, error) { return nil, nil }); err == nil {
		t.Fatal("Register with empty id: err = nil, want error")
	}
	if err := stt.Register("registertest-nilbuild", "x", nil); err == nil {
		t.Fatal("Register with nil build func: err = nil, want error")
	}
}
