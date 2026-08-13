package config

// Deepgram Voice Agent listen/speak leg resolution.

import "strings"

// DeepgramAudioSettings holds the resolved Deepgram Voice Agent listen/speak
// selection for the Server- and Device-Target wiring to apply to the kernel
// provider via DeepgramLive.ConfigureAudio. Empty/zero fields mean "keep the
// kernel default".
type DeepgramAudioSettings struct {
	ListenModel       string
	SpeakModel        string
	SpeakSpeed        float64
	EOTThreshold      float64
	EagerEOTThreshold float64
	EOTTimeoutMs      int
}

// DeepgramAudioConfig resolves the Deepgram Voice Agent audio legs from config.
// Explicit deepgram_listen_model / deepgram_speak_model win; otherwise the
// catalog's "listen+speak" composite in [voice_agent].model supplies them, so
// selecting a model profile actually reaches the provider. A [voice_agent].model
// naming a think LLM is ignored here — DeepgramThinkConfig owns that value.
func (cfg *Config) DeepgramAudioConfig() DeepgramAudioSettings {
	if cfg == nil {
		return DeepgramAudioSettings{}
	}
	va := cfg.VoiceAgent
	out := DeepgramAudioSettings{
		ListenModel:       strings.TrimSpace(va.DeepgramListenModel),
		SpeakModel:        strings.TrimSpace(va.DeepgramSpeakModel),
		SpeakSpeed:        va.DeepgramSpeakSpeed,
		EOTThreshold:      va.DeepgramListenEOTThreshold,
		EagerEOTThreshold: va.DeepgramListenEagerEOTThreshold,
		EOTTimeoutMs:      va.DeepgramListenEOTTimeoutMs,
	}
	listen, speak := splitDeepgramAudioModelID(va.Model)
	if out.ListenModel == "" {
		out.ListenModel = listen
	}
	if out.SpeakModel == "" {
		out.SpeakModel = speak
	}
	return out
}

// splitDeepgramAudioModelID pulls the listen and speak legs out of a
// [voice_agent].model value. It accepts the catalog composite
// ("flux-general-multi+aura-2") and a bare listen model ("flux-general-en").
// Values naming a think LLM yield no legs.
//
// A speak leg is only returned when it names a specific voice: the historical
// composites end in the family name "aura-2", which is not a model id, so it is
// dropped and the kernel's locale-aware Aura-2 default applies.
func splitDeepgramAudioModelID(model string) (listen, speak string) {
	model = strings.TrimSpace(model)
	if model == "" || !isDeepgramAudioModelID(model) {
		return "", ""
	}
	listen, speak, _ = strings.Cut(model, "+")
	listen = strings.TrimSpace(listen)
	speak = strings.TrimSpace(speak)
	if !isDeepgramListenModelID(listen) {
		listen = ""
	}
	if !isDeepgramVoiceID(speak) {
		speak = ""
	}
	return listen, speak
}

// isDeepgramListenModelID reports whether a value names a Deepgram STT model.
func isDeepgramListenModelID(model string) bool {
	lower := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(lower, "nova") || strings.HasPrefix(lower, "flux-general")
}

// isDeepgramVoiceID reports whether a value names a specific Deepgram TTS voice
// ("aura-2-thalia-en", "aura-asteria-en", "flux-kit-en") rather than a family
// name ("aura-2").
func isDeepgramVoiceID(model string) bool {
	lower := strings.ToLower(strings.TrimSpace(model))
	if !strings.HasPrefix(lower, "aura") && !strings.HasPrefix(lower, "flux-") {
		return false
	}
	return strings.Count(lower, "-") >= 2
}
