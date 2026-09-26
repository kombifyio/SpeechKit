package config

import (
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyManagedIntegrationDefaultsSkipsNonCloudOnly(t *testing.T) {
	useMemorySecretStoreForTest(t)
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

func TestLoadBackfillsGeneralAutoStartFromLegacyVoiceAgentSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[general]
dictate_hotkey = "win+alt"
assist_hotkey = "ctrl+win"
voice_agent_hotkey = "ctrl+shift"

[voice_agent]
auto_start_on_launch = true
`

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.General.AutoStartOnLaunch {
		t.Fatal("General.AutoStartOnLaunch = false, want true from legacy voice_agent section")
	}
	if !cfg.VoiceAgent.AutoStartOnLaunch {
		t.Fatal("VoiceAgent.AutoStartOnLaunch = false, want true after sync")
	}
	if !cfg.General.StartAtLogin {
		t.Fatal("General.StartAtLogin = false, want true from legacy startup preference")
	}
}

func TestLoadPrefersGeneralAutoStartOverLegacyVoiceAgentSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[general]
dictate_hotkey = "win+alt"
assist_hotkey = "ctrl+win"
voice_agent_hotkey = "ctrl+shift"
auto_start_on_launch = false

[voice_agent]
auto_start_on_launch = true
`

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.General.AutoStartOnLaunch {
		t.Fatal("General.AutoStartOnLaunch = true, want explicit general setting to win")
	}
	if cfg.VoiceAgent.AutoStartOnLaunch {
		t.Fatal("VoiceAgent.AutoStartOnLaunch = true, want sync from explicit general setting")
	}
	if cfg.General.StartAtLogin {
		t.Fatal("General.StartAtLogin = true, want explicit dashboard auto-open setting to keep login start disabled")
	}
}

func TestLoadPrefersExplicitStartAtLoginOverLegacyAutoStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[general]
dictate_hotkey = "win+alt"
assist_hotkey = "ctrl+win"
voice_agent_hotkey = "ctrl+shift"
auto_start_on_launch = true
start_at_login = false
`

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.General.AutoStartOnLaunch {
		t.Fatal("General.AutoStartOnLaunch = false, want true from explicit config")
	}
	if cfg.General.StartAtLogin {
		t.Fatal("General.StartAtLogin = true, want explicit start_at_login=false to win")
	}
}

func TestApplyLocalInstallDefaultsPreparesPendingLocalInstallForOnboardingDownloads(t *testing.T) {
	cfg := defaults()
	cfg.Local.Enabled = false
	cfg.Routing.Strategy = "cloud-only"
	cfg.HuggingFace.Enabled = true
	state := &InstallState{Mode: InstallModeLocal}

	changed := ApplyLocalInstallDefaults(cfg, state)

	if !changed {
		t.Fatal("expected local install defaults to change config")
	}
	if !cfg.Local.Enabled {
		t.Fatal("local provider should be enabled for local-first installs")
	}
	if cfg.Routing.Strategy != "local-only" {
		t.Fatalf("routing strategy = %q, want %q", cfg.Routing.Strategy, "local-only")
	}
	if cfg.HuggingFace.Enabled {
		t.Fatal("HuggingFace should be disabled on fresh local install while onboarding is pending")
	}
	if cfg.Local.Model != DefaultLocalSTTModel {
		t.Fatalf("local model = %q, want %q", cfg.Local.Model, DefaultLocalSTTModel)
	}
}

func TestApplyLocalInstallDefaultsSkipsCompletedSetup(t *testing.T) {
	cfg := defaults()
	state := &InstallState{Mode: InstallModeLocal, SetupDone: true}

	changed := ApplyLocalInstallDefaults(cfg, state)

	if changed {
		t.Fatal("expected completed setup to keep config unchanged")
	}
	if !cfg.Local.Enabled {
		t.Fatal("local provider should remain enabled after setup is complete")
	}
	if cfg.Routing.Strategy != "local-only" {
		t.Fatalf("routing strategy = %q, want %q", cfg.Routing.Strategy, "local-only")
	}
}

func TestApplyLocalInstallDefaultsSkipsCloudInstalls(t *testing.T) {
	cfg := defaults()
	state := &InstallState{Mode: InstallModeCloud}

	changed := ApplyLocalInstallDefaults(cfg, state)

	if changed {
		t.Fatal("expected cloud installs to keep config unchanged")
	}
	if !cfg.Local.Enabled {
		t.Fatal("local provider should remain enabled unless a cloud install explicitly changes it")
	}
	if cfg.Routing.Strategy != "local-only" {
		t.Fatalf("routing strategy = %q, want %q", cfg.Routing.Strategy, "local-only")
	}
}

// --- InstallMode tests ---

func TestLoadMalformedTOMLFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Write garbage TOML that will fail to parse.
	if err := os.WriteFile(path, []byte("{{{{not valid toml!!!!"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load should not error on malformed TOML, got: %v", err)
	}
	if cfg.General.Language != stt.LanguageMulti {
		t.Errorf("expected default language %q, got %q", stt.LanguageMulti, cfg.General.Language)
	}
	if cfg.General.Hotkey != "ctrl+win" {
		t.Errorf("expected default hotkey %q, got %q", "ctrl+win", cfg.General.Hotkey)
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
