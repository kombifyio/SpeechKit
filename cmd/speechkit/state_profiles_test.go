package main

import (
	"testing"

	"github.com/kombifyio/SpeechKit/internal/models"
)

func TestActiveProfilesFromConfigReflectsConfiguredProviders(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Local.Enabled = true
	cfg.Routing.Strategy = "dynamic"
	cfg.LocalLLM.Enabled = true
	cfg.LocalLLM.UtilityModel = "gemma4:e4b"
	cfg.LocalLLM.AssistModel = "gemma4:e4b"

	profiles := activeProfilesFromConfig(cfg, filteredModelCatalog())

	if got := profiles[string(models.ModalitySTT)]; got != "stt.local.whispercpp" {
		t.Fatalf("active stt profile = %q, want %q", got, "stt.local.whispercpp")
	}
	if got := profiles[string(models.ModalityUtility)]; got != "utility.builtin.gemma4-e4b" {
		t.Fatalf("active utility profile = %q, want %q", got, "utility.builtin.gemma4-e4b")
	}
	if got := profiles[string(models.ModalityAssist)]; got != "assist.builtin.gemma4-e4b" {
		t.Fatalf("active assist profile = %q, want %q", got, "assist.builtin.gemma4-e4b")
	}
}

func TestActiveProfilesFromConfigReflectsOllamaProvider(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Providers.Ollama.Enabled = true
	cfg.Providers.Ollama.AssistModel = "gemma4:e4b"

	profiles := activeProfilesFromConfig(cfg, filteredModelCatalog())

	if got := profiles[string(models.ModalityAssist)]; got != "assist.ollama.gemma4-e4b" {
		t.Fatalf("active assist profile = %q, want %q", got, "assist.ollama.gemma4-e4b")
	}
}

func TestActiveProfilesFromConfigPrefersLocalSTTWhenDynamicCloudAlsoMatches(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Local.Enabled = true
	cfg.Local.Model = "ggml-small.bin"
	cfg.Routing.Strategy = "dynamic"
	cfg.Providers.OpenAI.Enabled = true
	cfg.Providers.OpenAI.APIKeyEnv = "SPEECHKIT_TEST_OPENAI_KEY"
	cfg.Providers.OpenAI.STTModel = "whisper-1"
	t.Setenv("SPEECHKIT_TEST_OPENAI_KEY", "test-key")

	profiles := activeProfilesFromConfig(cfg, filteredModelCatalog())

	if got := profiles[string(models.ModalitySTT)]; got != "stt.local.whispercpp" {
		t.Fatalf("active stt profile = %q, want %q", got, "stt.local.whispercpp")
	}
}

func TestActiveProfilesFromConfigUsesCloudSTTWhenRoutingCloudOnly(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Local.Enabled = true
	cfg.Local.Model = "ggml-small.bin"
	cfg.Routing.Strategy = "cloud-only"
	cfg.Providers.OpenAI.Enabled = true
	cfg.Providers.OpenAI.APIKeyEnv = "SPEECHKIT_TEST_OPENAI_KEY"
	cfg.Providers.OpenAI.STTModel = "whisper-1"
	t.Setenv("SPEECHKIT_TEST_OPENAI_KEY", "test-key")

	profiles := activeProfilesFromConfig(cfg, filteredModelCatalog())

	if got := profiles[string(models.ModalitySTT)]; got != "stt.openai.whisper-1" {
		t.Fatalf("active stt profile = %q, want %q", got, "stt.openai.whisper-1")
	}
}
