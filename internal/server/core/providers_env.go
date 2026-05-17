//go:build linux

package core

import (
	"github.com/kombifyio/SpeechKit/internal/config"
)

// anyCloudKeyEnvSet reports whether any cloud-provider env key
// resolves to a non-empty value at the current point in time. Used by
// /v1/deployment/status's providers.cloud_keys_present field and by
// the install-E2E local-only gate.
//
// The set of keys mirrors what scripts/install-server.sh's
// --strict-local-only flag refuses to write into .env:
// GOOGLE_AI_API_KEY (via Google.APIKeyEnv or default),
// the Google STT key env, OPENAI_API_KEY, GROQ_API_KEY, HF_TOKEN, and
// OPENROUTER_API_KEY.
func anyCloudKeyEnvSet(cfg *config.Config) bool {
	if cfg == nil {
		cfg = &config.Config{}
	}
	envs := []string{
		firstNonEmpty(cfg.Providers.Google.APIKeyEnv, config.GoogleAIAPIKeyEnv),
		config.GoogleSTTAPIKeyEnvName(cfg),
		firstNonEmpty(cfg.Providers.OpenAI.APIKeyEnv, "OPENAI_API_KEY"),
		firstNonEmpty(cfg.Providers.Groq.APIKeyEnv, "GROQ_API_KEY"),
		config.HuggingFaceTokenEnvName(cfg),
		firstNonEmpty(cfg.Providers.OpenRouter.APIKeyEnv, "OPENROUTER_API_KEY"),
	}
	for _, name := range envs {
		if envPresent(name) {
			return true
		}
	}
	return false
}
