// Package ttswiring resolves a config.Config into the neutral
// tts.EnabledProviders input consumed by tts.BuildRouter. It is the single
// config→provider-options resolution path shared by the Device-Target
// (cmd/speechkit) and the Server-Target (internal/server/core), which
// previously each carried a near-identical copy.
//
// Secret resolution and per-provider default policy live here; router assembly
// and model_selection pinning stay in tts.BuildRouter.
package ttswiring

import (
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/tts"
)

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// ResolveEnabledProviders inspects cfg.TTS and returns the providers that are
// both enabled and have a usable credential/config, plus human-readable notes
// for the startup log (callers that don't log notes can ignore them).
//
// Provider default policy is the union of what the two targets previously did,
// so neither regresses:
//   - OpenAI model:  TTS.OpenAI.Model → Providers.OpenAI.TTSModel → "tts-1"
//   - OpenAI voice:  TTS.OpenAI.Voice → Providers.OpenAI.TTSVoice → TTS.Voice → "nova"
//   - Google voice:  TTS.Google.Voice → TTS.Voice → Google default
//   - HF model:      TTS.HuggingFace.Model → Qwen3-TTS Base default
//
// The OpenAI/Google literal defaults match the provider constructors, so they
// are equivalent to passing the empty string; they are spelled out here only so
// the returned notes carry the effective voice/model.
func ResolveEnabledProviders(cfg *config.Config) (tts.EnabledProviders, []string) {
	var enabled tts.EnabledProviders
	var notes []string

	if cfg.TTS.OpenAI.Enabled {
		if apiKey := strings.TrimSpace(config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv)); apiKey != "" {
			enabled.OpenAI = &tts.OpenAIOpts{
				APIKey: apiKey,
				Model:  firstNonEmpty(cfg.TTS.OpenAI.Model, cfg.Providers.OpenAI.TTSModel, "tts-1"),
				Voice:  firstNonEmpty(cfg.TTS.OpenAI.Voice, cfg.Providers.OpenAI.TTSVoice, cfg.TTS.Voice, "nova"),
			}
		}
	}

	if cfg.TTS.Google.Enabled {
		if apiKey := strings.TrimSpace(config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)); apiKey != "" {
			enabled.Google = &tts.GoogleOpts{
				APIKey: apiKey,
				Voice:  firstNonEmpty(cfg.TTS.Google.Voice, cfg.TTS.Voice),
			}
		}
	}

	// Deepgram Aura reuses the shared DEEPGRAM_API_KEY (no separate credential).
	if cfg.TTS.Deepgram.Enabled {
		if key, _ := config.ResolveDeepgramKey(cfg); strings.TrimSpace(key) != "" {
			enabled.Deepgram = &tts.DeepgramOpts{
				APIKey: key,
				Model:  firstNonEmpty(cfg.TTS.Deepgram.Voice, cfg.TTS.Deepgram.Model),
			}
		}
	}

	if cfg.TTS.HuggingFace.Enabled {
		if token, _, err := config.ResolveHuggingFaceToken(cfg); err == nil && token != "" {
			enabled.HuggingFace = &tts.HuggingFaceOpts{
				Token: token,
				Model: firstNonEmpty(cfg.TTS.HuggingFace.Model, "Qwen/Qwen3-TTS-12Hz-1.7B-Base"),
			}
		}
	}

	if cfg.TTS.Piper.Enabled {
		if voiceDir := strings.TrimSpace(cfg.TTS.Piper.VoiceDir); voiceDir == "" {
			notes = append(notes, "TTS: Piper enabled but voice_dir is empty; skipping")
		} else {
			enabled.Piper = &tts.PiperOpts{
				Binary:        cfg.TTS.Piper.Binary,
				VoiceDir:      voiceDir,
				DefaultVoices: cfg.TTS.Piper.DefaultVoices,
				Timeout:       time.Duration(cfg.TTS.Piper.TimeoutSec) * time.Second,
			}
		}
	}

	enabled.PreferredProfileID = strings.TrimSpace(cfg.ModelSelection.TTS.PrimaryProfileID)
	return enabled, notes
}
