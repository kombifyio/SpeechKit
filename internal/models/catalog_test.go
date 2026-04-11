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
	if sttProfile.ModelID != "whisper.cpp" {
		t.Fatalf("STT default model = %q", sttProfile.ModelID)
	}

	ttsProfile, ok := catalog.DefaultProfile(ModalityTTS)
	if !ok {
		t.Fatal("default TTS profile missing")
	}
	if ttsProfile.ModelID != "tts-1" {
		t.Fatalf("TTS default model = %q", ttsProfile.ModelID)
	}
	if !ttsProfile.AllowInference {
		t.Fatal("TTS default should allow inference")
	}

	utilityProfile, ok := catalog.DefaultProfile(ModalityUtility)
	if !ok {
		t.Fatal("default utility profile missing")
	}
	if utilityProfile.ExecutionMode != ExecutionModeOllama {
		t.Fatalf("utility default execution = %q, want %q", utilityProfile.ExecutionMode, ExecutionModeOllama)
	}

	assistProfile, ok := catalog.DefaultProfile(ModalityAssist)
	if !ok {
		t.Fatal("default assist profile missing")
	}
	if assistProfile.ExecutionMode != ExecutionModeOllama {
		t.Fatalf("assist default execution = %q, want %q", assistProfile.ExecutionMode, ExecutionModeOllama)
	}
	if assistProfile.ModelID != "gemma4:e4b" {
		t.Fatalf("assist default model = %q", assistProfile.ModelID)
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

func TestDefaultCatalogIncludesGemma4LocalProfiles(t *testing.T) {
	catalog := DefaultCatalog()

	// After catalog reduction: only e4b variants remain; e2b and 26b were removed.
	required := map[string]string{
		"utility.ollama.gemma4-e4b": "gemma4:e4b",
		"assist.ollama.gemma4-e4b":  "gemma4:e4b",
	}

	found := map[string]string{}
	for _, profile := range catalog.Profiles {
		if _, ok := required[profile.ID]; ok {
			found[profile.ID] = profile.ModelID
		}
	}

	for id, wantModel := range required {
		if got, ok := found[id]; !ok {
			t.Fatalf("missing profile %q", id)
		} else if got != wantModel {
			t.Fatalf("profile %q model = %q, want %q", id, got, wantModel)
		}
	}
}

func TestDefaultCatalogUsesMinimalSwitcherChoices(t *testing.T) {
	catalog := DefaultCatalog()

	gotByModality := map[Modality][]Profile{}
	for _, profile := range catalog.Profiles {
		switch profile.Modality {
		case ModalitySTT, ModalityUtility, ModalityAssist, ModalityRealtimeVoice:
			gotByModality[profile.Modality] = append(gotByModality[profile.Modality], profile)
		}
	}

	if got := len(gotByModality[ModalitySTT]); got != 3 {
		t.Fatalf("stt profile count = %d, want 3", got)
	}
	if got := len(gotByModality[ModalityUtility]); got != 3 {
		t.Fatalf("utility profile count = %d, want 3", got)
	}
	if got := len(gotByModality[ModalityAssist]); got != 3 {
		t.Fatalf("assist profile count = %d, want 3", got)
	}
	if got := len(gotByModality[ModalityRealtimeVoice]); got != 1 {
		t.Fatalf("realtime voice profile count = %d, want 1", got)
	}

	realtimeProfile, ok := catalog.DefaultProfile(ModalityRealtimeVoice)
	if !ok {
		t.Fatal("default realtime voice profile missing")
	}
	if realtimeProfile.ExecutionMode != ExecutionModeGoogle {
		t.Fatalf("realtime voice execution = %q, want %q", realtimeProfile.ExecutionMode, ExecutionModeGoogle)
	}
	if realtimeProfile.ModelID != "gemini-2.5-flash-native-audio-preview-12-2025" {
		t.Fatalf("realtime voice model = %q", realtimeProfile.ModelID)
	}
}
