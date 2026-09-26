package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPerformanceConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`
[performance]
process_priority = "normal"
subprocess_priority = "normal"
capture_thread_priority = "highest"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Performance.ProcessPriority != "normal" {
		t.Fatalf("ProcessPriority = %q, want normal", cfg.Performance.ProcessPriority)
	}
	if cfg.Performance.SubprocessPriority != "normal" {
		t.Fatalf("SubprocessPriority = %q, want normal", cfg.Performance.SubprocessPriority)
	}
	if cfg.Performance.CaptureThreadPriority != "highest" {
		t.Fatalf("CaptureThreadPriority = %q, want highest", cfg.Performance.CaptureThreadPriority)
	}

	// Absent block: all fields empty — consumers treat empty as the
	// protective default (above_normal/below_normal/realtime).
	defaults, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("Load defaults: %v", err)
	}
	if defaults.Performance != (PerformanceConfig{}) {
		t.Fatalf("default Performance = %+v, want zero value", defaults.Performance)
	}
}

func TestDefaultLocalSTTModelIsBundledStarterModel(t *testing.T) {
	if DefaultLocalSTTModel != "ggml-small.bin" {
		t.Fatalf("DefaultLocalSTTModel = %q, want bundled starter model ggml-small.bin", DefaultLocalSTTModel)
	}
}

func TestGoogleProviderRegionDefaultAndOverride(t *testing.T) {
	t.Run("defaults to europe-west3 when absent from TOML", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		body := `[providers.google]
api_key_env = "GOOGLE_AI_API_KEY"
`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write toml: %v", err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Providers.Google.Region != "europe-west3" {
			t.Errorf("region = %q, want europe-west3 when not set in TOML", cfg.Providers.Google.Region)
		}
	})

	t.Run("respects explicit override in TOML", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		body := `[providers.google]
api_key_env = "GOOGLE_AI_API_KEY"
region = "us-central1"
`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write toml: %v", err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Providers.Google.Region != "us-central1" {
			t.Errorf("region = %q, want us-central1", cfg.Providers.Google.Region)
		}
	})

	t.Run("no TOML file uses europe-west3 from defaults", func(t *testing.T) {
		cfg, err := Load("/nonexistent/no-config.toml")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Providers.Google.Region != "europe-west3" {
			t.Errorf("region = %q, want europe-west3", cfg.Providers.Google.Region)
		}
	})
}

func TestApplyLocalInstallDefaultsBackfillsBuiltInPrimaryModels(t *testing.T) {
	cfg := &Config{}
	changed := ApplyLocalInstallDefaults(cfg, &InstallState{Mode: InstallModeLocal})

	if !changed {
		t.Fatal("ApplyLocalInstallDefaults should report changed when model defaults are missing")
	}
	if got, want := cfg.ModelSelection.Dictate.PrimaryProfileID, DefaultDictatePrimaryProfileID; got != want {
		t.Errorf("dictate primary profile = %q, want %q", got, want)
	}
	if got, want := cfg.ModelSelection.Assist.PrimaryProfileID, DefaultAssistPrimaryProfileID; got != want {
		t.Errorf("assist primary profile = %q, want %q", got, want)
	}
	if got, want := cfg.ModelSelection.VoiceAgent.PrimaryProfileID, DefaultVoiceAgentPrimaryProfileID; got != want {
		t.Errorf("voice agent primary profile = %q, want %q", got, want)
	}
}

func TestLoadPreservesConfiguredLocalLLMProfilesWithoutModelPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[model_selection.assist]
primary_profile_id = "assist.builtin.gemma4-e4b"
fallback_profile_id = ""

[model_selection.voice_agent]
primary_profile_id = "realtime.builtin.pipeline"
fallback_profile_id = ""

[local_llm]
enabled = false
model_path = ""

[voice_agent]
model = "speechkit-local-voice-pipeline"
pipeline_fallback = true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.ModelSelection.Assist.PrimaryProfileID; got != "assist.builtin.gemma4-e4b" {
		t.Fatalf("assist primary profile = %q, want local built-in profile", got)
	}
	if got, want := cfg.ModelSelection.VoiceAgent.PrimaryProfileID, "realtime.builtin.pipeline"; got != want {
		t.Fatalf("voice agent primary profile = %q, want %q", got, want)
	}
	if !cfg.VoiceAgent.PipelineFallback {
		t.Fatal("voice agent pipeline fallback should stay enabled")
	}
	if got, want := cfg.VoiceAgent.Model, "speechkit-local-voice-pipeline"; got != want {
		t.Fatalf("voice agent model = %q, want %q", got, want)
	}
}

func TestLoadBackfillsAssistModelFromLegacyAgentModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[providers.ollama]
enabled = true
agent_model = "gemma4:e4b"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := cfg.Providers.Ollama.AgentModel, "gemma4:e4b"; got != want {
		t.Fatalf("agent model = %q, want %q", got, want)
	}
	if got, want := cfg.Providers.Ollama.AssistModel, "gemma4:e4b"; got != want {
		t.Fatalf("assist model = %q, want %q", got, want)
	}
}

func TestLoadBackfillsLocalLLMAssistModelFromLegacyAgentModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[local_llm]
enabled = true
agent_model = "gemma4:e4b"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := cfg.LocalLLM.AgentModel, "gemma4:e4b"; got != want {
		t.Fatalf("agent model = %q, want %q", got, want)
	}
	if got, want := cfg.LocalLLM.AssistModel, "gemma4:e4b"; got != want {
		t.Fatalf("assist model = %q, want %q", got, want)
	}
}

func TestLoadRejectsRemovedVoiceAgentInstructionAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[voice_agent]
instruction = "Legacy framework prompt"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load succeeded, want removed alias error")
	}
}

func TestLoadPrefersExplicitStoreSaveAudioOverLegacyFeedback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[feedback]
save_audio = true

[store]
backend = "sqlite"
save_audio = false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Store.SaveAudio {
		t.Fatal("store.save_audio should remain false when explicitly set in [store]")
	}
}

func TestLoadPreservesExplicitPostgresStoreConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[feedback]
db_path = "C:/legacy/feedback.db"

[store]
backend = "postgres"
postgres_dsn = "` + postgresTestDSN("speechkit", "secret", "localhost:5432", "speechkit", "?sslmode=disable") + `"
save_audio = false
max_audio_storage_mb = 1024
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Store.Backend != "postgres" {
		t.Fatalf("store.backend = %q, want postgres", cfg.Store.Backend)
	}
	if cfg.Store.PostgresDSN == "" {
		t.Fatal("expected postgres dsn to be loaded")
	}
	if cfg.Store.SQLitePath != "" {
		t.Fatalf("store.sqlite_path = %q, want empty", cfg.Store.SQLitePath)
	}
	if cfg.Store.MaxAudioStorageMB != 1024 {
		t.Fatalf("store.max_audio_storage_mb = %d, want 1024", cfg.Store.MaxAudioStorageMB)
	}
}
