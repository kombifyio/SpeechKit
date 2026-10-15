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
	if err := save(path, cfg); err != nil {
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

// A negative TTL is a typo, not a policy: loading fails rather than guessing
// whether it meant "no TTL" or "delete everything".
func TestLoadRejectsNegativeRetentionTTL(t *testing.T) {
	path := writePrivacyTestConfig(t, "[privacy]\nretention_ttl_days = -1\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load must fail for a negative retention_ttl_days")
	}
}

// Under ephemeral only a provider surface that asserts no-store is admitted;
// a surface whose posture is an account setting, or that has no manifest at
// all, is refused with the stable reason. Under retain everything is allowed.
func TestProviderAllowedUnderRetentionFollowsTheNoStoreManifest(t *testing.T) {
	ephemeral := &Config{}
	ephemeral.Privacy.RetentionScope = RetentionScopeEphemeral
	for _, tc := range []struct {
		provider, modality string
		want               bool
	}{
		{"deepgram", "stt", true},
		{"local", "stt", true},
		{"openai", "stt", false},
		{"no-such-provider", "stt", false},
	} {
		allowed, reason := ephemeral.ProviderAllowedUnderRetention(tc.provider, tc.modality)
		if allowed != tc.want {
			t.Errorf("%s/%s allowed = %v, want %v", tc.provider, tc.modality, allowed, tc.want)
		}
		if !allowed && reason != framework.RetentionScopeReasonProviderRetains {
			t.Errorf("%s/%s reason = %q, want the provider_retains reason", tc.provider, tc.modality, reason)
		}
	}
	if allowed, _ := (&Config{}).ProviderAllowedUnderRetention("openai", "stt"); !allowed {
		t.Error("the retain scope must not refuse a provider")
	}
}
