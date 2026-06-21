package hostconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadMapsModesAndPolicy(t *testing.T) {
	path := writeConfig(t, `
[general]
dictate_enabled = false
assist_enabled = true
voice_agent_enabled = true
assist_hotkey = "ctrl+alt+a"

[model_selection.assist]
primary_profile_id = "assist.openai.gpt-4o-mini"
fallback_profile_id = "assist.builtin.gemma4-e4b"
`)

	settings, policy, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !settings.Assist.Enabled {
		t.Error("expected Assist enabled")
	}
	if got, want := settings.Assist.Hotkey, "ctrl+alt+a"; got != want {
		t.Errorf("Assist hotkey = %q, want %q", got, want)
	}
	if got, want := settings.Assist.PrimaryProfileID, "assist.openai.gpt-4o-mini"; got != want {
		t.Errorf("Assist primary = %q, want %q", got, want)
	}
	if got, want := settings.Assist.FallbackProfileID, "assist.builtin.gemma4-e4b"; got != want {
		t.Errorf("Assist fallback = %q, want %q", got, want)
	}

	// Disabled modes drop out of the derived policy; enabled ones stay.
	if hasMode(policy.EnabledModes, speechkit.ModeDictation) {
		t.Error("Dictation is disabled in config but present in policy")
	}
	if !hasMode(policy.EnabledModes, speechkit.ModeAssist) {
		t.Error("Assist enabled in config but missing from policy")
	}
	if !hasMode(policy.EnabledModes, speechkit.ModeVoiceAgent) {
		t.Error("VoiceAgent enabled in config but missing from policy")
	}

	// A configured fallback profile flips AllowFallbacks on.
	if !policy.AllowFallbacks {
		t.Error("expected AllowFallbacks true when a fallback profile is configured")
	}
}

func TestModeSettingsFromDefaultsEmptyPrimary(t *testing.T) {
	cfg := &config.Config{}
	cfg.General.AssistEnabled = true

	settings := ModeSettingsFrom(cfg)
	if got, want := settings.Assist.PrimaryProfileID, config.DefaultAssistPrimaryProfileID; got != want {
		t.Errorf("empty primary not defaulted: got %q, want %q", got, want)
	}
	// Empty ModeSource resolves to "local", never silently "server".
	if got := settings.Assist.ModeSource; got != config.ModeSourceLocal {
		t.Errorf("ModeSource = %q, want %q", got, config.ModeSourceLocal)
	}
}

func TestNormalizeSelectionDropsRedundantFallback(t *testing.T) {
	cfg := &config.Config{}
	cfg.ModelSelection.Assist = config.ModeModelSelection{
		PrimaryProfileID:  "assist.builtin.gemma4-e4b",
		FallbackProfileID: "assist.builtin.gemma4-e4b",
	}

	settings := ModeSettingsFrom(cfg)
	if settings.Assist.FallbackProfileID != "" {
		t.Errorf("fallback identical to primary should be dropped, got %q", settings.Assist.FallbackProfileID)
	}

	// And with no real fallback anywhere, the policy keeps fallbacks off.
	if PolicyFrom(cfg).AllowFallbacks {
		t.Error("expected AllowFallbacks false when no distinct fallback is configured")
	}
}

func TestNilConfigYieldsZeroValues(t *testing.T) {
	if got := ModeSettingsFrom(nil); !reflect.DeepEqual(got, speechkit.ModeSettings{}) {
		t.Errorf("nil cfg should yield zero ModeSettings, got %+v", got)
	}
	if got := PolicyFrom(nil); len(got.EnabledModes) != 0 || got.AllowFallbacks {
		t.Errorf("nil cfg should yield zero RuntimePolicy, got %+v", got)
	}
}

func hasMode(modes []speechkit.Mode, target speechkit.Mode) bool {
	for _, m := range modes {
		if m == target {
			return true
		}
	}
	return false
}
