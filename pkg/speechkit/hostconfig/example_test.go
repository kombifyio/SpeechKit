package hostconfig_test

import (
	"fmt"
	"log"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/hostconfig"
)

// Example shows the one-call path from a config.toml on disk to the public
// ModeSettings and RuntimePolicy an embedding host drives the framework with.
func Example() {
	settings, policy, err := hostconfig.Load("config.toml")
	if err != nil {
		log.Fatal(err)
	}

	// settings tells the host which modes are on and which provider profiles
	// they use; policy is a starting point the host can tighten before
	// validating selections with speechkit.ValidateModeSettingsForPolicy.
	fmt.Println("dictation enabled:", settings.Dictation.Enabled)
	fmt.Println("enabled modes:", policy.EnabledModes)
}

// ExampleModeSettingsFrom converts a host-constructed Config — useful for
// hosts that load or synthesise configuration themselves. An empty primary
// selection is filled with the built-in default profile.
func ExampleModeSettingsFrom() {
	cfg := &hostconfig.Config{}
	cfg.General.AssistEnabled = true
	cfg.General.AssistHotkey = "ctrl+alt+a"

	settings := hostconfig.ModeSettingsFrom(cfg)
	fmt.Println(settings.Assist.Enabled, settings.Assist.Hotkey, settings.Assist.PrimaryProfileID)
	// Output: true ctrl+alt+a assist.builtin.gemma4-e4b
}

// ExampleParse decodes raw TOML bytes into the public Config. Unknown tables
// (the reference app's full config carries many more) are ignored.
func ExampleParse() {
	cfg, err := hostconfig.Parse([]byte(`
[general]
dictate_enabled = true
dictate_hotkey = "ctrl+alt+d"
`))
	if err != nil {
		log.Fatal(err)
	}

	settings := hostconfig.ModeSettingsFrom(cfg)
	fmt.Println(settings.Dictation.Enabled, settings.Dictation.Hotkey)
	// Output: true ctrl+alt+d
}
