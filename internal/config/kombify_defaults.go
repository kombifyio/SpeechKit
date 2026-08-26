package config

import (
	"os"
	"strings"
)

// KombifyDeploymentDefaultsEnv opts the Server-Target into the kombify
// reference deployment defaults (Deepgram-first across all modes). The Device
// reference build enables the same overlay through the `kombify_reference`
// build tag instead of this env.
const KombifyDeploymentDefaultsEnv = "SPEECHKIT_KOMBIFY_DEFAULTS"

// Profile IDs the overlay pins. These mirror entries in
// pkg/speechkit/catalog.go and the Deepgram TTS / Voice Agent adapters.
const (
	kombifyDeepgramSTTProfileID = "stt.deepgram.nova-3"
	kombifyDeepgramTTSProfileID = "tts.deepgram.aura-2"
	kombifyDeepgramSTTModel     = "nova-3"
	kombifyDeepgramTTSModel     = "aura-2-thalia-en"
)

// KombifyDeploymentDefaultsRequested reports whether the Server-Target env opt-in
// is set. The Device reference build does not consult this — it calls
// ApplyKombifyDeploymentDefaults directly behind a build tag.
func KombifyDeploymentDefaultsRequested() bool {
	return parseManagedBool(os.Getenv(KombifyDeploymentDefaultsEnv))
}

// ApplyKombifyDeploymentDefaults makes Deepgram the first-level provider across
// all modes for kombify's own deployments — WITHOUT touching the OSS framework
// default constants. It only takes effect when a Deepgram API key resolves, so
// external builds that never set DEEPGRAM_API_KEY keep the local-first defaults.
//
// The overlay preserves the primary/secondary pattern: Deepgram becomes the
// primary profile and the previously-selected profile is demoted to the
// fallback. Returns human-readable notes for startup logging.
func ApplyKombifyDeploymentDefaults(cfg *Config) []string {
	if cfg == nil {
		return nil
	}
	key, env, _ := ResolveProviderCredentialValue(cfg, "deepgram")
	if strings.TrimSpace(env) == "" {
		env = ProviderCredentialEnvName(cfg, "deepgram")
	}
	if strings.TrimSpace(key) == "" {
		return []string{"kombify defaults: DEEPGRAM_API_KEY not resolved (env=" + env + "); Deepgram-first defaults skipped"}
	}

	var notes []string

	// ── STT (Dictation + the Assist/Voice-Agent STT leg share this router) ──
	if !cfg.Providers.Deepgram.Enabled {
		cfg.Providers.Deepgram.Enabled = true
		notes = append(notes, "kombify defaults: enabled Deepgram STT provider")
	}
	if strings.TrimSpace(cfg.Providers.Deepgram.STTModel) == "" {
		cfg.Providers.Deepgram.STTModel = kombifyDeepgramSTTModel
		cfg.Providers.Deepgram.STTSmartFormat = true
		cfg.Providers.Deepgram.STTNumerals = true
		cfg.Providers.Deepgram.STTUseVocabularyKeyterms = true
	}
	if setModePrimaryWithFallback(&cfg.ModelSelection.Dictate, kombifyDeepgramSTTProfileID, DefaultDictatePrimaryProfileID) {
		notes = append(notes, "kombify defaults: Dictate primary = "+kombifyDeepgramSTTProfileID)
	}

	// ── TTS (Aura-2 voice output for Assist + Voice Agent) ──
	if !cfg.TTS.Enabled {
		cfg.TTS.Enabled = true
	}
	if !cfg.TTS.Deepgram.Enabled {
		cfg.TTS.Deepgram.Enabled = true
		notes = append(notes, "kombify defaults: enabled Deepgram Aura TTS")
	}
	if strings.TrimSpace(cfg.TTS.Deepgram.Model) == "" {
		cfg.TTS.Deepgram.Model = kombifyDeepgramTTSModel
	}
	if setModePrimaryWithFallback(&cfg.ModelSelection.TTS, kombifyDeepgramTTSProfileID, DefaultTTSPrimaryProfileID) {
		notes = append(notes, "kombify defaults: TTS primary = "+kombifyDeepgramTTSProfileID)
	}

	// ── Voice Agent (Deepgram Voice Agent API, audio-to-audio) ──
	if p := strings.ToLower(strings.TrimSpace(cfg.VoiceAgent.Provider)); p == "" || p == "gemini" {
		cfg.VoiceAgent.Provider = "deepgram"
		notes = append(notes, "kombify defaults: Voice Agent provider = deepgram")
	}

	return notes
}

// setModePrimaryWithFallback pins primary as the mode's primary profile and, if
// no fallback is set yet, demotes the previous primary (or the supplied default)
// to the fallback slot. Returns true when the primary changed.
func setModePrimaryWithFallback(sel *ModeModelSelection, primary, fallbackDefault string) bool {
	if sel == nil || strings.TrimSpace(primary) == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(sel.PrimaryProfileID), primary) {
		return false
	}
	prev := strings.TrimSpace(sel.PrimaryProfileID)
	sel.PrimaryProfileID = primary
	if strings.TrimSpace(sel.FallbackProfileID) == "" {
		switch {
		case prev != "" && !strings.EqualFold(prev, primary):
			sel.FallbackProfileID = prev
		case strings.TrimSpace(fallbackDefault) != "":
			sel.FallbackProfileID = fallbackDefault
		}
	}
	if strings.TrimSpace(sel.ModeSource) == "" {
		sel.ModeSource = ModeSourceLocal
	}
	return true
}
