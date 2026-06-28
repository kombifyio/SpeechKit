package stt

import (
	"testing"

	"github.com/kombifyio/SpeechKit/internal/models"
)

func TestBuildReturnsProviderPerExecutionMode(t *testing.T) {
	cases := []struct {
		mode     models.ExecutionMode
		wantName string
	}{
		{models.ExecutionModeHFRouted, "huggingface"},
		{models.ExecutionModeOpenAI, "openai"},
		{models.ExecutionModeGroq, "groq"},
		{models.ExecutionModeGoogle, "google"},
		{models.ExecutionModeDeepgram, "deepgram"},
		{models.ExecutionModeAssemblyAI, "assemblyai"},
		{models.ExecutionModeOpenRouter, "openrouter"},
		{models.ExecutionModeOllama, "ollama"},
	}

	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			name, provider, err := Build(BuildSpec{
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
	name, provider, err := Build(BuildSpec{
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
		name        string
		providerID  string
		modelID     string
		wantName    string
		wantBaseURL string
	}{
		{
			name:        "openai current stt profile",
			providerID:  "stt.openai.gpt-4o-transcribe",
			modelID:     "gpt-4o-mini-transcribe",
			wantName:    "openai",
			wantBaseURL: "https://api.openai.com",
		},
		{
			name:        "groq turbo profile",
			providerID:  "stt.groq.whisper-large-v3-turbo",
			modelID:     "whisper-large-v3",
			wantName:    "groq",
			wantBaseURL: "https://api.groq.com/openai",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, provider, err := Build(BuildSpec{
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
			openaiCompat, ok := provider.(*OpenAICompatibleProvider)
			if !ok {
				t.Fatalf("Build(provider=%s) returned %T, want *OpenAICompatibleProvider", tc.providerID, provider)
			}
			if openaiCompat.Name() != tc.wantName {
				t.Fatalf("provider.Name() = %q, want %q", openaiCompat.Name(), tc.wantName)
			}
			if openaiCompat.Model != tc.modelID {
				t.Fatalf("provider model = %q, want %q", openaiCompat.Model, tc.modelID)
			}
			if openaiCompat.BaseURL != tc.wantBaseURL {
				t.Fatalf("provider base URL = %q, want %q", openaiCompat.BaseURL, tc.wantBaseURL)
			}
		})
	}
}

func TestBuildGoogleForwardsStreamingCredentialEnvNames(t *testing.T) {
	_, provider, err := Build(BuildSpec{
		Provider:                        "stt.google.latest-long",
		ModelID:                         "latest_long",
		APIKey:                          "key",
		GoogleStreamingCredentialsEnv:   "CUSTOM_GOOGLE_STT_JSON",
		GoogleApplicationCredentialsEnv: "CUSTOM_GOOGLE_APPLICATION_CREDENTIALS",
	})
	if err != nil {
		t.Fatalf("Build(google) returned error: %v", err)
	}
	google, ok := provider.(*GoogleSTTProvider)
	if !ok {
		t.Fatalf("Build(google) returned %T, want *GoogleSTTProvider", provider)
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
	_, _, err := Build(BuildSpec{ExecutionMode: models.ExecutionModeLocal, ModelID: "whisper"})
	if err == nil {
		t.Fatal("Build(local) = nil error, want unsupported-mode error")
	}
}

func TestBuildUnsupportedModeErrors(t *testing.T) {
	_, provider, err := Build(BuildSpec{ExecutionMode: models.ExecutionMode("nonsense")})
	if err == nil {
		t.Fatal("Build(nonsense) = nil error, want error")
	}
	if provider != nil {
		t.Errorf("Build(nonsense) provider = %v, want nil", provider)
	}
}

func TestBuildDeepgramDiarizationOverride(t *testing.T) {
	_, provider, err := Build(BuildSpec{
		ExecutionMode:    models.ExecutionModeDeepgram,
		ModelID:          "nova-3",
		APIKey:           "key",
		DiarizationModel: "custom-diar",
	})
	if err != nil {
		t.Fatalf("Build(deepgram) error: %v", err)
	}
	dg, ok := provider.(*DeepgramProvider)
	if !ok {
		t.Fatalf("Build(deepgram) returned %T, want *DeepgramProvider", provider)
	}
	if dg.DiarizationModel != "custom-diar" {
		t.Errorf("DiarizationModel = %q, want %q", dg.DiarizationModel, "custom-diar")
	}
}

func TestBuildDeepgramOptions(t *testing.T) {
	_, provider, err := Build(BuildSpec{
		ExecutionMode: models.ExecutionModeDeepgram,
		ModelID:       "nova-3",
		APIKey:        "key",
		Deepgram: DeepgramOptions{
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
	dg, ok := provider.(*DeepgramProvider)
	if !ok {
		t.Fatalf("Build(deepgram) returned %T, want *DeepgramProvider", provider)
	}
	if dg.SmartFormat || dg.UseVocabularyKeyterms {
		t.Errorf("Deepgram boolean options = smart:%v keyterms:%v, want false/false", dg.SmartFormat, dg.UseVocabularyKeyterms)
	}
	if dg.LanguageOverride != "multi" || dg.EndpointingMs != 100 {
		t.Errorf("Deepgram options = language:%q endpointing:%d", dg.LanguageOverride, dg.EndpointingMs)
	}
}

func TestBuildOllamaDefaultsBaseURL(t *testing.T) {
	_, provider, err := Build(BuildSpec{
		ExecutionMode: models.ExecutionModeOllama,
		ModelID:       "gemma",
	})
	if err != nil {
		t.Fatalf("Build(ollama) error: %v", err)
	}
	if provider == nil || provider.Name() != "ollama" {
		t.Fatalf("Build(ollama) returned unexpected provider %v", provider)
	}
}
