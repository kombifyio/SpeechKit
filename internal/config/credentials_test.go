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
