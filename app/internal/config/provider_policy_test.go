package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// A pinned Ollama endpoint may only be a loopback address. Any other value is
// rejected when the policy is read, and Ollama is then not built at all rather
// than falling back to the configured URL.
func TestPinnedNonLoopbackOllamaURLIsRejected(t *testing.T) {
	for _, tc := range []struct {
		pin     string
		allowed bool
	}{
		{pin: "http://ollama.corp.example:11434", allowed: false},
		{pin: "http://10.0.0.5:11434", allowed: false},
		{pin: "http://127.0.0.1:11434", allowed: true},
	} {
		managed := t.TempDir()
		writePlist(t, filepath.Join(managed, testBundleID+".plist"), `<key>Ollama.BaseURL</key><string>`+tc.pin+`</string>`)
		cfg := defaults()
		cfg.Providers.Ollama.BaseURL = "http://192.168.1.20:11434"

		AttachPolicy(cfg, readManagedPreferences(managed, testBundleID, nil))

		allowed, reason := cfg.ProviderAllowed("ollama")
		if allowed != tc.allowed {
			t.Fatalf("pin %s: Ollama allowed = %v (reason %q), want %v", tc.pin, allowed, reason, tc.allowed)
		}
		if !allowed && reason != framework.PolicyReasonProviderNotAllowed {
			t.Fatalf("pin %s: reason = %q", tc.pin, reason)
		}
		if allowed && cfg.OllamaBaseURL() != tc.pin {
			t.Fatalf("pin %s: Ollama dials %q", tc.pin, cfg.OllamaBaseURL())
		}
	}
}

// On a managed host config.toml is untrusted: a key outside the
// user-editable allowlist is neither applied nor written back, while an
// editable preference is kept.
func TestManagedModeIgnoresNonEditableConfigKey(t *testing.T) {
	SetManagedMode(true)
	t.Cleanup(func() { SetManagedMode(false) })
	previous := readPolicyValues
	readPolicyValues = func() PolicyValues {
		return singleSourcePolicy(PolicyValues{ProvidersAllowed: []string{"local", "foundry"}}, PolicySourceMachinePolicies)
	}
	t.Cleanup(func() { readPolicyValues = previous })

	path := filepath.Join(t.TempDir(), "config.toml")
	body := `
[general]
dictate_hotkey = "ctrl+alt+k"

[providers]
allowed = ["deepgram"]

[providers.foundry]
project_endpoint = "https://attacker.services.ai.azure.com"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers.Foundry.ProjectEndpoint != defaults().Providers.Foundry.ProjectEndpoint {
		t.Fatalf("a non-editable endpoint from config.toml was applied: %q", cfg.Providers.Foundry.ProjectEndpoint)
	}
	if ok, _ := cfg.ProviderAllowed("deepgram"); ok {
		t.Fatal("an allowlist in config.toml widened the machine policy")
	}
	if cfg.General.DictateHotkey != "ctrl+alt+k" {
		t.Fatalf("an editable preference was dropped: %q", cfg.General.DictateHotkey)
	}

	if err := save(path, cfg); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(written), "attacker") || strings.Contains(string(written), "project_endpoint") {
		t.Fatalf("a non-editable key was written back:\n%s", written)
	}
}
