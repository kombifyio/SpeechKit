package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := defaults()
	cfg.General.Hotkey = "ctrl+shift"
	cfg.HuggingFace.Enabled = true
	cfg.HuggingFace.Model = "openai/whisper-large-v3-turbo"
	cfg.UI.OverlayEnabled = false
	cfg.UI.Visualizer = "circle"
	cfg.UI.Design = "kombify"

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.General.Hotkey != "ctrl+shift" {
		t.Fatalf("hotkey = %q", reloaded.General.Hotkey)
	}
	if reloaded.HuggingFace.Model != "openai/whisper-large-v3-turbo" {
		t.Fatalf("model = %q", reloaded.HuggingFace.Model)
	}
	if reloaded.UI.OverlayEnabled {
		t.Fatal("overlay should round-trip as disabled")
	}
	if reloaded.UI.Visualizer != "circle" {
		t.Fatalf("visualizer = %q", reloaded.UI.Visualizer)
	}
	if reloaded.UI.Design != "kombify" {
		t.Fatalf("design = %q", reloaded.UI.Design)
	}
}

func TestSaveRoundTripAssistModels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := defaults()
	cfg.Providers.OpenAI.AssistModel = "gpt-5.4-2026-03-05"
	cfg.Providers.Google.AssistModel = "gemini-2.5-flash"
	cfg.Providers.Ollama.AssistModel = "gemma4:e4b"
	cfg.LocalLLM.AssistModel = "gemma4:e4b"
	cfg.HuggingFace.AssistModel = "Qwen/Qwen3.5-27B"

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := reloaded.Providers.OpenAI.AssistModel, cfg.Providers.OpenAI.AssistModel; got != want {
		t.Fatalf("openai assist model = %q, want %q", got, want)
	}
	if got, want := reloaded.Providers.Google.AssistModel, cfg.Providers.Google.AssistModel; got != want {
		t.Fatalf("google assist model = %q, want %q", got, want)
	}
	if got, want := reloaded.Providers.Ollama.AssistModel, cfg.Providers.Ollama.AssistModel; got != want {
		t.Fatalf("ollama assist model = %q, want %q", got, want)
	}
	if got, want := reloaded.LocalLLM.AssistModel, cfg.LocalLLM.AssistModel; got != want {
		t.Fatalf("local LLM assist model = %q, want %q", got, want)
	}
	if got, want := reloaded.HuggingFace.AssistModel, cfg.HuggingFace.AssistModel; got != want {
		t.Fatalf("huggingface assist model = %q, want %q", got, want)
	}
}

func TestLoadShortcutLocaleAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[shortcuts.locale.de]
leading_fillers = ["bitte", "hey speechkit"]
summarize = ["kurzfassung", "briefing"]
copy_last = ["kopier den letzten block"]

[shortcuts.locale.en]
summarize = ["brief me"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	de, ok := cfg.Shortcuts.Locale["de"]
	if !ok {
		t.Fatal("expected shortcuts.locale.de to be loaded")
	}
	if got, want := len(de.LeadingFillers), 2; got != want {
		t.Fatalf("len(leading_fillers) = %d, want %d", got, want)
	}
	if got, want := de.Summarize[0], "kurzfassung"; got != want {
		t.Fatalf("de summarize[0] = %q, want %q", got, want)
	}
	if got, want := de.CopyLast[0], "kopier den letzten block"; got != want {
		t.Fatalf("de copy_last[0] = %q, want %q", got, want)
	}

	en, ok := cfg.Shortcuts.Locale["en"]
	if !ok {
		t.Fatal("expected shortcuts.locale.en to be loaded")
	}
	if got, want := en.Summarize[0], "brief me"; got != want {
		t.Fatalf("en summarize[0] = %q, want %q", got, want)
	}
}

func TestSaveRoundTripShortcutLocaleAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := defaults()
	cfg.Shortcuts.Locale = map[string]ShortcutLocaleConfig{
		"de": {
			LeadingFillers: []string{"bitte"},
			Summarize:      []string{"kurzfassung"},
			CopyLast:       []string{"kopier den letzten block"},
			InsertLast:     []string{"setz das ein"},
			QuickNote:      []string{"merkzettel"},
		},
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	de, ok := reloaded.Shortcuts.Locale["de"]
	if !ok {
		t.Fatal("expected shortcuts.locale.de after round-trip")
	}
	if got, want := de.Summarize[0], "kurzfassung"; got != want {
		t.Fatalf("de summarize[0] = %q, want %q", got, want)
	}
	if got, want := de.QuickNote[0], "merkzettel"; got != want {
		t.Fatalf("de quick_note[0] = %q, want %q", got, want)
	}
}

func TestApplyManagedIntegrationDefaultsNoopWhenHFAlreadyEnabled(t *testing.T) {
	useMemorySecretStoreForTest(t)
	restoreBuild := OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaults()
	cfg.HuggingFace.Enabled = true
	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")
	t.Setenv("HF_TOKEN", "test-token")

	changed := ApplyManagedIntegrationDefaults(cfg)

	if changed {
		t.Fatal("managed defaults should not change config when HF is already enabled")
	}
	if !cfg.HuggingFace.Enabled {
		t.Fatal("huggingface should remain enabled")
	}
}

func TestApplyManagedIntegrationDefaultsEnablesHFWhenExplicitlyDisabled(t *testing.T) {
	useMemorySecretStoreForTest(t)
	restoreBuild := OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaults()
	cfg.Local.Enabled = false
	cfg.Routing.Strategy = "cloud-only"
	cfg.HuggingFace.Enabled = false
	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")
	t.Setenv("HF_TOKEN", "test-token")

	changed := ApplyManagedIntegrationDefaults(cfg)

	if !changed {
		t.Fatal("expected managed defaults to enable huggingface when explicitly disabled")
	}
	if !cfg.HuggingFace.Enabled {
		t.Fatal("huggingface should be enabled")
	}
}

func TestApplyManagedIntegrationDefaultsDoesNotOverrideExplicitProviderConfig(t *testing.T) {
	useMemorySecretStoreForTest(t)
	restoreBuild := OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaults()
	cfg.HuggingFace.Enabled = false
	cfg.Local.Enabled = true
	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")
	t.Setenv("HF_TOKEN", "test-token")

	changed := ApplyManagedIntegrationDefaults(cfg)

	if changed {
		t.Fatal("managed defaults should not override explicit local provider setup")
	}
	if cfg.HuggingFace.Enabled {
		t.Fatal("huggingface should stay disabled")
	}
}
