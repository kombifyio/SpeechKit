package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

func TestBuildShortcutResolverUsesConfiguredAliases(t *testing.T) {
	cfg := &config.Config{
		Shortcuts: config.ShortcutsConfig{
			Locale: map[string]config.ShortcutLocaleConfig{
				"de": {
					LeadingFillers: []string{"bitte"},
					Summarize:      []string{"kurzfassung"},
				},
			},
		},
	}

	resolution := buildShortcutResolver(cfg).Resolve("Bitte Kurzfassung in drei Punkten", "de-DE")

	if got, want := resolution.Intent, shortcuts.IntentSummarize; got != want {
		t.Fatalf("Intent = %q, want %q", got, want)
	}
	if got, want := resolution.Payload, "in drei punkten"; got != want {
		t.Fatalf("Payload = %q, want %q", got, want)
	}
}

func TestBuildShortcutResolverKeepsDefaultCatalog(t *testing.T) {
	cfg := &config.Config{}

	resolution := buildShortcutResolver(cfg).Resolve("summarize this in bullets", "en")

	if got, want := resolution.Intent, shortcuts.IntentSummarize; got != want {
		t.Fatalf("Intent = %q, want %q", got, want)
	}
	if got, want := resolution.Payload, "in bullets"; got != want {
		t.Fatalf("Payload = %q, want %q", got, want)
	}
}

func TestBuildGenkitConfigSkipsSelectedBuiltInLocalLLMWithoutModelFile(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.LocalLLM.Enabled = false
	cfg.LocalLLM.BaseURL = ""
	cfg.ModelSelection.Assist.PrimaryProfileID = "assist.builtin.gemma4-e4b"

	aiCfg := buildGenkitConfig(cfg)

	if got := aiCfg.LocalLLMBaseURL; got != "" {
		t.Fatalf("local LLM base URL = %q, want empty while model file is missing", got)
	}
	if aiCfg.UseOrderedAssistModels {
		t.Fatal("ordered assist models should stay disabled while the selected local model is missing")
	}
	if got := len(aiCfg.OrderedAssistModels); got != 0 {
		t.Fatalf("ordered assist models = %d, want 0", got)
	}
}

func TestBuildGenkitConfigRegistersSelectedBuiltInLocalLLMWithModelFile(t *testing.T) {
	modelPath := filepath.Join(t.TempDir(), "gemma-3-4b-it-Q4_K_M.gguf")
	if err := os.WriteFile(modelPath, []byte("gguf"), 0o600); err != nil {
		t.Fatalf("write local llm model: %v", err)
	}

	cfg := defaultTestConfig()
	cfg.LocalLLM.Enabled = false
	cfg.LocalLLM.BaseURL = ""
	cfg.LocalLLM.ModelPath = modelPath
	cfg.LocalLLM.AssistModel = filepath.Base(modelPath)
	cfg.ModelSelection.Assist.PrimaryProfileID = "assist.builtin.gemma4-e4b"

	aiCfg := buildGenkitConfig(cfg)

	if got, want := aiCfg.LocalLLMBaseURL, config.DefaultLocalLLMBaseURL; got != want {
		t.Fatalf("local LLM base URL = %q, want %q", got, want)
	}
	if !aiCfg.UseOrderedAssistModels {
		t.Fatal("expected ordered assist models to be enabled")
	}
	if len(aiCfg.OrderedAssistModels) != 1 {
		t.Fatalf("ordered assist models = %d, want 1", len(aiCfg.OrderedAssistModels))
	}
	if got, want := aiCfg.OrderedAssistModels[0].Provider, "local"; got != want {
		t.Fatalf("ordered provider = %q, want %q", got, want)
	}
	if got, want := aiCfg.OrderedAssistModels[0].Model, filepath.Base(modelPath); got != want {
		t.Fatalf("ordered model = %q, want %q", got, want)
	}
}

func TestBuildGenkitConfigRegistersSelectedOllamaProvider(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Providers.Ollama.Enabled = false
	cfg.Providers.Ollama.BaseURL = ""
	cfg.ModelSelection.Assist.PrimaryProfileID = "assist.ollama.gemma4-e4b"

	aiCfg := buildGenkitConfig(cfg)

	if got, want := aiCfg.OllamaBaseURL, "http://localhost:11434"; got != want {
		t.Fatalf("ollama base URL = %q, want %q", got, want)
	}
	if !aiCfg.UseOrderedAssistModels {
		t.Fatal("expected ordered assist models to be enabled")
	}
	if len(aiCfg.OrderedAssistModels) != 1 {
		t.Fatalf("ordered assist models = %d, want 1", len(aiCfg.OrderedAssistModels))
	}
	if got, want := aiCfg.OrderedAssistModels[0].Provider, "ollama"; got != want {
		t.Fatalf("ordered provider = %q, want %q", got, want)
	}
	if got, want := aiCfg.OrderedAssistModels[0].Model, "gemma4:e4b"; got != want {
		t.Fatalf("ordered model = %q, want %q", got, want)
	}
}
