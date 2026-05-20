package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveGoogleSTTKeyDoesNotUseDefaultGoogleAIKey(t *testing.T) {
	disableDopplerForCredentialTest(t)
	t.Setenv(GoogleAIAPIKeyEnv, "gemini-key")
	t.Setenv(GoogleSTTDefaultAPIKeyEnv, "")
	t.Setenv(GoogleCloudSTTAPIKeyEnv, "")
	t.Setenv(GoogleLegacySTTAPIKeyEnv, "")

	cfg := &Config{}
	cfg.Providers.Google.APIKeyEnv = GoogleAIAPIKeyEnv

	key, source := ResolveGoogleSTTKey(cfg)
	if key != "" || source != "" {
		t.Fatalf("ResolveGoogleSTTKey() = (%q, %q), want empty when only GOOGLE_AI_API_KEY is set", key, source)
	}
}

func TestResolveGoogleSTTKeyPrefersDedicatedKey(t *testing.T) {
	disableDopplerForCredentialTest(t)
	t.Setenv(GoogleAIAPIKeyEnv, "gemini-key")
	t.Setenv(GoogleSTTDefaultAPIKeyEnv, "speech-key")
	t.Setenv(GoogleCloudSTTAPIKeyEnv, "cloud-key")
	t.Setenv(GoogleLegacySTTAPIKeyEnv, "legacy-key")

	cfg := &Config{}
	cfg.Providers.Google.APIKeyEnv = GoogleAIAPIKeyEnv

	key, source := ResolveGoogleSTTKey(cfg)
	if key != "speech-key" || source != GoogleSTTDefaultAPIKeyEnv {
		t.Fatalf("ResolveGoogleSTTKey() = (%q, %q), want dedicated key source", key, source)
	}
}

func TestResolveGoogleSTTKeyAllowsCustomNonGeminiAPIKeyEnv(t *testing.T) {
	disableDopplerForCredentialTest(t)
	t.Setenv(GoogleSTTDefaultAPIKeyEnv, "")
	t.Setenv(GoogleCloudSTTAPIKeyEnv, "")
	t.Setenv(GoogleLegacySTTAPIKeyEnv, "")
	t.Setenv("CUSTOM_GOOGLE_SPEECH_KEY", "custom-speech-key")

	cfg := &Config{}
	cfg.Providers.Google.APIKeyEnv = "CUSTOM_GOOGLE_SPEECH_KEY"

	key, source := ResolveGoogleSTTKey(cfg)
	if key != "custom-speech-key" || source != "CUSTOM_GOOGLE_SPEECH_KEY" {
		t.Fatalf("ResolveGoogleSTTKey() = (%q, %q), want custom speech key source", key, source)
	}
}

func disableDopplerForCredentialTest(t *testing.T) {
	t.Helper()
	previousLookPath := dopplerLookPath
	dopplerLookPath = func(string) (string, error) {
		return "", errors.New("doppler disabled for test: " + exec.ErrNotFound.Error())
	}
	t.Cleanup(func() {
		dopplerLookPath = previousLookPath
	})
}

func TestApplyManagedDevServerDefaultsDoesNotSeedTargets(t *testing.T) {
	cfg := defaults()
	if len(cfg.ServerConnection.Targets) != 0 {
		t.Fatalf("precondition: default Targets must be empty, got %d", len(cfg.ServerConnection.Targets))
	}

	if ApplyManagedDevServerDefaults(cfg) {
		t.Fatal("server targets must be user/operator configured, never seeded by managed defaults")
	}
	if got := len(cfg.ServerConnection.Targets); got != 0 {
		t.Fatalf("seeded %d targets, want 0", got)
	}
}

func TestApplyManagedDevServerDefaultsKeepsExplicitServerTarget(t *testing.T) {
	cfg := defaults()
	cfg.ServerConnection.Targets = []ServerConnectionTargetConfig{
		{
			ID:                "customer-server",
			Label:             "Customer server",
			URL:               "https://speechkit.customer.example.com",
			BearerTokenEnv:    "CUSTOMER_SPEECHKIT_TOKEN",
			AuthMode:          ServerConnectionAuthModeBearer,
			FallbackToLocal:   false,
			RequestTimeoutSec: 45,
		},
	}

	if ApplyManagedDevServerDefaults(cfg) {
		t.Fatal("explicit user targets must not be rewritten by managed defaults")
	}
	if got, want := len(cfg.ServerConnection.Targets), 1; got != want {
		t.Fatalf("targets = %d, want %d", got, want)
	}
	if got := cfg.ServerConnection.Targets[0].BearerTokenEnv; got != "CUSTOMER_SPEECHKIT_TOKEN" {
		t.Errorf("user-customised BearerTokenEnv was overwritten: got %q", got)
	}
}

func TestExampleConfigDoesNotShipManagedServerTargets(t *testing.T) {
	examplePath := filepath.Join("..", "..", "config.example.toml")
	cfg, err := Load(examplePath)
	if err != nil {
		t.Fatalf("load config.example.toml: %v", err)
	}
	if cfg.ServerConnection.URL != "" {
		t.Fatalf("config.example.toml server URL = %q, want empty", cfg.ServerConnection.URL)
	}
	if got := len(cfg.ServerConnection.Targets); got != 0 {
		t.Fatalf("config.example.toml ships %d server targets, want 0", got)
	}

	data, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read config.example.toml: %v", err)
	}
	raw := string(data)
	for _, forbidden := range []string{
		"speechkit" + ".kombify.io",
		"api" + ".kombify.io/v1/speechkit",
		"huggingface" + "-inference",
		"api-inference" + ".huggingface.co",
	} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("config.example.toml contains managed/private server target %q", forbidden)
		}
	}
}
