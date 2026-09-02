package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/hostconfig"
)

// The desktop loader and the public hostconfig loader must read the same
// legacy file to the same embedder-visible result; otherwise a Companion host
// and the reference app would bind different hotkeys and modes from one
// config.toml.
func TestLoadAgreesWithPublicHostconfigLoaderOnLegacyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `[general]
hotkey = "win+alt"
hotkey_mode = "push_to_talk"
agent_hotkey = "ctrl+win"
agent_mode = "assist"
voice_agent_hotkey = " ctrl+shift "

[model_selection.assist]
primary_profile_id = "assist.builtin.gemma4-e4b"

[voice_agent]
close_behavior = "NEW_CHAT"

[server_connection]
url = "https://speech.example.test/"
auth_mode = "api_key"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	desktop, err := Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	public, err := hostconfig.LoadConfig(path)
	if err != nil {
		t.Fatalf("hostconfig.LoadConfig: %v", err)
	}

	shared := sharedView(desktop)
	desktopSettings := hostconfig.ModeSettingsFrom(&shared)
	publicSettings := hostconfig.ModeSettingsFrom(public)
	if !reflect.DeepEqual(desktopSettings, publicSettings) {
		t.Fatalf("loaders disagree\n desktop: %+v\n public:  %+v", desktopSettings, publicSettings)
	}
	if desktop.General.HotkeyMode != HotkeyBehaviorHoldToTalk {
		t.Fatalf("legacy hotkey_mode alias = %q, want %q", desktop.General.HotkeyMode, HotkeyBehaviorHoldToTalk)
	}
}
