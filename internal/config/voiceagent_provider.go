package config

import "strings"

// Voice Agent default-provider derivation.
//
// Session serving reads cfg.VoiceAgent.Provider (internal/server/core
// buildVoiceAgentHandler) and never consults ModelSelection.VoiceAgent, so
// the helpers below are the single source of truth for "which provider serves
// a default Voice Agent session". The catalog readiness surface marks the
// voice_agent profile Active from this same derivation so the ops display
// never asserts a backend that is not the serving one, on vanilla and
// kombify-default deployments alike (kombify-SpeechKit-5nt5).

// NormalizeVoiceAgentProviderName maps public aliases and catalog profile IDs
// onto the canonical Voice Agent provider names ("gemini", "deepgram",
// "assemblyai", "openai", "cascaded"). Unknown names pass through lower-cased
// with underscores dashed so unknown-provider errors echo a stable spelling.
// Canonical table — internal/server/core and the WebSocket adapter delegate
// here instead of keeping drift-prone copies.
func NormalizeVoiceAgentProviderName(provider string) string {
	name := strings.ToLower(strings.TrimSpace(provider))
	name = strings.ReplaceAll(name, "_", "-")
	switch name {
	case "google", "gemini", "gemini-live", "google-live", "realtime.google.gemini-native-audio":
		return "gemini"
	case "deepgram", "deepgram-agent", "deepgram-live", "realtime.deepgram.voice-agent":
		return "deepgram"
	case "assemblyai", "assembly-ai", "assemblyai-agent", "assemblyai-live", "realtime.assemblyai.voice-agent":
		return "assemblyai"
	case "openai", "openai-realtime", "openai-live", "realtime.openai.gpt-realtime-2":
		return "openai"
	case "cascaded", "local-cascaded", "pipeline", "pipeline-fallback", "voice-agent-cascaded", "voice-agent-cascaded-pipeline":
		return "cascaded"
	default:
		return name
	}
}

// EffectiveVoiceAgentProvider returns the normalized provider that serves a
// Voice Agent session when the client does not request one explicitly:
// cfg.VoiceAgent.Provider, with empty defaulting to Gemini Live. Keep in
// lockstep with internal/server/core/voiceagent_wiring.go, which consumes
// this for the serving default.
func EffectiveVoiceAgentProvider(cfg *Config) string {
	name := ""
	if cfg != nil {
		name = NormalizeVoiceAgentProviderName(cfg.VoiceAgent.Provider)
	}
	if name == "" {
		name = "gemini"
	}
	return name
}

// voiceAgentProfileIDByProvider mirrors the realtime profile IDs in
// pkg/speechkit/catalog.go, same convention as the kombify overlay constants
// in kombify_defaults.go.
var voiceAgentProfileIDByProvider = map[string]string{
	"gemini":     "realtime.google.gemini-native-audio",
	"deepgram":   "realtime.deepgram.voice-agent",
	"assemblyai": "realtime.assemblyai.voice-agent",
	"openai":     "realtime.openai.gpt-realtime-2",
	"cascaded":   "realtime.builtin.pipeline",
}

// EffectiveVoiceAgentProfileID maps the effective default provider onto its
// catalog profile ID. Providers without a catalog profile (e.g. the moshi
// stub) return "" — no voice_agent profile is marked Active for them.
func EffectiveVoiceAgentProfileID(cfg *Config) string {
	return voiceAgentProfileIDByProvider[EffectiveVoiceAgentProvider(cfg)]
}
