package config

import (
	"os"
	"path/filepath"
	"testing"
)

// An install upgraded past the 2026-09-30 retirement must not dial a retired
// model: every persisted retired id loads as its successor, while Foundry
// deployment names, which the user owns, keep their saved spelling and an
// empty one stays on the deployment the install was already calling.
func TestLoadMigratesRetiredModelsToSuccessors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	saved := `
[providers.openai]
stt_model = "whisper-1"
assist_model = "gpt-5.4-2026-03-05"
tts_model = "tts-1-hd"

[providers.groq]
utility_model = "llama-3.1-8b-instant"

[providers.google]
agent_model = "gemini-2.5-pro"

[providers.foundry]
enabled = true
assist_deployment = "gpt-5.6-terra"
realtime_deployment = ""
voicelive_model = "gpt-realtime-2"

[voice_agent]
provider = "gemini"
model = "gemini-3.1-flash-live-preview"

[model_selection.tts]
fallback_profile_id = "tts.openai.tts-1-hd"
`
	if err := os.WriteFile(path, []byte(saved), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for field, got := range map[string]string{
		"gpt-transcribe":             cfg.Providers.OpenAI.STTModel,
		"gpt-6.1-sol":                cfg.Providers.OpenAI.AssistModel,
		"gpt-4o-mini-tts":            cfg.Providers.OpenAI.TTSModel,
		"openai/gpt-oss-20b":         cfg.Providers.Groq.UtilityModel,
		"gemini-3.8-flash":           cfg.Providers.Google.AgentModel,
		"gpt-realtime-2.1":           cfg.Providers.Foundry.VoiceLiveModel,
		"gemini-3.8-live":            cfg.VoiceAgent.Model,
		"tts.openai.gpt-4o-mini-tts": cfg.ModelSelection.TTS.FallbackProfileID,
		"gpt-5.6-terra":              cfg.Providers.Foundry.AssistDeployment,
		"gpt-realtime-2":             cfg.Providers.Foundry.RealtimeDeployment,
	} {
		if got != field {
			t.Errorf("loaded %q, want %q", got, field)
		}
	}
}

// Speech-to-Text v2 rejects API keys, so Chirp 3 is usable with
// service-account credentials alone and never with only an API key.
func TestGoogleSTTUsableRequiresServiceAccountForChirp3(t *testing.T) {
	cfg := &Config{}
	cfg.Providers.Google.STTModel = "chirp_3"
	cfg.Providers.Google.STTAPIKeyEnv = "SPEECHKIT_TEST_GOOGLE_STT_KEY"
	cfg.Providers.Google.STTCredentialsJSONEnv = "SPEECHKIT_TEST_GOOGLE_STT_SA"
	cfg.Providers.Google.ApplicationCredentialsEnv = "SPEECHKIT_TEST_GOOGLE_ADC"

	t.Setenv("SPEECHKIT_TEST_GOOGLE_STT_KEY", "key-only")
	if _, ok := GoogleSTTUsable(cfg); ok {
		t.Fatal("chirp_3 with only an API key reported usable")
	}
	t.Setenv("SPEECHKIT_TEST_GOOGLE_STT_KEY", "")
	t.Setenv("SPEECHKIT_TEST_GOOGLE_STT_SA", `{"type":"service_account"}`)
	if _, ok := GoogleSTTUsable(cfg); !ok {
		t.Fatal("chirp_3 with service-account credentials reported unusable")
	}
}
