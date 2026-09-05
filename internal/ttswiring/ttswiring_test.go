package ttswiring

import (
	"context"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

// newCfg returns a config with TTS enabled and the named env-var pointers set
// so ResolveSecret/ResolveHuggingFaceToken find the keys exported by the test.
func newCfg() *config.Config {
	cfg := &config.Config{}
	cfg.TTS.Enabled = true
	return cfg
}

func TestResolveOpenAIModelVoicePrecedence(t *testing.T) {
	t.Setenv("TEST_OPENAI_KEY", "sk-test")

	cfg := newCfg()
	cfg.TTS.OpenAI.Enabled = true
	cfg.Providers.OpenAI.APIKeyEnv = "TEST_OPENAI_KEY"

	// 1) The TTS-specific fields win when set.
	cfg.TTS.OpenAI.Model = "tts-1-hd"
	cfg.TTS.OpenAI.Voice = "shimmer"
	enabled, _ := ResolveEnabledProviders(cfg)
	if enabled.OpenAI == nil {
		t.Fatal("OpenAI not enabled despite key + flag")
	}
	if enabled.OpenAI.Model != "tts-1-hd" || enabled.OpenAI.Voice != "shimmer" {
		t.Fatalf("got model=%q voice=%q, want tts-1-hd/shimmer", enabled.OpenAI.Model, enabled.OpenAI.Voice)
	}

	// 2) Falls back to provider-level fields, then to the literal defaults.
	cfg.TTS.OpenAI.Model = ""
	cfg.TTS.OpenAI.Voice = ""
	cfg.Providers.OpenAI.TTSModel = "tts-1"
	cfg.Providers.OpenAI.TTSVoice = "echo"
	enabled, _ = ResolveEnabledProviders(cfg)
	if enabled.OpenAI.Model != "tts-1" || enabled.OpenAI.Voice != "echo" {
		t.Fatalf("provider-level fallback: got model=%q voice=%q, want tts-1/echo", enabled.OpenAI.Model, enabled.OpenAI.Voice)
	}

	// 3) Global TTS.Voice is the last config tier before the literal default.
	cfg.Providers.OpenAI.TTSVoice = ""
	cfg.TTS.Voice = "fable"
	enabled, _ = ResolveEnabledProviders(cfg)
	if enabled.OpenAI.Voice != "fable" {
		t.Fatalf("global voice tier: got %q, want fable", enabled.OpenAI.Voice)
	}

	// 4) Everything empty → literal defaults.
	cfg.Providers.OpenAI.TTSModel = ""
	cfg.TTS.Voice = ""
	enabled, _ = ResolveEnabledProviders(cfg)
	if enabled.OpenAI.Model != "tts-1" || enabled.OpenAI.Voice != "nova" {
		t.Fatalf("literal default: got model=%q voice=%q, want tts-1/nova", enabled.OpenAI.Model, enabled.OpenAI.Voice)
	}
}

func TestResolveOpenAIDisabledWithoutKey(t *testing.T) {
	cfg := newCfg()
	cfg.TTS.OpenAI.Enabled = true
	cfg.Providers.OpenAI.APIKeyEnv = "TEST_OPENAI_KEY_UNSET"
	enabled, _ := ResolveEnabledProviders(cfg)
	if enabled.OpenAI != nil {
		t.Fatal("OpenAI should be nil when the key env resolves empty")
	}
}

func TestResolveGoogleVoiceFallback(t *testing.T) {
	t.Setenv("TEST_GOOGLE_KEY", "g-test")
	cfg := newCfg()
	cfg.TTS.Google.Enabled = true
	cfg.Providers.Google.APIKeyEnv = "TEST_GOOGLE_KEY"
	cfg.TTS.Voice = "de-DE-Neural2-B"
	enabled, _ := ResolveEnabledProviders(cfg)
	if enabled.Google == nil || enabled.Google.Voice != "de-DE-Neural2-B" {
		t.Fatalf("google voice fallback to TTS.Voice failed: %+v", enabled.Google)
	}
}

func TestResolveHuggingFaceModelDefault(t *testing.T) {
	t.Setenv("TEST_HF_TOKEN", "hf-test")
	cfg := newCfg()
	cfg.TTS.HuggingFace.Enabled = true
	cfg.HuggingFace.TokenEnv = "TEST_HF_TOKEN"
	enabled, _ := ResolveEnabledProviders(cfg)
	if enabled.HuggingFace == nil {
		t.Fatal("HuggingFace not enabled despite token + flag")
	}
	if enabled.HuggingFace.Model != "Qwen/Qwen3-TTS-12Hz-1.7B-Base" {
		t.Fatalf("HF default model = %q", enabled.HuggingFace.Model)
	}
}

func TestResolvePiperEmptyVoiceDirNote(t *testing.T) {
	cfg := newCfg()
	cfg.TTS.Piper.Enabled = true
	cfg.TTS.Piper.VoiceDir = ""
	enabled, notes := ResolveEnabledProviders(cfg)
	if enabled.Piper != nil {
		t.Fatal("Piper should be skipped when voice_dir is empty")
	}
	if len(notes) == 0 {
		t.Fatal("expected a note explaining the Piper skip")
	}
}

func TestResolvePreferredProfileIDTrimmed(t *testing.T) {
	cfg := newCfg()
	cfg.ModelSelection.TTS.PrimaryProfileID = "  tts.google.studio-o  "
	enabled, _ := ResolveEnabledProviders(cfg)
	if enabled.PreferredProfileID != "tts.google.studio-o" {
		t.Fatalf("PreferredProfileID = %q, want trimmed", enabled.PreferredProfileID)
	}
}

func TestResolveRestrictedScopeSuspendsCloudTTS(t *testing.T) {
	t.Setenv("TEST_OPENAI_KEY", "sk-test")

	cfg := newCfg()
	cfg.TTS.OpenAI.Enabled = true
	cfg.Providers.OpenAI.APIKeyEnv = "TEST_OPENAI_KEY"
	cfg.Privacy.NetworkScope = config.NetworkScopeDeviceOnly

	enabled, notes := ResolveEnabledProviders(cfg)
	if enabled.OpenAI != nil {
		t.Fatal("cloud TTS must be suspended in device_only scope")
	}
	if !cfg.TTS.OpenAI.Enabled {
		t.Fatal("suspension must not rewrite the stored config toggle")
	}
	joined := ""
	for _, n := range notes {
		joined += n + " "
	}
	if !strings.Contains(joined, "suspended") {
		t.Fatalf("notes should mention suspension, got %v", notes)
	}

	// Widening the scope resumes the unchanged config.
	cfg.Privacy.NetworkScope = config.NetworkScopeOpen
	enabled, _ = ResolveEnabledProviders(cfg)
	if enabled.OpenAI == nil {
		t.Fatal("cloud TTS should resume when the scope is widened")
	}
}

// A Microsoft sign-in has no resource key; the token source alone must
// register Foundry TTS, on whichever engine the voice selects.
func TestResolveFoundryEntraWithoutKey(t *testing.T) {
	cfg := newCfg()
	cfg.TTS.Foundry.Enabled = true
	cfg.Providers.Foundry.Enabled = true
	cfg.Providers.Foundry.ProjectEndpoint = "https://example.services.ai.azure.com/api/projects/demo"
	cfg.Providers.Foundry.APIKeyEnv = "TEST_UNSET_FOUNDRY_KEY"
	cfg.Providers.Foundry.AuthMode = config.FoundryAuthModeEntra
	cfg.Providers.Foundry.TTSDeployment = "my-openai-tts"
	token := func(ctx context.Context) (string, error) { return "t", nil }

	enabled, _ := ResolveEnabledProvidersWithAuth(cfg, Auth{FoundryBearerToken: token})
	if enabled.Foundry == nil || enabled.Foundry.BearerToken == nil {
		t.Fatalf("OpenAI-route Foundry TTS not registered with a token source: %+v", enabled.Foundry)
	}

	cfg.Providers.Foundry.TTSDeployment = "MAI-Voice-2"
	enabled, _ = ResolveEnabledProvidersWithAuth(cfg, Auth{FoundryBearerToken: token})
	if enabled.FoundrySpeech == nil || enabled.FoundrySpeech.BearerToken == nil {
		t.Fatalf("Speech-route Foundry TTS not registered with a token source: %+v", enabled.FoundrySpeech)
	}

	// Key mode ignores a stray token source and, without a key, registers nothing.
	cfg.Providers.Foundry.AuthMode = config.FoundryAuthModeAPIKey
	enabled, notes := ResolveEnabledProvidersWithAuth(cfg, Auth{FoundryBearerToken: token})
	if enabled.Foundry != nil || enabled.FoundrySpeech != nil {
		t.Fatal("key mode without a key must not register Foundry TTS")
	}
	if !strings.Contains(strings.Join(notes, "\n"), "no API key or Microsoft sign-in") {
		t.Fatalf("expected a note about the missing credential, got %v", notes)
	}
}
