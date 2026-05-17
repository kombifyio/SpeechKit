package config

import (
	"errors"
	"os/exec"
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

func TestApplyManagedDevServerDefaultsSeedsTargets(t *testing.T) {
	restore := OverrideManagedDevServerBuildForTests("1")
	defer restore()

	cfg := defaults()
	if len(cfg.ServerConnection.Targets) != 0 {
		t.Fatalf("precondition: default Targets must be empty, got %d", len(cfg.ServerConnection.Targets))
	}

	changed := ApplyManagedDevServerDefaults(cfg)
	if !changed {
		t.Fatal("ApplyManagedDevServerDefaults must return true when seeding presets into an empty Targets list")
	}

	wantIDs := []string{"kombify-origin", "kombify-gateway", "huggingface-inference"}
	if got, want := len(cfg.ServerConnection.Targets), len(wantIDs); got != want {
		t.Fatalf("seeded %d targets, want %d", got, want)
	}
	for i, want := range wantIDs {
		if cfg.ServerConnection.Targets[i].ID != want {
			t.Errorf("target[%d].ID = %q, want %q", i, cfg.ServerConnection.Targets[i].ID, want)
		}
	}

	for _, target := range cfg.ServerConnection.Targets {
		if target.URL == "" {
			t.Errorf("preset %q has empty URL", target.ID)
		}
		if target.BearerTokenEnv == "" {
			t.Errorf("preset %q has empty BearerTokenEnv", target.ID)
		}
		if !target.FallbackToLocal {
			t.Errorf("preset %q must default to FallbackToLocal=true so users don't get silently stuck offline", target.ID)
		}
	}
}

func TestApplyManagedDevServerDefaultsIsIdempotent(t *testing.T) {
	restore := OverrideManagedDevServerBuildForTests("1")
	defer restore()

	cfg := defaults()
	if !ApplyManagedDevServerDefaults(cfg) {
		t.Fatal("first call must return true (seeding from empty)")
	}
	firstCount := len(cfg.ServerConnection.Targets)

	if ApplyManagedDevServerDefaults(cfg) {
		t.Error("second call must return false — presets are already present, nothing to seed")
	}
	if got := len(cfg.ServerConnection.Targets); got != firstCount {
		t.Errorf("Targets count changed on second call: was %d, now %d", firstCount, got)
	}
}

func TestApplyManagedDevServerDefaultsKeepsUserCustomisations(t *testing.T) {
	restore := OverrideManagedDevServerBuildForTests("1")
	defer restore()

	cfg := defaults()
	// User edited the kombify-origin preset URL to point at a staging host.
	// The seeder must NOT overwrite it on subsequent runs.
	cfg.ServerConnection.Targets = []ServerConnectionTargetConfig{
		{
			ID:                "kombify-origin",
			Label:             "User custom label",
			URL:               "https://staging.speechkit.kombify.io",
			BearerTokenEnv:    "STAGING_TOKEN",
			AuthMode:          ServerConnectionAuthModeBearer,
			FallbackToLocal:   false,
			RequestTimeoutSec: 45,
		},
	}

	ApplyManagedDevServerDefaults(cfg)

	if len(cfg.ServerConnection.Targets) < 3 {
		t.Fatalf("missing presets are still added; got %d targets, want at least 3", len(cfg.ServerConnection.Targets))
	}
	// User's customised kombify-origin must be left alone.
	if got := cfg.ServerConnection.Targets[0].URL; got != "https://staging.speechkit.kombify.io" {
		t.Errorf("user-customised target was overwritten: URL = %q", got)
	}
	if got := cfg.ServerConnection.Targets[0].BearerTokenEnv; got != "STAGING_TOKEN" {
		t.Errorf("user-customised BearerTokenEnv was overwritten: got %q", got)
	}
}

func TestApplyManagedDevServerDefaultsNoOpForOSSBuild(t *testing.T) {
	restore := OverrideManagedDevServerBuildForTests("0")
	defer restore()

	cfg := defaults()
	if ApplyManagedDevServerDefaults(cfg) {
		t.Error("OSS build must not seed kombify presets — managedDevServerBuildEnabled=0 gates them off")
	}
	if got := len(cfg.ServerConnection.Targets); got != 0 {
		t.Errorf("OSS build seeded %d targets, want 0", got)
	}
}
