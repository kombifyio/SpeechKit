package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
)

// parseWakewordSettingsForm fills f.Wakeword* from the posted form, falling
// back to the current cfg values when a field is missing. Robust against
// malformed numbers (uses cfg default) and case-insensitive enable toggles.
func parseWakewordSettingsForm(req *http.Request, cfg *config.Config, f *settingsFormData) {
	f.WakewordEnabled = boolFormValue(req, "wakeword_enabled", cfg.Wakeword.Enabled)
	f.WakewordBackend = config.NormalizeWakewordBackend(
		valueOrDefault(req.FormValue("wakeword_backend"), cfg.Wakeword.Backend),
	)
	f.WakewordPhraseID = strings.ToLower(strings.TrimSpace(
		valueOrDefault(req.FormValue("wakeword_phrase_id"), cfg.Wakeword.PhraseID),
	))
	f.WakewordDefaultMode = config.NormalizeWakewordDefaultMode(
		valueOrDefault(req.FormValue("wakeword_default_mode"), cfg.Wakeword.DefaultMode),
	)

	// Threshold: 0 is a valid "use catalog recommendation" sentinel, so
	// we don't fall back to cfg when the field is exactly 0 — only when
	// the field is absent or unparseable.
	thresholdRaw := strings.TrimSpace(req.FormValue("wakeword_threshold"))
	if thresholdRaw == "" {
		f.WakewordThreshold = cfg.Wakeword.Threshold
	} else if parsed, err := strconv.ParseFloat(thresholdRaw, 64); err == nil {
		f.WakewordThreshold = parsed
	} else {
		f.WakewordThreshold = cfg.Wakeword.Threshold
	}

	minRaw := strings.TrimSpace(req.FormValue("wakeword_min_consecutive_frames"))
	if minRaw == "" {
		f.WakewordMinConsecutiveFrames = cfg.Wakeword.MinConsecutiveFrames
	} else if parsed, err := strconv.Atoi(minRaw); err == nil && parsed >= 0 {
		f.WakewordMinConsecutiveFrames = parsed
	} else {
		f.WakewordMinConsecutiveFrames = cfg.Wakeword.MinConsecutiveFrames
	}

	cooldownRaw := strings.TrimSpace(req.FormValue("wakeword_cooldown_ms"))
	if cooldownRaw == "" {
		f.WakewordCooldownMs = cfg.Wakeword.CooldownMs
	} else if parsed, err := strconv.Atoi(cooldownRaw); err == nil && parsed >= 0 {
		f.WakewordCooldownMs = parsed
	} else {
		f.WakewordCooldownMs = cfg.Wakeword.CooldownMs
	}
}
