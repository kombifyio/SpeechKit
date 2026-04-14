package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("Load defaults: %v", err)
	}

	if cfg.General.Language != "de" {
		t.Errorf("default language = %q, want %q", cfg.General.Language, "de")
	}
	if cfg.General.Hotkey != "win+alt" {
		t.Errorf("default hotkey = %q, want %q", cfg.General.Hotkey, "win+alt")
	}
	if cfg.General.AutoStopSilenceMs != 500 {
		t.Errorf("default silence ms = %d, want 500", cfg.General.AutoStopSilenceMs)
	}
	if cfg.Local.Enabled {
		t.Error("local provider should be disabled by default")
	}
	if cfg.HuggingFace.Enabled {
		t.Error("HuggingFace should be disabled by default in OSS-safe builds")
	}
	if cfg.HuggingFace.Model != "openai/whisper-large-v3" {
		t.Errorf("default HF model = %q", cfg.HuggingFace.Model)
	}
	if cfg.VoiceAgent.Model != "gemini-2.5-flash-native-audio-preview-12-2025" {
		t.Errorf("default voice agent model = %q", cfg.VoiceAgent.Model)
	}
	if cfg.Routing.PreferLocalUnderSeconds != 10 {
		t.Errorf("default prefer local = %f, want 10", cfg.Routing.PreferLocalUnderSeconds)
	}
	if cfg.Routing.Strategy != "cloud-only" {
		t.Errorf("default routing strategy = %q, want %q", cfg.Routing.Strategy, "cloud-only")
	}
	if !cfg.UI.OverlayEnabled {
		t.Error("overlay should be enabled by default")
	}
	if cfg.UI.Visualizer != "pill" {
		t.Errorf("visualizer = %q, want %q", cfg.UI.Visualizer, "pill")
	}
	if cfg.UI.Design != "default" {
		t.Errorf("design = %q, want %q", cfg.UI.Design, "default")
	}
	if !cfg.Store.SaveAudio {
		t.Error("store audio persistence should be enabled by default for local mode")
	}
	if !cfg.Feedback.SaveAudio {
		t.Error("legacy feedback audio persistence should stay aligned with store defaults")
	}
	if cfg.Store.AudioRetentionDays != 7 {
		t.Errorf("store audio retention days = %d, want 7", cfg.Store.AudioRetentionDays)
	}
	if cfg.Feedback.AudioRetentionDays != 7 {
		t.Errorf("legacy feedback audio retention days = %d, want 7", cfg.Feedback.AudioRetentionDays)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[general]
language = "en"
hotkey = "ctrl+f5"

[huggingface]
enabled = false
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.General.Language != "en" {
		t.Errorf("language = %q, want %q", cfg.General.Language, "en")
	}
	if cfg.General.Hotkey != "ctrl+f5" {
		t.Errorf("hotkey = %q, want %q", cfg.General.Hotkey, "ctrl+f5")
	}
	if cfg.HuggingFace.Enabled {
		t.Error("HuggingFace should be disabled")
	}
	// Defaults should still be present for unset fields
	if cfg.Local.Port != 8080 {
		t.Errorf("local port = %d, want 8080 (default)", cfg.Local.Port)
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
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
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
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
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
postgres_dsn = "postgres://speechkit:secret@localhost:5432/speechkit?sslmode=disable"
save_audio = false
max_audio_storage_mb = 1024
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
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

func TestResolveSecret_EnvVar(t *testing.T) {
	t.Setenv("TEST_SECRET_KEY", "test-value-123")
	val := ResolveSecret("TEST_SECRET_KEY")
	if val != "test-value-123" {
		t.Errorf("ResolveSecret = %q, want %q", val, "test-value-123")
	}
}

func TestResolveSecret_Missing(t *testing.T) {
	val := ResolveSecret("NONEXISTENT_KEY_THAT_SHOULD_NOT_EXIST_12345")
	// Might return empty or a Doppler value; just ensure no panic
	_ = val
}

func TestResolveSecret_DopplerFallback(t *testing.T) {
	t.Cleanup(resetDopplerHooksForTests)
	t.Setenv("DOPPLER_PROJECT", "test-project")
	t.Setenv("DOPPLER_CONFIG", "stage")
	dopplerLookPath = func(file string) (string, error) {
		if file != "doppler" {
			t.Fatalf("lookPath file = %q", file)
		}
		return "C:\\fake\\doppler.exe", nil
	}
	dopplerSecretLookup = func(dopplerPath, key, project, cfg string) (string, error) {
		if dopplerPath != "C:\\fake\\doppler.exe" {
			t.Fatalf("dopplerPath = %q", dopplerPath)
		}
		if key != "TEST_DOPPLER_SECRET" {
			t.Fatalf("key = %q", key)
		}
		if project == "test-project" && cfg == "stage" {
			return "secret-from-doppler", nil
		}
		return "", errors.New("not found")
	}

	value := ResolveSecret("TEST_DOPPLER_SECRET")

	if value != "secret-from-doppler" {
		t.Fatalf("ResolveSecret = %q", value)
	}
}

func TestFindDopplerExecutableUsesEnvOverride(t *testing.T) {
	t.Cleanup(resetDopplerHooksForTests)
	dopplerLookPath = func(string) (string, error) {
		return "", &exec.Error{Name: "doppler", Err: errors.New("not found")}
	}

	fake := filepath.Join(t.TempDir(), "doppler.exe")
	if err := os.WriteFile(fake, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOPPLER_PATH", fake)

	path := findDopplerExecutable()

	if path != fake {
		t.Fatalf("findDopplerExecutable = %q, want %q", path, fake)
	}
}

func TestFindDopplerExecutableFallsBackToWingetLink(t *testing.T) {
	t.Cleanup(resetDopplerHooksForTests)
	dopplerLookPath = func(string) (string, error) {
		return "", &exec.Error{Name: "doppler", Err: errors.New("not found")}
	}

	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	fake := filepath.Join(localAppData, "Microsoft", "WinGet", "Links", "doppler.exe")
	if err := os.MkdirAll(filepath.Dir(fake), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fake, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	path := findDopplerExecutable()

	if path != fake {
		t.Fatalf("findDopplerExecutable = %q, want %q", path, fake)
	}
}

func TestDopplerProjectsAndConfigsRequireExplicitEnv(t *testing.T) {
	t.Setenv("DOPPLER_PROJECT", "test-project")
	t.Setenv("DOPPLER_CONFIG", "stage")

	projects := dopplerProjects()
	configs := dopplerConfigs()

	if len(projects) == 0 || projects[0] != "test-project" {
		t.Fatalf("projects = %v", projects)
	}
	if len(configs) == 0 || configs[0] != "stage" {
		t.Fatalf("configs = %v", configs)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %v", projects)
	}
	if len(configs) != 1 {
		t.Fatalf("configs = %v", configs)
	}
}

func TestDopplerProjectsAndConfigsStayEmptyWithoutEnv(t *testing.T) {
	projects := dopplerProjects()
	configs := dopplerConfigs()

	if len(projects) != 0 {
		t.Fatalf("projects = %v", projects)
	}
	if len(configs) != 0 {
		t.Fatalf("configs = %v", configs)
	}
}

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
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
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
	restoreBuild := OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaults()
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

func TestDefaultHotkeyMode(t *testing.T) {
	cfg := defaults()
	if cfg.General.HotkeyMode != "push_to_talk" {
		t.Fatalf("default HotkeyMode = %q, want %q", cfg.General.HotkeyMode, "push_to_talk")
	}
}

func TestDefaultOverlayPosition(t *testing.T) {
	cfg := defaults()
	if cfg.UI.OverlayPosition != "top" {
		t.Fatalf("default OverlayPosition = %q, want %q", cfg.UI.OverlayPosition, "top")
	}
	if cfg.UI.OverlayMovable {
		t.Fatal("default OverlayMovable = true, want false")
	}
	if cfg.UI.OverlayFreeX != 0 || cfg.UI.OverlayFreeY != 0 {
		t.Fatalf("default free overlay coordinates = (%d,%d), want (0,0)", cfg.UI.OverlayFreeX, cfg.UI.OverlayFreeY)
	}
}

func TestDefaultStoreAudioSettings(t *testing.T) {
	cfg := defaults()
	if cfg.General.Hotkey != "win+alt" {
		t.Fatalf("default Hotkey = %q, want %q", cfg.General.Hotkey, "win+alt")
	}
	if !cfg.Store.SaveAudio {
		t.Fatal("default Store.SaveAudio = false, want true")
	}
	if cfg.Store.AudioRetentionDays != 7 {
		t.Fatalf("default Store.AudioRetentionDays = %d, want %d", cfg.Store.AudioRetentionDays, 7)
	}
}

func TestSaveRoundTripNewFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := defaults()
	cfg.General.HotkeyMode = "toggle"
	cfg.UI.OverlayPosition = "bottom"
	cfg.UI.OverlayMovable = true
	cfg.UI.OverlayFreeX = 864
	cfg.UI.OverlayFreeY = 512

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.General.HotkeyMode != "toggle" {
		t.Fatalf("HotkeyMode = %q, want %q", reloaded.General.HotkeyMode, "toggle")
	}
	if reloaded.UI.OverlayPosition != "bottom" {
		t.Fatalf("OverlayPosition = %q, want %q", reloaded.UI.OverlayPosition, "bottom")
	}
	if !reloaded.UI.OverlayMovable {
		t.Fatal("OverlayMovable = false, want true")
	}
	if reloaded.UI.OverlayFreeX != 864 || reloaded.UI.OverlayFreeY != 512 {
		t.Fatalf("free overlay coordinates = (%d,%d), want (864,512)", reloaded.UI.OverlayFreeX, reloaded.UI.OverlayFreeY)
	}
}

func TestLoadPreservesUnsetNewFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Write a config file that does NOT contain hotkey_mode or overlay_position.
	content := `[general]
language = "en"
hotkey = "ctrl+shift"
auto_stop_silence_ms = 300

[ui]
overlay_enabled = true
visualizer = "pill"
design = "default"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Fields absent from file should retain defaults.
	if cfg.General.HotkeyMode != "push_to_talk" {
		t.Fatalf("HotkeyMode = %q, want default %q", cfg.General.HotkeyMode, "push_to_talk")
	}
	if cfg.UI.OverlayPosition != "top" {
		t.Fatalf("OverlayPosition = %q, want default %q", cfg.UI.OverlayPosition, "top")
	}
}

func TestApplyManagedIntegrationDefaultsSkipsNonCloudOnly(t *testing.T) {
	restoreBuild := OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaults()
	cfg.HuggingFace.Enabled = false // Explicitly disabled
	cfg.Routing.Strategy = "dynamic"
	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")
	t.Setenv("HF_TOKEN", "test-token")

	changed := ApplyManagedIntegrationDefaults(cfg)

	if changed {
		t.Fatal("ApplyManagedIntegrationDefaults should return false for non-cloud-only strategy")
	}
	if cfg.HuggingFace.Enabled {
		t.Fatal("huggingface should remain disabled when strategy is not cloud-only")
	}
}

func TestApplyLocalInstallDefaultsPreparesPendingLocalInstallForOnboardingDownloads(t *testing.T) {
	cfg := defaults()
	state := &InstallState{Mode: InstallModeLocal}

	changed := ApplyLocalInstallDefaults(cfg, state)

	if !changed {
		t.Fatal("expected local install defaults to change config")
	}
	if cfg.Local.Enabled {
		t.Fatal("local provider should stay disabled until the user downloads a model")
	}
	if cfg.Routing.Strategy != "dynamic" {
		t.Fatalf("routing strategy = %q, want %q", cfg.Routing.Strategy, "dynamic")
	}
	if cfg.HuggingFace.Enabled {
		t.Fatal("HuggingFace should be disabled on fresh local install while onboarding is pending")
	}
	if cfg.Local.Model != "ggml-small.bin" {
		t.Fatalf("local model = %q, want %q", cfg.Local.Model, "ggml-small.bin")
	}
}

func TestApplyLocalInstallDefaultsSkipsCompletedSetup(t *testing.T) {
	cfg := defaults()
	state := &InstallState{Mode: InstallModeLocal, SetupDone: true}

	changed := ApplyLocalInstallDefaults(cfg, state)

	if changed {
		t.Fatal("expected completed setup to keep config unchanged")
	}
	if cfg.Local.Enabled {
		t.Fatal("local provider should remain unchanged after setup is complete")
	}
	if cfg.Routing.Strategy != "cloud-only" {
		t.Fatalf("routing strategy = %q, want %q", cfg.Routing.Strategy, "cloud-only")
	}
}

func TestApplyLocalInstallDefaultsSkipsCloudInstalls(t *testing.T) {
	cfg := defaults()
	state := &InstallState{Mode: InstallModeCloud}

	changed := ApplyLocalInstallDefaults(cfg, state)

	if changed {
		t.Fatal("expected cloud installs to keep config unchanged")
	}
	if cfg.Local.Enabled {
		t.Fatal("local provider should remain disabled for cloud installs")
	}
	if cfg.Routing.Strategy != "cloud-only" {
		t.Fatalf("routing strategy = %q, want %q", cfg.Routing.Strategy, "cloud-only")
	}
}

// --- InstallMode tests ---

func TestLoadMalformedTOMLFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Write garbage TOML that will fail to parse.
	if err := os.WriteFile(path, []byte("{{{{not valid toml!!!!"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load should not error on malformed TOML, got: %v", err)
	}
	if cfg.General.Language != "de" {
		t.Errorf("expected default language %q, got %q", "de", cfg.General.Language)
	}
	if cfg.General.Hotkey != "win+alt" {
		t.Errorf("expected default hotkey %q, got %q", "win+alt", cfg.General.Hotkey)
	}
}

func TestLoadInstallState_NoFile(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	state, err := LoadInstallState()
	if err != nil {
		t.Fatalf("LoadInstallState: %v", err)
	}
	if state.Mode != InstallModeNotSet {
		t.Fatalf("Mode = %q, want empty", state.Mode)
	}
	if state.DeviceID != "" {
		t.Fatalf("DeviceID = %q, want empty", state.DeviceID)
	}
	if state.SetupDone {
		t.Fatal("SetupDone should be false")
	}
}

func TestSaveAndLoadInstallState(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	state := &InstallState{Mode: InstallModeCloud}
	if err := SaveInstallState(state); err != nil {
		t.Fatalf("SaveInstallState: %v", err)
	}

	loaded, err := LoadInstallState()
	if err != nil {
		t.Fatalf("LoadInstallState: %v", err)
	}
	if loaded.Mode != InstallModeCloud {
		t.Fatalf("Mode = %q, want %q", loaded.Mode, InstallModeCloud)
	}
	if loaded.DeviceID == "" {
		t.Fatal("DeviceID should be set after save")
	}
}

func TestIsFirstRun_True(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	if !IsFirstRun() {
		t.Fatal("IsFirstRun should return true for empty APPDATA dir")
	}
}

func TestIsFirstRun_False(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	if err := SaveInstallState(&InstallState{Mode: InstallModeLocal}); err != nil {
		t.Fatalf("SaveInstallState: %v", err)
	}

	if IsFirstRun() {
		t.Fatal("IsFirstRun should return false after SaveInstallState")
	}
}

func TestSaveInstallState_GeneratesDeviceID(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	state := &InstallState{Mode: InstallModeLocal}
	if state.DeviceID != "" {
		t.Fatal("precondition: DeviceID should start empty")
	}

	if err := SaveInstallState(state); err != nil {
		t.Fatalf("SaveInstallState: %v", err)
	}

	loaded, err := LoadInstallState()
	if err != nil {
		t.Fatalf("LoadInstallState: %v", err)
	}
	if loaded.DeviceID == "" {
		t.Fatal("DeviceID should be generated on save")
	}
	if len(loaded.DeviceID) < 32 {
		t.Fatalf("DeviceID too short: %q", loaded.DeviceID)
	}
}

func TestInstallModeConstants(t *testing.T) {
	if InstallModeLocal != "local" {
		t.Fatalf("InstallModeLocal = %q, want %q", InstallModeLocal, "local")
	}
	if InstallModeCloud != "cloud" {
		t.Fatalf("InstallModeCloud = %q, want %q", InstallModeCloud, "cloud")
	}
	if InstallModeNotSet != "" {
		t.Fatalf("InstallModeNotSet = %q, want empty", InstallModeNotSet)
	}
}
