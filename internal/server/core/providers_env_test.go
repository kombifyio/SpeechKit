//go:build linux

package core

import (
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestAnyCloudKeyEnvSetTrueWhenOpenAIIsSet(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("GOOGLE_AI_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("HF_TOKEN", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("GOOGLE_STT_API_KEY", "")
	t.Setenv("SPEECHKIT_GOOGLE_STT_API_KEY", "")
	t.Setenv("SPEECHKIT_GOOGLE_STT_CREDENTIALS_JSON", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("DEEPGRAM_API_KEY", "")
	t.Setenv("ASSEMBLYAI_API_KEY", "")

	if !anyCloudKeyEnvSet(&config.Config{}) {
		t.Fatalf("expected anyCloudKeyEnvSet=true when OPENAI_API_KEY is set")
	}
}

func TestAnyCloudKeyEnvSetFalseWhenAllUnset(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GOOGLE_AI_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("HF_TOKEN", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("GOOGLE_STT_API_KEY", "")
	t.Setenv("SPEECHKIT_GOOGLE_STT_API_KEY", "")
	t.Setenv("SPEECHKIT_GOOGLE_STT_CREDENTIALS_JSON", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("DEEPGRAM_API_KEY", "")
	t.Setenv("ASSEMBLYAI_API_KEY", "")

	if anyCloudKeyEnvSet(&config.Config{}) {
		t.Fatalf("expected anyCloudKeyEnvSet=false when no cloud key env is set")
	}
}

func TestAnyCloudKeyEnvSetFalseOnNilConfig(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GOOGLE_AI_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("HF_TOKEN", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("GOOGLE_STT_API_KEY", "")
	t.Setenv("SPEECHKIT_GOOGLE_STT_API_KEY", "")
	t.Setenv("SPEECHKIT_GOOGLE_STT_CREDENTIALS_JSON", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("DEEPGRAM_API_KEY", "")
	t.Setenv("ASSEMBLYAI_API_KEY", "")

	if anyCloudKeyEnvSet(nil) {
		t.Fatalf("expected anyCloudKeyEnvSet=false on nil config with no env")
	}
}

func TestAnyCloudKeyEnvSetRespectsConfigEnvOverride(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GOOGLE_AI_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("HF_TOKEN", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("GOOGLE_STT_API_KEY", "")
	t.Setenv("SPEECHKIT_GOOGLE_STT_API_KEY", "")
	t.Setenv("SPEECHKIT_GOOGLE_STT_CREDENTIALS_JSON", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("DEEPGRAM_API_KEY", "")
	t.Setenv("ASSEMBLYAI_API_KEY", "")
	t.Setenv("CUSTOM_OPENAI_KEY", "sk-override")

	cfg := &config.Config{}
	cfg.Providers.OpenAI.APIKeyEnv = "CUSTOM_OPENAI_KEY"
	if !anyCloudKeyEnvSet(cfg) {
		t.Fatalf("expected anyCloudKeyEnvSet=true when cfg names a non-default env that is set")
	}
}
