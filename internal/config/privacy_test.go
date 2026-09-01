package config

import (
	"os"
	"path/filepath"
	"testing"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func writePrivacyTestConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadDefaultsToOpenScope(t *testing.T) {
	path := writePrivacyTestConfig(t, "[general]\nlanguage = \"en\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.NetworkScope(); got != framework.NetworkScopeOpen {
		t.Fatalf("scope = %q, want open", got)
	}
	if !cfg.SetupTrafficAllowed() {
		t.Fatal("setup traffic must be allowed in open scope")
	}
}

func TestLoadRejectsUnknownNetworkScope(t *testing.T) {
	path := writePrivacyTestConfig(t, "[privacy]\nnetwork_scope = \"lan\"\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load must fail for an unknown network_scope value")
	}
}

func TestLoadRestrictedScopeRoundTrip(t *testing.T) {
	path := writePrivacyTestConfig(t, "[privacy]\nnetwork_scope = \"Device_Only\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.NetworkScope(); got != framework.NetworkScopeDeviceOnly {
		t.Fatalf("scope = %q, want device_only", got)
	}
	if cfg.SetupTrafficAllowed() {
		t.Fatal("setup traffic must default to blocked in restricted scopes")
	}

	// Save + reload keeps the canonical value and does not mutate any other
	// suspended settings (suspension semantics: config values persist).
	cfg.ServerConnection.Enabled = true
	cfg.ServerConnection.URL = "https://public.example.com"
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := reloaded.NetworkScope(); got != framework.NetworkScopeDeviceOnly {
		t.Fatalf("reloaded scope = %q, want device_only", got)
	}
	if !reloaded.ServerConnection.Enabled || reloaded.ServerConnection.URL == "" {
		t.Fatal("restricted scope must suspend, not erase, the server connection config")
	}
}

func TestSetupTrafficOptIn(t *testing.T) {
	path := writePrivacyTestConfig(t, "[privacy]\nnetwork_scope = \"local_network\"\nallow_setup_traffic = true\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.SetupTrafficAllowed() {
		t.Fatal("allow_setup_traffic = true must permit setup traffic in local_network")
	}
}
