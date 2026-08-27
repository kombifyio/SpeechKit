package hostconfig_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/hostconfig"
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

const modesAndPolicyConfig = `
[general]
dictate_enabled = false
assist_enabled = true
voice_agent_enabled = true
assist_hotkey = "ctrl+alt+a"

[model_selection.assist]
primary_profile_id = "assist.openai.gpt-4o-mini"
fallback_profile_id = "assist.builtin.gemma4-e4b"
`

// parityConfig sets every mapped field the reference app's defaults would
// otherwise fill, so Load (defaults + backfills) and Parse (verbatim) must
// agree on it. This pins the mapping itself, not the defaulting.
const parityConfig = `
[general]
dictate_enabled = true
assist_enabled = true
voice_agent_enabled = false
dictate_hotkey = "ctrl+alt+d"
assist_hotkey = "ctrl+alt+a"
voice_agent_hotkey = "ctrl+alt+v"
dictate_hotkey_behavior = "hold_to_talk"
assist_hotkey_behavior = "hold_to_talk"
voice_agent_hotkey_behavior = "hold_to_talk"

[model_selection.dictate]
primary_profile_id = "stt.local.whispercpp"

[model_selection.assist]
primary_profile_id = "assist.openai.gpt-4o-mini"
fallback_profile_id = "assist.builtin.gemma4-e4b"

[model_selection.voice_agent]
primary_profile_id = "realtime.builtin.pipeline"

[vocabulary]
dictionary = "en"

[tts]
enabled = true

[voice_agent]
enable_session_summary = true
pipeline_fallback = false
close_behavior = "continue"
agent_profile_id = "default"
agent_sequence_id = ""

[server_connection]
enabled = false
url = ""
bearer_token_env = "SPEECHKIT_SERVER_TOKEN"
auth_mode = "bearer"
beta_install_id_env = "SPEECHKIT_BETA_INSTALL_ID"
beta_install_secret_env = "SPEECHKIT_BETA_INSTALL_SECRET"
fallback_to_local = false
request_timeout_sec = 0
`

func TestLoadMapsModesAndPolicy(t *testing.T) {
	path := writeConfig(t, modesAndPolicyConfig)

	settings, policy, err := hostconfig.Load(path)
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

// TestParseMatchesLoad pins the contract between the two entry points: for a
// config that sets every mapped field explicitly (so the reference app's
// defaulting has nothing to add), Parse + ModeSettingsFrom / PolicyFrom must
// produce exactly what Load produces.
func TestParseMatchesLoad(t *testing.T) {
	path := writeConfig(t, parityConfig)

	loadedSettings, loadedPolicy, err := hostconfig.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cfg, err := hostconfig.Parse([]byte(parityConfig))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	parsedSettings := hostconfig.ModeSettingsFrom(cfg)
	parsedPolicy := hostconfig.PolicyFrom(cfg)

	if !reflect.DeepEqual(parsedSettings, loadedSettings) {
		t.Errorf("Parse-derived settings diverge from Load:\nparse: %+v\nload:  %+v", parsedSettings, loadedSettings)
	}
	if !reflect.DeepEqual(parsedPolicy, loadedPolicy) {
		t.Errorf("Parse-derived policy diverges from Load:\nparse: %+v\nload:  %+v", parsedPolicy, loadedPolicy)
	}
}

// TestParseIgnoresUnknownTables proves the reference app's full config.toml
// (which carries many more tables) decodes cleanly into the public subset.
func TestParseIgnoresUnknownTables(t *testing.T) {
	cfg, err := hostconfig.Parse([]byte(`
[general]
assist_enabled = true

[audio]
backend = "wasapi"

[some_future_table]
key = "value"
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.General.AssistEnabled {
		t.Error("expected AssistEnabled from [general]")
	}
}

func TestModeSettingsFromDefaultsEmptyPrimary(t *testing.T) {
	cfg := &hostconfig.Config{}
	cfg.General.AssistEnabled = true

	settings := hostconfig.ModeSettingsFrom(cfg)
	// The built-in default Assist profile is a public catalog contract.
	if got, want := settings.Assist.PrimaryProfileID, "assist.builtin.gemma4-e4b"; got != want {
		t.Errorf("empty primary not defaulted: got %q, want %q", got, want)
	}
	// Empty ModeSource resolves to "local", never silently "server".
	if got := settings.Assist.ModeSource; got != hostconfig.ModeSourceLocal {
		t.Errorf("ModeSource = %q, want %q", got, hostconfig.ModeSourceLocal)
	}
}

func TestNormalizeSelectionDropsRedundantFallback(t *testing.T) {
	cfg := &hostconfig.Config{}
	cfg.ModelSelection.Assist = hostconfig.ModeSelection{
		PrimaryProfileID:  "assist.builtin.gemma4-e4b",
		FallbackProfileID: "assist.builtin.gemma4-e4b",
	}

	settings := hostconfig.ModeSettingsFrom(cfg)
	if settings.Assist.FallbackProfileID != "" {
		t.Errorf("fallback identical to primary should be dropped, got %q", settings.Assist.FallbackProfileID)
	}

	// And with no real fallback anywhere, the policy keeps fallbacks off.
	if hostconfig.PolicyFrom(cfg).AllowFallbacks {
		t.Error("expected AllowFallbacks false when no distinct fallback is configured")
	}
}

func TestNilConfigYieldsZeroValues(t *testing.T) {
	if got := hostconfig.ModeSettingsFrom(nil); !reflect.DeepEqual(got, speechkit.ModeSettings{}) {
		t.Errorf("nil cfg should yield zero ModeSettings, got %+v", got)
	}
	if got := hostconfig.PolicyFrom(nil); len(got.EnabledModes) != 0 || got.AllowFallbacks {
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
