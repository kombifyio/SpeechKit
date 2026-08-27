package config

import (
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/secrets"
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestProviderRuntimeRegistryCoversFrameworkMatrix(t *testing.T) {
	for _, row := range framework.DefaultProviderMatrix() {
		if _, ok := ProviderRuntimeFor(row.Provider); !ok {
			t.Fatalf("provider runtime registry missing framework provider %q", row.Provider)
		}
	}
	if got := len(ProviderRuntimes()); got < 10 {
		t.Fatalf("provider runtime registry has %d providers, want at least 10", got)
	}
}

func TestProviderCredentialMetadataNormalizesGoogleSTT(t *testing.T) {
	cfg := &Config{}
	cfg.Providers.OpenAI.APIKeyEnv = "CUSTOM_OPENAI_KEY"
	cfg.Providers.Google.STTAPIKeyEnv = "CUSTOM_GOOGLE_STT_KEY"

	if got, want := NormalizeProviderCredentialTarget("stt.google.chirp-3"), "google_stt"; got != want {
		t.Fatalf("NormalizeProviderCredentialTarget = %q, want %q", got, want)
	}
	if got, want := ProviderForCredentialTarget("google-stt"), "google"; got != want {
		t.Fatalf("ProviderForCredentialTarget = %q, want %q", got, want)
	}
	if got, want := ProviderCredentialEnvName(cfg, "google_stt"), "CUSTOM_GOOGLE_STT_KEY"; got != want {
		t.Fatalf("ProviderCredentialEnvName(google_stt) = %q, want %q", got, want)
	}
	if got, want := ProviderCredentialEnvName(cfg, "openai"), "CUSTOM_OPENAI_KEY"; got != want {
		t.Fatalf("ProviderCredentialEnvName(openai) = %q, want %q", got, want)
	}
	if got, want := ProviderLabel("google_stt"), "Google Speech-to-Text"; got != want {
		t.Fatalf("ProviderLabel(google_stt) = %q, want %q", got, want)
	}
}

func TestProviderCredentialAvailableUsesCentralResolvers(t *testing.T) {
	disableDopplerForCredentialTest(t)
	restore := secrets.UseMemoryStoreForTests()
	defer restore()

	t.Setenv(GoogleAIAPIKeyEnv, "gemini-key")
	t.Setenv(GoogleSTTDefaultAPIKeyEnv, "")
	t.Setenv(GoogleCloudSTTAPIKeyEnv, "cloud-stt-key")
	t.Setenv(GoogleLegacySTTAPIKeyEnv, "")
	t.Setenv("CUSTOM_DEEPGRAM_KEY", "deepgram-key")

	cfg := &Config{}
	cfg.Providers.Google.APIKeyEnv = GoogleAIAPIKeyEnv
	cfg.Providers.Deepgram.APIKeyEnv = "CUSTOM_DEEPGRAM_KEY"

	if !ProviderCredentialAvailable(cfg, "google_stt") {
		t.Fatal("google_stt credential should resolve from Google STT fallback env candidates")
	}
	status := ProviderCredentialStatusFor(cfg, "google_stt")
	if !status.Available || status.EnvName != GoogleCloudSTTAPIKeyEnv {
		t.Fatalf("google_stt status = %+v, want available from %s", status, GoogleCloudSTTAPIKeyEnv)
	}
	if !ProviderCredentialAvailable(cfg, "deepgram") {
		t.Fatal("deepgram credential should resolve from configured env")
	}
}

func TestProviderCredentialAvailableForHuggingFaceIsNotManagedBuildGated(t *testing.T) {
	disableDopplerForCredentialTest(t)
	restore := secrets.UseMemoryStoreForTests()
	defer restore()
	restoreBuild := OverrideManagedHuggingFaceBuildForTests("0")
	defer restoreBuild()

	t.Setenv("HF_TOKEN", "hf-key")
	cfg := &Config{}
	cfg.HuggingFace.TokenEnv = "HF_TOKEN"

	if !ProviderCredentialAvailable(cfg, "huggingface") {
		t.Fatal("generic Hugging Face credential resolution should not require the desktop managed build flag")
	}
	status := ProviderCredentialStatusFor(cfg, "huggingface")
	if !status.Available || status.EnvName != "HF_TOKEN" || status.Source == string(secrets.TokenSourceNone) {
		t.Fatalf("huggingface credential status = %+v, want available from HF_TOKEN", status)
	}
}

func TestSetProviderEnabledUsesProviderRuntimeDefaults(t *testing.T) {
	cfg := &Config{}
	if err := SetProviderEnabled(cfg, "open-router", true); err != nil {
		t.Fatalf("enable openrouter: %v", err)
	}
	if !cfg.Providers.OpenRouter.Enabled {
		t.Fatal("openrouter provider should be enabled")
	}
	if got, want := cfg.Providers.OpenRouter.STTModel, "openai/whisper-1"; got != want {
		t.Fatalf("openrouter stt model = %q, want %q", got, want)
	}

	err := SetProviderEnabled(cfg, "not-a-provider", true)
	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("unsupported provider error = %v, want ErrUnsupportedProvider", err)
	}
}

func TestSetProviderEnabledAssemblyAIFillsNativeLLM(t *testing.T) {
	cfg := &Config{}
	if err := SetProviderEnabled(cfg, "assemblyai", true); err != nil {
		t.Fatalf("enable assemblyai: %v", err)
	}
	if !cfg.Providers.AssemblyAI.Enabled {
		t.Fatal("assemblyai should be enabled")
	}
	if !cfg.Providers.AssemblyAI.StreamingLLM {
		t.Fatal("assemblyai should keep streaming LLM on")
	}
	if cfg.Providers.AssemblyAI.LLMGatewayUtilityModel != DefaultAssemblyAILLMGatewayUtilityModel {
		t.Fatalf("utility model = %q", cfg.Providers.AssemblyAI.LLMGatewayUtilityModel)
	}
}
