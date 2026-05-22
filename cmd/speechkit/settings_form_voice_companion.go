package main

import (
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
)

// parseVoiceCompanionSettingsForm extracts the HomeAssistant +
// Piper TTS fields from a settings POST. All boolean and string
// reads go through the *FormValue helpers so a partial POST (e.g.
// from the onboarding wizard) preserves existing cfg values instead
// of silently clearing them — see feedback memory
// "form-value-fallback".
//
// PiperDefaultVoices is parsed from any form field of the shape
// "piper_default_voice[<locale>]" so the UI can post a map without
// a custom encoding. Empty values remove the locale entry; missing
// locales preserve whatever cfg already has.
func parseVoiceCompanionSettingsForm(req *http.Request, cfg *config.Config, f *settingsFormData) {
	if req == nil || cfg == nil || f == nil {
		return
	}

	f.HomeAssistantURL = valueOrDefault(trimmedFormValue(req, "homeassistant_url"), cfg.Assist.HomeAssistant.URL)
	f.HomeAssistantTokenEnv = valueOrDefault(trimmedFormValue(req, "homeassistant_token_env"), cfg.Assist.HomeAssistant.TokenEnv)
	f.HomeAssistantLanguage = valueOrDefault(trimmedFormValue(req, "homeassistant_language"), cfg.Assist.HomeAssistant.Language)

	f.PiperEnabled = boolFormValue(req, "piper_enabled", cfg.TTS.Piper.Enabled)
	f.PiperBinary = valueOrDefault(trimmedFormValue(req, "piper_binary"), cfg.TTS.Piper.Binary)
	f.PiperVoiceDir = valueOrDefault(trimmedFormValue(req, "piper_voice_dir"), cfg.TTS.Piper.VoiceDir)
	f.PiperTimeoutSec = nonNegativeIntFormValue(req, "piper_timeout_sec", cfg.TTS.Piper.TimeoutSec)

	f.PiperDefaultVoices = parsePiperDefaultVoiceMap(req, cfg.TTS.Piper.DefaultVoices)
}

// parsePiperDefaultVoiceMap walks all req form keys matching
// "piper_default_voice[<locale>]" and produces a merged map of
// per-locale voice filenames. When the form omits a locale that
// already exists in cfg, the existing value is preserved. An empty
// value explicitly clears the entry.
func parsePiperDefaultVoiceMap(req *http.Request, current map[string]string) map[string]string {
	out := make(map[string]string, len(current))
	for k, v := range current {
		if strings.TrimSpace(v) != "" {
			out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	if req.Form == nil {
		_ = req.ParseForm() //nolint:errcheck // saveSettings already ParseForm'd; this is defensive for direct callers
	}
	const prefix = "piper_default_voice["
	for key, values := range req.Form {
		if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, "]") {
			continue
		}
		locale := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(key, prefix), "]")))
		if locale == "" {
			continue
		}
		val := ""
		if len(values) > 0 {
			val = strings.TrimSpace(values[0])
		}
		if val == "" {
			delete(out, locale)
			continue
		}
		out[locale] = val
	}
	return out
}
