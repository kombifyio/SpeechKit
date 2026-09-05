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
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func providerCredential(cfg *config.Config, target string) string {
	value, _, err := config.ResolveProviderCredentialValue(cfg, target)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
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
	return ResolveEnabledProvidersWithAuth(cfg, Auth{})
}

// Auth carries credentials a host mints at runtime rather than reads from
// the secret store. The Foundry token source replaces the resource key when
// [providers.foundry] auth_mode is "entra"; a nil source keeps the key path.
type Auth struct {
	FoundryBearerToken framework.BearerTokenFunc
}

// ResolveEnabledProvidersWithAuth is ResolveEnabledProviders with host-minted
// credentials. The Device-Target passes its Microsoft sign-in token source;
// the Server-Target has none yet and uses the plain variant.
func ResolveEnabledProvidersWithAuth(cfg *config.Config, auth Auth) (tts.EnabledProviders, []string) {
	var enabled tts.EnabledProviders
	var notes []string

	// Restricted network scopes suspend cloud TTS providers at assembly time;
	// the (unchanged) config toggles resume when the scope is widened again.
	// Piper below is a local engine and stays available in every scope.
	cloudAllowed, _ := cfg.NetworkScope().AllowsProviderKind(framework.ProviderKindCloudProvider)
	if !cloudAllowed {
		notes = append(notes, "Privacy: cloud TTS providers suspended (network scope "+string(cfg.NetworkScope())+")")
	}

	if cloudAllowed && cfg.TTS.OpenAI.Enabled {
		if apiKey := providerCredential(cfg, "openai"); apiKey != "" {
			enabled.OpenAI = &tts.OpenAIOpts{
				APIKey: apiKey,
				Model:  firstNonEmpty(cfg.TTS.OpenAI.Model, cfg.Providers.OpenAI.TTSModel, "tts-1"),
				Voice:  firstNonEmpty(cfg.TTS.OpenAI.Voice, cfg.Providers.OpenAI.TTSVoice, cfg.TTS.Voice, "nova"),
			}
		}
	}

	if cloudAllowed && cfg.TTS.Google.Enabled {
		if apiKey := providerCredential(cfg, "google"); apiKey != "" {
			enabled.Google = &tts.GoogleOpts{
				APIKey: apiKey,
				Voice:  firstNonEmpty(cfg.TTS.Google.Voice, cfg.TTS.Voice),
			}
		}
	}

	// Deepgram Aura reuses the shared DEEPGRAM_API_KEY (no separate credential).
	if cloudAllowed && cfg.TTS.Deepgram.Enabled {
		if key := providerCredential(cfg, "deepgram"); key != "" {
			enabled.Deepgram = &tts.DeepgramOpts{
				APIKey: key,
				Model:  firstNonEmpty(cfg.TTS.Deepgram.Voice, cfg.TTS.Deepgram.Model),
			}
		}
	}

	if cloudAllowed && cfg.TTS.HuggingFace.Enabled {
		if token := providerCredential(cfg, "huggingface"); token != "" {
			enabled.HuggingFace = &tts.HuggingFaceOpts{
				Token: token,
				Model: firstNonEmpty(cfg.TTS.HuggingFace.Model, "Qwen/Qwen3-TTS-12Hz-1.7B-Base"),
			}
		}
	}

	// Foundry TTS reuses the shared AZURE_AI_API_KEY and project endpoint from
	// [providers.foundry] (no separate credential). MAI-Voice deployments are
	// served by the resource's Azure Speech surface; everything else goes
	// through the OpenAI-compatible /audio/speech route.
	if cloudAllowed && cfg.TTS.Foundry.Enabled {
		apiKey := providerCredential(cfg, "foundry")
		token := auth.FoundryBearerToken
		if !cfg.Providers.Foundry.UsesEntra() {
			token = nil
		}
		if apiKey != "" || token != nil {
			foundry := cfg.Providers.Foundry
			if foundry.TTSEngine() == config.FoundryEngineSpeech {
				host, err := config.FoundrySpeechHost(foundry.ProjectEndpoint)
				if err != nil {
					notes = append(notes, "TTS: Microsoft Foundry enabled but project endpoint is invalid: "+err.Error())
				} else {
					enabled.FoundrySpeech = &tts.AzureSpeechOpts{
						Host:        host,
						APIKey:      apiKey,
						BearerToken: token,
						Voice:       firstNonEmpty(cfg.TTS.Foundry.Voice, foundry.ResolvedTTSVoice()),
						Style:       strings.TrimSpace(foundry.TTSStyle),
					}
				}
			} else {
				baseURL, err := config.FoundryOpenAIBase(foundry.ProjectEndpoint)
				if err != nil {
					notes = append(notes, "TTS: Microsoft Foundry enabled but project endpoint is invalid: "+err.Error())
				} else {
					enabled.Foundry = &tts.FoundryOpts{
						APIKey:      apiKey,
						BearerToken: token,
						BaseURL:     baseURL,
						Model:       firstNonEmpty(cfg.TTS.Foundry.Model, foundry.ResolvedTTSDeployment()),
						Voice:       firstNonEmpty(cfg.TTS.Foundry.Voice, foundry.TTSVoice, cfg.TTS.Voice, config.DefaultFoundryTTSVoice),
					}
				}
			}
		} else {
			notes = append(notes, "TTS: Microsoft Foundry enabled but no API key or Microsoft sign-in configured")
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
