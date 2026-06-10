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
