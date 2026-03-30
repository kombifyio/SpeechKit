package models

import "testing"

func TestDefaultCatalogIsLocalFirst(t *testing.T) {
	catalog := DefaultCatalog()
	if len(catalog.Profiles) == 0 {
		t.Fatal("Profiles = 0, want non-empty catalog")
	}

	sttProfile, ok := catalog.DefaultProfile(ModalitySTT)
	if !ok {
		t.Fatal("default STT profile missing")
	}
	if sttProfile.ExecutionMode != ExecutionModeLocal {
		t.Fatalf("STT default execution = %q, want %q", sttProfile.ExecutionMode, ExecutionModeLocal)
	}
	if sttProfile.AllowInference {
		t.Fatal("STT default should not allow inference")
	}
	if sttProfile.ModelID != "Qwen/Qwen3-ASR-1.7B" {
		t.Fatalf("STT default model = %q", sttProfile.ModelID)
	}

	ttsProfile, ok := catalog.DefaultProfile(ModalityTTS)
	if !ok {
		t.Fatal("default TTS profile missing")
	}
	if ttsProfile.ModelID != "Qwen/Qwen3-TTS-12Hz-1.7B-VoiceDesign" {
		t.Fatalf("TTS default model = %q", ttsProfile.ModelID)
	}
	if ttsProfile.AllowInference {
		t.Fatal("TTS default should not allow inference")
	}

	utilityProfile, ok := catalog.DefaultProfile(ModalityUtility)
	if !ok {
		t.Fatal("default utility profile missing")
	}
	if utilityProfile.ExecutionMode != ExecutionModeOpenAI {
		t.Fatalf("utility default execution = %q, want %q", utilityProfile.ExecutionMode, ExecutionModeOpenAI)
	}

	agentProfile, ok := catalog.DefaultProfile(ModalityAgent)
	if !ok {
		t.Fatal("default agent profile missing")
	}
	if agentProfile.ExecutionMode != ExecutionModeOpenAI {
		t.Fatalf("agent default execution = %q, want %q", agentProfile.ExecutionMode, ExecutionModeOpenAI)
	}
	if agentProfile.ModelID != "gpt-5.4" {
		t.Fatalf("agent default model = %q", agentProfile.ModelID)
	}

	embeddingProfile, ok := catalog.DefaultProfile(ModalityEmbedding)
	if !ok {
		t.Fatal("default embedding profile missing")
	}
	if embeddingProfile.ExecutionMode != ExecutionModeGoogle {
		t.Fatalf("embedding default execution = %q, want %q", embeddingProfile.ExecutionMode, ExecutionModeGoogle)
	}
	if embeddingProfile.ModelID != "gemini-embedding-2" {
		t.Fatalf("embedding default model = %q", embeddingProfile.ModelID)
	}
	if !embeddingProfile.AllowInference {
		t.Fatal("embedding default should allow inference")
	}
}

func TestDefaultCatalogIncludesMultiProviderProfiles(t *testing.T) {
	catalog := DefaultCatalog()

	modeFound := map[ExecutionMode]bool{}
	for _, profile := range catalog.Profiles {
		modeFound[profile.ExecutionMode] = true
	}

	required := []ExecutionMode{
		ExecutionModeLocal,
		ExecutionModeHFRouted,
		ExecutionModeOpenAI,
		ExecutionModeGroq,
		ExecutionModeGoogle,
		ExecutionModeOllama,
	}
	for _, mode := range required {
		if !modeFound[mode] {
			t.Fatalf("expected at least one profile with execution mode %q", mode)
		}
	}
}

func TestDefaultCatalogHasNoHFEndpointProfiles(t *testing.T) {
	catalog := DefaultCatalog()
	for _, profile := range catalog.Profiles {
		if profile.ExecutionMode == "hf_endpoint" {
			t.Fatalf("catalog still contains HF endpoint profile %q; all endpoint profiles should be removed", profile.ID)
		}
	}
}
