// Package ttsroute holds the single source of truth that maps a Voice-Output
// profile ID (e.g. "tts.google.studio-o") to the provider name the TTS routers
// use internally (e.g. "google").
//
// It is a zero-dependency leaf package so that both the framework kernel
// (internal/tts) and the public embeddable surface (pkg/speechkit/tts) can
// share one mapping without violating the OSS boundary (pkg/** must not import
// internal/*). Keep the prefixes in sync with the TTS catalog entries.
package ttsroute

import "strings"

// PreferredProvider maps a Voice-Output profile ID to the provider name. It
// returns the empty string when the profile is unknown or unset — callers
// should treat that as "no preference, use default strategy ordering".
//
// Mapping:
//
//	tts.openai.*                        → "openai"
//	tts.google.*                        → "google"
//	tts.deepgram.*                      → "deepgram"
//	tts.huggingface.*                   → "huggingface"
//	tts.foundry.*                       → "foundry"
//	tts.openedai.* / tts.kokoro.*       → "kokoro"            (OpenAI-compatible self-hosted endpoint)
//	tts.local.kokoro*                   → "kokoro_local"      (ONNX in-process)
//	tts.local.supertonic*              → "supertonic_local"  (ONNX in-process, multilingual)
//	tts.local.chatterbox*              → "chatterbox_local"  (ONNX in-process, voice-clone)
//	tts.local.piper / tts.local.piper-* → "piper"             (Piper subprocess)
func PreferredProvider(profileID string) string {
	id := strings.TrimSpace(profileID)
	if id == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(id, "tts.openai."):
		return "openai"
	case strings.HasPrefix(id, "tts.google."):
		return "google"
	case strings.HasPrefix(id, "tts.deepgram."):
		return "deepgram"
	case strings.HasPrefix(id, "tts.huggingface."):
		return "huggingface"
	case strings.HasPrefix(id, "tts.foundry."):
		return "foundry"
	case strings.HasPrefix(id, "tts.openedai."), strings.HasPrefix(id, "tts.kokoro."):
		return "kokoro"
	case strings.HasPrefix(id, "tts.local.kokoro"):
		return "kokoro_local"
	case strings.HasPrefix(id, "tts.local.supertonic"):
		return "supertonic_local"
	case strings.HasPrefix(id, "tts.local.chatterbox"):
		return "chatterbox_local"
	case id == "tts.local.piper", strings.HasPrefix(id, "tts.local.piper-"):
		return "piper"
	default:
		return ""
	}
}
