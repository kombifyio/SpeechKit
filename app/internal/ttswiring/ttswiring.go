// Package ttswiring resolves a config.Config into the neutral
// tts.EnabledProviders input consumed by tts.BuildRouter. It is the single
// config→provider-options resolution path shared by the Device-Target
// (app/cmd/speechkit) and the Server-Target (app/internal/server/core), which
// previously each carried a near-identical copy.
//
// Secret resolution and per-provider default policy live here; router assembly
// and model_selection pinning stay in tts.BuildRouter.
package ttswiring

import (
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/app/internal/config"
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/ttsroute"
)

// geminiTTSModel returns the Gemini API speech model the Google voice output
// runs on, or "" for Cloud Text-to-Speech: an explicit [tts.google] model
// naming a Gemini TTS model wins, and selecting the Gemini TTS profile implies
// the default one.
func geminiTTSModel(cfg *config.Config) string {
	model := strings.TrimSpace(cfg.TTS.Google.Model)
	if strings.HasPrefix(strings.ToLower(model), "gemini-") {
		return model
	}
	if ttsroute.PreferredProvider(cfg.ModelSelection.TTS.PrimaryProfileID) == "gemini" {
		return tts.GeminiTTSDefaultModel
	}
	return ""
}

// geminiTTSVoice keeps a Gemini prebuilt voice name ("Kore") and drops a Cloud
// Text-to-Speech voice ("de-DE-Chirp3-HD-Kore"), which the Gemini API rejects,
// so the provider default applies.
func geminiTTSVoice(voice string) string {
	voice = strings.TrimSpace(voice)
	if strings.Contains(voice, "-") {
		return ""
	}
	return voice
}

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
//   - OpenAI model:  TTS.OpenAI.Model → Providers.OpenAI.TTSModel → "gpt-4o-mini-tts"
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
	// The provider allowlist and the retention scope: a refused provider is
	// never constructed, whatever its [tts] toggle says. adapter is the
	// option-manifest identifier.
	allowed := func(adapter string, enabled bool) bool {
		if !enabled {
			return false
		}
		ok, reason := cfg.ProviderConstructionAllowed(adapter, provideropts.ModalityTTS)
		if !ok {
			notes = append(notes, "Policy: TTS provider "+adapter+" not constructed ["+reason+"]")
		}
		return ok
	}

	if cloudAllowed && allowed("openai", cfg.TTS.OpenAI.Enabled) {
		if apiKey := providerCredential(cfg, "openai"); apiKey != "" {
			enabled.OpenAI = &tts.OpenAIOpts{
				APIKey: apiKey,
				Model:  firstNonEmpty(cfg.TTS.OpenAI.Model, cfg.Providers.OpenAI.TTSModel, catalogDefaultModel("openai", framework.ModeTTS)),
				Voice:  firstNonEmpty(cfg.TTS.OpenAI.Voice, cfg.Providers.OpenAI.TTSVoice, cfg.TTS.Voice, "nova"),
			}
		}
	}

	if cloudAllowed && cfg.TTS.Google.Enabled {
		if apiKey := providerCredential(cfg, "google"); apiKey != "" {
			if model := geminiTTSModel(cfg); model != "" && allowed("gemini", true) {
				enabled.Gemini = &tts.GeminiOpts{
					APIKey: apiKey,
					Model:  model,
					Voice:  geminiTTSVoice(firstNonEmpty(cfg.TTS.Google.Voice, cfg.TTS.Voice)),
				}
			} else if model == "" && allowed("google", true) {
				enabled.Google = &tts.GoogleOpts{
					APIKey: apiKey,
					Voice:  firstNonEmpty(cfg.TTS.Google.Voice, cfg.TTS.Voice),
				}
			}
		}
	}

	// Deepgram Aura reuses the shared DEEPGRAM_API_KEY (no separate credential).
	if cloudAllowed && allowed("deepgram", cfg.TTS.Deepgram.Enabled) {
		if key := providerCredential(cfg, "deepgram"); key != "" {
			enabled.Deepgram = &tts.DeepgramOpts{
				APIKey: key,
				Model:  firstNonEmpty(cfg.TTS.Deepgram.Voice, cfg.TTS.Deepgram.Model),
			}
		}
	}

	if cloudAllowed && allowed("huggingface", cfg.TTS.HuggingFace.Enabled) {
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
	if cloudAllowed && allowed("foundry", cfg.TTS.Foundry.Enabled) {
		apiKey := providerCredential(cfg, "foundry")
		token := auth.FoundryBearerToken
		if !cfg.FoundryUsesEntra() {
			token = nil
		}
		if apiKey != "" || token != nil {
			foundry := cfg.Providers.Foundry
			if foundry.TTSEngine() == config.FoundryEngineSpeech {
				host, err := config.FoundrySpeechHost(cfg.FoundryProjectEndpoint())
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
				baseURL, err := config.FoundryOpenAIBase(cfg.FoundryProjectEndpoint())
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

	// xAI Grok text-to-speech is opt-in BYOK and never a default or a
	// fallback: the [providers.xai] switch only makes xAI available, and the
	// voice is registered only when the user also named a tts.xai.* profile as
	// the primary or fallback of [model_selection.tts]. Enabling xAI for
	// Dictation or the Voice Agent therefore never makes Grok speak Assist
	// replies.
	if cfg.ModelSelection.TTS.NamesProvider("xai") {
		switch {
		case !cfg.Providers.XAI.Enabled:
			notes = append(notes, "TTS: xAI is selected but [providers.xai] is not enabled; skipping")
		case cloudAllowed && allowed("xai", true):
			if apiKey := providerCredential(cfg, "xai"); apiKey != "" {
				enabled.XAI = &tts.XAIOpts{
					APIKey: apiKey,
					Voice:  firstNonEmpty(cfg.Providers.XAI.TTSVoice, config.DefaultXAITTSVoice),
				}
			} else {
				notes = append(notes, "TTS: xAI is selected but no XAI_API_KEY is stored")
			}
		}
	}

	if allowed("piper", cfg.TTS.Piper.Enabled) {
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

	// A self-hosted OpenAI-compatible server (Kokoro-FastAPI, Speaches) is a
	// Local Provider: allowed in the open and local_network scopes, and pinned
	// to local addresses whenever the scope is restricted.
	if strings.TrimSpace(cfg.TTS.Local.URL) != "" && allowed("openedai", cfg.TTS.Local.Enabled) {
		scope := cfg.NetworkScope()
		if allowed, _ := scope.AllowsProviderKind(framework.ProviderKindLocalProvider); !allowed {
			notes = append(notes, "Privacy: self-hosted TTS server suspended (network scope "+string(scope)+")")
		} else {
			enabled.SelfHostedOpenAI = &tts.SelfHostedOpenAIOpts{
				BaseURL:      strings.TrimSpace(cfg.TTS.Local.URL),
				APIKey:       config.ResolveSecret(cfg.TTS.Local.APIKeyEnv),
				Model:        selfHostedTTSModel(cfg.TTS.Local.Model),
				Voice:        strings.TrimSpace(cfg.TTS.Local.Voice),
				RequireLocal: scope.Restricted(),
			}
		}
	}

	enabled.PreferredProfileID = strings.TrimSpace(cfg.ModelSelection.TTS.PrimaryProfileID)
	return enabled, notes
}

// selfHostedTTSModel maps the legacy [tts.local] model default (a Hugging Face
// repo id) to the model name OpenAI-compatible Kokoro servers accept.
func selfHostedTTSModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" || strings.EqualFold(model, "hexgrad/Kokoro-82M") {
		return "kokoro"
	}
	return model
}

// catalogDefaultModel is the catalog's default model id for a provider and
// mode, read as data so a vendor retirement never needs a code change here.
func catalogDefaultModel(provider string, mode framework.Mode) string {
	def, _ := catalog.FindProviderDefault(provider, mode)
	return def.ModelID
}
