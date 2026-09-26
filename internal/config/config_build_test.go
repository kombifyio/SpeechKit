package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedHuggingFaceAvailableInBuild_DefaultsDisabledWhenUnset(t *testing.T) {
	restoreBuild := OverrideManagedHuggingFaceBuildForTests("")
	defer restoreBuild()

	if ManagedHuggingFaceAvailableInBuild() {
		t.Fatal("ManagedHuggingFaceAvailableInBuild() = true, want false without explicit build ldflag")
	}
}

func TestManagedHuggingFaceAvailableInBuild_PublicModuleFallbackStaysDisabled(t *testing.T) {
	restoreBuild := OverrideManagedHuggingFaceBuildForTests("")
	defer restoreBuild()

	if ManagedHuggingFaceAvailableInBuild() {
		t.Fatal("ManagedHuggingFaceAvailableInBuild() = true, want false without explicit build ldflag")
	}
}

func TestPhase0Defaults(t *testing.T) {
	cfg := defaults()

	const wantManifestURL = "https://api.github.com/repos/kombifyio/SpeechKit/releases/latest"
	if cfg.Update.ManifestURL != wantManifestURL {
		t.Errorf("Update.ManifestURL: want %q, got %q", wantManifestURL, cfg.Update.ManifestURL)
	}
	if cfg.Update.CheckIntervalHours != 6 {
		t.Errorf("Update.CheckIntervalHours: want 6, got %d", cfg.Update.CheckIntervalHours)
	}
	if !cfg.Update.Enabled {
		t.Errorf("Update.Enabled: want true by default, got false")
	}
	if cfg.Logging.MaxFileSizeMB != 50 {
		t.Errorf("Logging.MaxFileSizeMB: want 50, got %d", cfg.Logging.MaxFileSizeMB)
	}
	if cfg.Logging.MaxFiles != 30 {
		t.Errorf("Logging.MaxFiles: want 30, got %d", cfg.Logging.MaxFiles)
	}
	if cfg.Logging.Level != "off" {
		t.Errorf("Logging.Level: want \"off\" by default (privacy-first opt-in), got %q", cfg.Logging.Level)
	}
	if cfg.Audit.Enabled {
		t.Errorf("Audit.Enabled: want false by default (compliance log is opt-in), got true")
	}
	if cfg.Audit.RetentionDays != 90 {
		t.Errorf("Audit.RetentionDays: want 90 (retention applies when audit is opted in), got %d", cfg.Audit.RetentionDays)
	}
	if !cfg.Telemetry.UpdateCheck {
		t.Errorf("Telemetry.UpdateCheck: want true by default, got false")
	}
}

func TestLoadDisablesTelemetryWhenUpdateDisabled(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(tmpFile, []byte("[update]\nenabled = false\n"), 0o600); err != nil {
		t.Fatalf("write tmp config: %v", err)
	}
	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Update.Enabled {
		t.Errorf("Update.Enabled: want false (from toml), got true")
	}
	if cfg.Telemetry.UpdateCheck {
		t.Errorf("Telemetry.UpdateCheck: want false (backfilled from disabled update), got true")
	}
}

func TestEnterprisePresetsLoad(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		wantProv string
		wantUpd  bool
		wantStrt string
	}{
		{
			name:     "onprem profile A",
			path:     "../../deploy/presets/enterprise-onprem.toml",
			wantProv: "local-cascaded",
			wantUpd:  false,
			wantStrt: "local-only",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(tc.path)
			if err != nil {
				t.Fatalf("Load(%s): %v", tc.path, err)
			}
			if cfg.VoiceAgent.Provider != tc.wantProv {
				t.Errorf("VoiceAgent.Provider: want %q, got %q", tc.wantProv, cfg.VoiceAgent.Provider)
			}
			if cfg.Update.Enabled != tc.wantUpd {
				t.Errorf("Update.Enabled: want %v, got %v", tc.wantUpd, cfg.Update.Enabled)
			}
			if cfg.Routing.Strategy != tc.wantStrt {
				t.Errorf("Routing.Strategy: want %q, got %q", tc.wantStrt, cfg.Routing.Strategy)
			}
		})
	}
}
