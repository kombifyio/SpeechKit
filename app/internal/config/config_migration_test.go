package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Persisted configuration format upgrades: a config.toml written by an older
// release loads into the current shape and the next save drops the legacy keys.

func loadLegacyMeetingConfig(t *testing.T, meeting string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[meeting]\n"+meeting), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestMeetingFallbackPolicyMigratesToFallbackProviders(t *testing.T) {
	cases := []struct {
		name          string
		file          string
		wantPrimary   string
		wantFallbacks []string
	}{
		{"copilot with local fallback", "generation_provider = \"github_copilot\"\nfallback_policy = \"allow_local_fallback\"\n", MeetingProviderCopilot, []string{MeetingProviderLocal}},
		{"copilot without fallback", "generation_provider = \"github_copilot\"\nfallback_policy = \"local_only\"\n", MeetingProviderCopilot, []string{}},
		{"local ignores a local fallback", "generation_provider = \"local\"\nfallback_policy = \"allow_local_fallback\"\n", MeetingProviderLocal, []string{}},
		{"explicit providers win", "generation_provider = \"github_copilot\"\nfallback_policy = \"allow_local_fallback\"\nfallback_providers = [\"foundry\"]\n", MeetingProviderCopilot, []string{MeetingProviderFoundry}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := loadLegacyMeetingConfig(t, tc.file)
			if cfg.Meeting.GenerationProvider != tc.wantPrimary {
				t.Fatalf("primary = %q, want %q", cfg.Meeting.GenerationProvider, tc.wantPrimary)
			}
			if !reflect.DeepEqual(cfg.Meeting.FallbackProviders, tc.wantFallbacks) {
				t.Fatalf("fallbacks = %#v, want %#v", cfg.Meeting.FallbackProviders, tc.wantFallbacks)
			}
			if cfg.Meeting.FallbackPolicy != "" {
				t.Fatalf("legacy fallback_policy kept: %q", cfg.Meeting.FallbackPolicy)
			}
		})
	}
}

func TestMigratedMeetingSettingsDropTheLegacyKeyOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[meeting]\ngeneration_provider = \"github_copilot\"\nfallback_policy = \"allow_local_fallback\"\ngeneration_model = \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := save(path, cfg); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path) // #nosec G304 -- test temp file
	if err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{"fallback_policy", "generation_model"} {
		if strings.Contains(string(written), removed) {
			t.Fatalf("saved config still carries %s", removed)
		}
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded.Meeting.FallbackProviders, []string{MeetingProviderLocal}) {
		t.Fatalf("fallbacks after a save = %#v", reloaded.Meeting.FallbackProviders)
	}
}

func TestLoadMigratesOldBuiltInHotkeyDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `[general]
hotkey = "win+alt"
dictate_hotkey = "win+alt"
assist_hotkey = "ctrl+win"
voice_agent_hotkey = "ctrl+shift"
agent_hotkey = "ctrl+win"
agent_mode = "assist"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := cfg.General.Hotkey, "ctrl+win"; got != want {
		t.Fatalf("legacy hotkey alias = %q, want %q", got, want)
	}
	if got, want := cfg.General.DictateHotkey, "ctrl+win"; got != want {
		t.Fatalf("dictate hotkey = %q, want %q", got, want)
	}
	if got, want := cfg.General.AssistHotkey, "win+alt"; got != want {
		t.Fatalf("assist hotkey = %q, want %q", got, want)
	}
	if got, want := cfg.General.AgentHotkey, "win+alt"; got != want {
		t.Fatalf("agent hotkey alias = %q, want %q", got, want)
	}
}
