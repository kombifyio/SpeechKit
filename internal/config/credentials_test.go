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

func TestResolveDeepgramKeyUsesConfiguredEnv(t *testing.T) {
	disableDopplerForCredentialTest(t)
	t.Setenv(DeepgramAPIKeyEnv, "")
	t.Setenv("CUSTOM_DEEPGRAM_KEY", "deepgram-key")

	cfg := &Config{}
	cfg.Providers.Deepgram.APIKeyEnv = "CUSTOM_DEEPGRAM_KEY"

	key, source := ResolveDeepgramKey(cfg)
	if key != "deepgram-key" || source != "CUSTOM_DEEPGRAM_KEY" {
		t.Fatalf("ResolveDeepgramKey() = (%q, %q)", key, source)
	}
}

func TestResolveAssemblyAIKeyUsesDefaultEnv(t *testing.T) {
	disableDopplerForCredentialTest(t)
	t.Setenv(AssemblyAIAPIKeyEnv, "assembly-key")

	key, source := ResolveAssemblyAIKey(&Config{})
	if key != "assembly-key" || source != AssemblyAIAPIKeyEnv {
		t.Fatalf("ResolveAssemblyAIKey() = (%q, %q)", key, source)
	}
}

func TestResolveDeepgramThinkKey(t *testing.T) {
	disableDopplerForCredentialTest(t)

	// No env name configured -> managed mode, no key, no error.
	if key, source := ResolveDeepgramThinkKey(&Config{}); key != "" || source != "" {
		t.Fatalf("ResolveDeepgramThinkKey(managed) = (%q, %q), want empty", key, source)
	}

	// Configured env name resolves the key and reports its source.
	t.Setenv("CUSTOM_THINK_KEY", "think-byo-key")
	cfg := &Config{}
	cfg.VoiceAgent.DeepgramThinkAPIKeyEnv = "CUSTOM_THINK_KEY"
	if key, source := ResolveDeepgramThinkKey(cfg); key != "think-byo-key" || source != "CUSTOM_THINK_KEY" {
		t.Fatalf("ResolveDeepgramThinkKey(byo) = (%q, %q)", key, source)
	}
}

func TestDeepgramThinkConfig(t *testing.T) {
	disableDopplerForCredentialTest(t)

	// Back-compat: a non-Gemini [voice_agent].model is reused as the think model;
	// no endpoint -> managed mode, no key.
	cfg := &Config{}
	cfg.VoiceAgent.Model = "gpt-4o-mini"
	if got := cfg.DeepgramThinkConfig(); got.Model != "gpt-4o-mini" || got.EndpointURL != "" || got.APIKey != "" {
		t.Fatalf("back-compat model reuse mismatch: %#v", got)
	}

	// A Gemini realtime model id must NOT leak into the Deepgram think model.
	cfg = &Config{}
	cfg.VoiceAgent.Model = "gemini-3.1-flash-live-preview"
	if got := cfg.DeepgramThinkConfig(); got.Model != "" {
		t.Fatalf("Gemini model must be ignored for Deepgram think, got %q", got.Model)
	}

	// Explicit think model wins over the realtime model; BYO endpoint resolves
	// the credential from the configured env.
	t.Setenv("CUSTOM_THINK_KEY", "think-byo-key")
	cfg = &Config{}
	cfg.VoiceAgent.Model = "gpt-4o-mini"
	cfg.VoiceAgent.DeepgramThinkProvider = "open_ai"
	cfg.VoiceAgent.DeepgramThinkModel = "gpt-4o"
	cfg.VoiceAgent.DeepgramThinkEndpointURL = "https://llm.example/v1"
	cfg.VoiceAgent.DeepgramThinkAPIKeyEnv = "CUSTOM_THINK_KEY"
	got := cfg.DeepgramThinkConfig()
	if got.Provider != "open_ai" || got.Model != "gpt-4o" || got.EndpointURL != "https://llm.example/v1" || got.APIKey != "think-byo-key" {
		t.Fatalf("explicit think config mismatch: %#v", got)
	}
}

func TestGoogleSTTCredentialEnvNamesUseDefaultsAndOverrides(t *testing.T) {
	if got := GoogleSTTCredentialsJSONEnvName(nil); got != GoogleSTTCredentialsJSONEnv {
		t.Fatalf("GoogleSTTCredentialsJSONEnvName(nil) = %q", got)
	}
	if got := GoogleApplicationCredentialsEnvName(nil); got != GoogleApplicationCredentialsEnv {
		t.Fatalf("GoogleApplicationCredentialsEnvName(nil) = %q", got)
	}
	cfg := &Config{}
	cfg.Providers.Google.STTCredentialsJSONEnv = "CUSTOM_GOOGLE_JSON"
	cfg.Providers.Google.ApplicationCredentialsEnv = "CUSTOM_GOOGLE_ADC"
	if got := GoogleSTTCredentialsJSONEnvName(cfg); got != "CUSTOM_GOOGLE_JSON" {
		t.Fatalf("GoogleSTTCredentialsJSONEnvName(cfg) = %q", got)
	}
	if got := GoogleApplicationCredentialsEnvName(cfg); got != "CUSTOM_GOOGLE_ADC" {
		t.Fatalf("GoogleApplicationCredentialsEnvName(cfg) = %q", got)
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
