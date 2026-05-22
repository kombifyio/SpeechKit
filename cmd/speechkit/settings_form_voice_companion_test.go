package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestParseVoiceCompanionForm_PreservesCfgWhenFieldsOmitted is the
// regression test for the v0.38.2 Settings UI additions: a partial
// POST (such as the onboarding wizard or an unrelated section update)
// must not clear HA/Piper settings the user has previously configured.
//
// This guards the same regression class as
// TestParseSettingsFormPreservesOverlayEnabledWhenFieldOmitted.
func TestParseVoiceCompanionForm_PreservesCfgWhenFieldsOmitted(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Assist.HomeAssistant.URL = "https://ha.example.com:8123"
	cfg.Assist.HomeAssistant.TokenEnv = "HA_LL_TOKEN"
	cfg.Assist.HomeAssistant.Language = "de"
	cfg.TTS.Piper.Enabled = true
	cfg.TTS.Piper.Binary = "/usr/local/bin/piper"
	cfg.TTS.Piper.VoiceDir = "/var/lib/piper-voices"
	cfg.TTS.Piper.TimeoutSec = 45
	cfg.TTS.Piper.DefaultVoices = map[string]string{
		"en": "en_US-amy-medium.onnx",
		"de": "de_DE-thorsten-medium.onnx",
	}

	body := url.Values{
		// Wizard-style POST that does not include any HA or Piper fields.
		"dictate_hotkey":     {"ctrl+win"},
		"assist_hotkey":      {"win+alt"},
		"voice_agent_hotkey": {"ctrl+shift"},
	}
	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = req.ParseForm()

	form, errMsg := parseSettingsForm(req, cfg)
	if errMsg != "" {
		t.Fatalf("parseSettingsForm error: %s", errMsg)
	}
	if form.HomeAssistantURL != "https://ha.example.com:8123" {
		t.Errorf("HomeAssistantURL = %q, want preserved", form.HomeAssistantURL)
	}
	if form.HomeAssistantTokenEnv != "HA_LL_TOKEN" {
		t.Errorf("HomeAssistantTokenEnv = %q, want preserved", form.HomeAssistantTokenEnv)
	}
	if form.HomeAssistantLanguage != "de" {
		t.Errorf("HomeAssistantLanguage = %q, want preserved", form.HomeAssistantLanguage)
	}
	if !form.PiperEnabled {
		t.Error("PiperEnabled = false, want preserved true")
	}
	if form.PiperBinary != "/usr/local/bin/piper" {
		t.Errorf("PiperBinary = %q, want preserved", form.PiperBinary)
	}
	if form.PiperVoiceDir != "/var/lib/piper-voices" {
		t.Errorf("PiperVoiceDir = %q, want preserved", form.PiperVoiceDir)
	}
	if form.PiperTimeoutSec != 45 {
		t.Errorf("PiperTimeoutSec = %d, want preserved 45", form.PiperTimeoutSec)
	}
	if got, want := form.PiperDefaultVoices["en"], "en_US-amy-medium.onnx"; got != want {
		t.Errorf("PiperDefaultVoices[en] = %q, want %q", got, want)
	}
	if got, want := form.PiperDefaultVoices["de"], "de_DE-thorsten-medium.onnx"; got != want {
		t.Errorf("PiperDefaultVoices[de] = %q, want %q", got, want)
	}
}

// TestParseVoiceCompanionForm_ExplicitOverrides confirms posting new
// values updates the form data.
func TestParseVoiceCompanionForm_ExplicitOverrides(t *testing.T) {
	cfg := defaultTestConfig()

	body := url.Values{
		"homeassistant_url":       {"https://ha.lan:8123"},
		"homeassistant_token_env": {"HA_TOKEN"},
		"homeassistant_language":  {"en"},
		"piper_enabled":           {"1"},
		"piper_binary":            {"piper"},
		"piper_voice_dir":         {"D:/piper/voices"},
		"piper_timeout_sec":       {"60"},
		"piper_default_voice[en]": {"en_GB-alba-medium.onnx"},
		"piper_default_voice[de]": {"de_DE-thorsten-high.onnx"},
		"dictate_hotkey":          {"ctrl+win"},
		"assist_hotkey":           {"win+alt"},
		"voice_agent_hotkey":      {"ctrl+shift"},
	}
	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = req.ParseForm()

	form, errMsg := parseSettingsForm(req, cfg)
	if errMsg != "" {
		t.Fatalf("parseSettingsForm error: %s", errMsg)
	}
	if form.HomeAssistantURL != "https://ha.lan:8123" {
		t.Errorf("HomeAssistantURL = %q", form.HomeAssistantURL)
	}
	if form.HomeAssistantTokenEnv != "HA_TOKEN" {
		t.Errorf("HomeAssistantTokenEnv = %q", form.HomeAssistantTokenEnv)
	}
	if form.HomeAssistantLanguage != "en" {
		t.Errorf("HomeAssistantLanguage = %q", form.HomeAssistantLanguage)
	}
	if !form.PiperEnabled {
		t.Error("PiperEnabled = false, want true after explicit POST")
	}
	if form.PiperBinary != "piper" {
		t.Errorf("PiperBinary = %q", form.PiperBinary)
	}
	if form.PiperVoiceDir != "D:/piper/voices" {
		t.Errorf("PiperVoiceDir = %q", form.PiperVoiceDir)
	}
	if form.PiperTimeoutSec != 60 {
		t.Errorf("PiperTimeoutSec = %d", form.PiperTimeoutSec)
	}
	if form.PiperDefaultVoices["en"] != "en_GB-alba-medium.onnx" {
		t.Errorf("PiperDefaultVoices[en] = %q", form.PiperDefaultVoices["en"])
	}
	if form.PiperDefaultVoices["de"] != "de_DE-thorsten-high.onnx" {
		t.Errorf("PiperDefaultVoices[de] = %q", form.PiperDefaultVoices["de"])
	}
}

// TestParseVoiceCompanionForm_EmptyValueClearsDefaultVoice confirms an
// explicit empty value removes the per-locale entry.
func TestParseVoiceCompanionForm_EmptyValueClearsDefaultVoice(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.TTS.Piper.DefaultVoices = map[string]string{
		"en": "en_US-amy-medium.onnx",
		"de": "de_DE-thorsten-medium.onnx",
	}
	body := url.Values{
		"piper_default_voice[en]": {""},
		"dictate_hotkey":          {"ctrl+win"},
		"assist_hotkey":           {"win+alt"},
		"voice_agent_hotkey":      {"ctrl+shift"},
	}
	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = req.ParseForm()

	form, errMsg := parseSettingsForm(req, cfg)
	if errMsg != "" {
		t.Fatalf("parseSettingsForm error: %s", errMsg)
	}
	if _, ok := form.PiperDefaultVoices["en"]; ok {
		t.Error("expected en entry to be removed by empty POST value")
	}
	if form.PiperDefaultVoices["de"] != "de_DE-thorsten-medium.onnx" {
		t.Errorf("PiperDefaultVoices[de] = %q, want preserved", form.PiperDefaultVoices["de"])
	}
}
