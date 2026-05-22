package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/assist/skills/voice_companion"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/tts"
)

// registerVoiceCompanionRoutes adds the HomeAssistant probe + Piper
// voice-listing endpoints that back the v0.38.2 Settings UI sections.
// Both endpoints are GET/POST-only and read-only against cfg (no
// persistence).
func registerVoiceCompanionRoutes(mux *http.ServeMux, cfg *config.Config) {
	mux.HandleFunc("/settings/homeassistant/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		writeJSON(w, runHomeAssistantProbe(r.Context(), r, cfg))
	})
	mux.HandleFunc("/api/tts/piper/voices", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, listPiperVoicesForSettings(r, cfg))
	})
}

// homeAssistantProbeResult is the JSON-encoded reply to
// /settings/homeassistant/test. OK=true means the probe succeeded;
// Error carries a human-readable message on failure.
type homeAssistantProbeResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	URL   string `json:"url,omitempty"`
}

// runHomeAssistantProbe builds a HomeAssistantSkill from form values
// (falling back to cfg) and runs Probe. The form fields match the
// /settings/update encoding so the UI can test an unsaved entry by
// posting the draft values directly.
func runHomeAssistantProbe(ctx context.Context, r *http.Request, cfg *config.Config) homeAssistantProbeResult {
	if err := r.ParseForm(); err != nil {
		return homeAssistantProbeResult{OK: false, Error: "could not parse request body"}
	}
	cfgURL := ""
	cfgTokenEnv := ""
	if cfg != nil {
		cfgURL = cfg.Assist.HomeAssistant.URL
		cfgTokenEnv = cfg.Assist.HomeAssistant.TokenEnv
	}
	url := strings.TrimSpace(r.FormValue("homeassistant_url"))
	if url == "" {
		url = strings.TrimSpace(cfgURL)
	}
	tokenEnv := strings.TrimSpace(r.FormValue("homeassistant_token_env"))
	if tokenEnv == "" {
		tokenEnv = strings.TrimSpace(cfgTokenEnv)
	}
	if url == "" {
		return homeAssistantProbeResult{OK: false, Error: "homeassistant_url is required"}
	}
	if tokenEnv == "" {
		return homeAssistantProbeResult{OK: false, Error: "homeassistant_token_env is required (env var name holding the long-lived access token)"}
	}
	token := strings.TrimSpace(config.ResolveSecret(tokenEnv))
	if token == "" {
		return homeAssistantProbeResult{OK: false, Error: "token env " + tokenEnv + " is not set; export the long-lived access token first"}
	}
	skill := voice_companion.NewHomeAssistantSkill(url, token)
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := skill.Probe(probeCtx); err != nil {
		return homeAssistantProbeResult{OK: false, Error: err.Error(), URL: url}
	}
	return homeAssistantProbeResult{OK: true, URL: url}
}

// piperVoiceListResult is the JSON-encoded reply to
// /api/tts/piper/voices. VoiceDir mirrors the directory that was
// scanned so the UI can show "(no voices found in <dir>)" when the
// list is empty.
type piperVoiceListResult struct {
	VoiceDir string              `json:"voiceDir"`
	Voices   []piperVoiceSummary `json:"voices"`
	Error    string              `json:"error,omitempty"`
	Defaults map[string]string   `json:"defaults"`
}

// listPiperVoicesForSettings reads VoiceDir from the form (?voice_dir=...)
// or falls back to cfg.TTS.Piper.VoiceDir. The endpoint never persists;
// the UI uses it before save to preview which voices a candidate dir
// would expose.
func listPiperVoicesForSettings(r *http.Request, cfg *config.Config) piperVoiceListResult {
	voiceDir := strings.TrimSpace(r.URL.Query().Get("voice_dir"))
	defaults := map[string]string{}
	if cfg != nil {
		if voiceDir == "" {
			voiceDir = strings.TrimSpace(cfg.TTS.Piper.VoiceDir)
		}
		for k, v := range cfg.TTS.Piper.DefaultVoices {
			defaults[k] = v
		}
	}
	if voiceDir == "" {
		return piperVoiceListResult{Voices: []piperVoiceSummary{}, Defaults: defaults}
	}
	voices, err := tts.ListPiperVoices(voiceDir)
	if err != nil {
		return piperVoiceListResult{VoiceDir: voiceDir, Voices: []piperVoiceSummary{}, Error: err.Error(), Defaults: defaults}
	}
	summaries := make([]piperVoiceSummary, 0, len(voices))
	for _, v := range voices {
		summaries = append(summaries, piperVoiceSummary{
			Filename: v.Filename,
			Locale:   v.Locale,
			Region:   v.Region,
			Name:     v.Name,
			Quality:  v.Quality,
			SizeKB:   v.SizeKB,
		})
	}
	return piperVoiceListResult{VoiceDir: voiceDir, Voices: summaries, Defaults: defaults}
}
