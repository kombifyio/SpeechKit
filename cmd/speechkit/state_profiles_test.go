package main

import (
	"testing"

	"github.com/kombifyio/SpeechKit/internal/models"
)

func TestActiveProfilesFromConfigReflectsConfiguredProviders(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Local.Enabled = true
	cfg.Routing.Strategy = "dynamic"
	cfg.Providers.Ollama.Enabled = true
	cfg.Providers.Ollama.UtilityModel = "gemma4:e4b"
	cfg.Providers.Ollama.AgentModel = "gemma4:e4b"

	profiles := activeProfilesFromConfig(cfg, filteredModelCatalog())

	if got := profiles[string(models.ModalitySTT)]; got != "stt.local.whispercpp" {
		t.Fatalf("active stt profile = %q, want %q", got, "stt.local.whispercpp")
	}
	if got := profiles[string(models.ModalityUtility)]; got != "utility.ollama.gemma4-e4b" {
		t.Fatalf("active utility profile = %q, want %q", got, "utility.ollama.gemma4-e4b")
	}
	if got := profiles[string(models.ModalityAssist)]; got != "assist.ollama.gemma4-e4b" {
		t.Fatalf("active assist profile = %q, want %q", got, "assist.ollama.gemma4-e4b")
	}
}
