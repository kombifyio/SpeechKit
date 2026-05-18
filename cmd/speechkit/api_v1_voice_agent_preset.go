package main

import (
	_ "embed"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/kombifyio/SpeechKit/internal/auditlog"
	"github.com/kombifyio/SpeechKit/internal/config"
)

//go:embed presets/enterprise-onprem.toml
var enterpriseOnpremPreset []byte

//go:embed presets/enterprise-cloud-byok.toml
var enterpriseCloudByokPreset []byte

type voiceAgentApplyProfileRequest struct {
	Profile string `json:"profile"`
}

type voiceAgentApplyProfileResponse struct {
	Applied bool   `json:"applied"`
	Profile string `json:"profile"`
}

// handleVoiceAgentApplyProfile applies an enterprise Voice Agent preset to the
// current config file and updates runtime state. The preset is read from the
// embedded TOML bytes, merged into the existing config (preserving fields not
// covered by the preset), and persisted via config.Save. A settings.changed
// audit event is emitted with key_path = "preset_applied".
//
// POST /api/v1/voice-agent/apply-profile
// Body: {"profile": "on-prem" | "cloud-byok"}
func handleVoiceAgentApplyProfile(cfgPath string, cfg *config.Config, state *appState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req voiceAgentApplyProfileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		profile := strings.TrimSpace(req.Profile)
		var presetData []byte
		switch profile {
		case "on-prem":
			presetData = enterpriseOnpremPreset
		case "cloud-byok":
			presetData = enterpriseCloudByokPreset
		default:
			http.Error(w, "unknown profile: must be \"on-prem\" or \"cloud-byok\"", http.StatusBadRequest)
			return
		}

		if cfg == nil {
			http.Error(w, "config unavailable", http.StatusServiceUnavailable)
			return
		}

		// Parse the preset TOML into a temporary Config (starting from defaults)
		// then overlay only the fields the preset explicitly sets. We do this by
		// decoding the TOML and checking which keys are defined so that customer
		// values (e.g. local.whisper_model_path) are preserved.
		presetCfg := &config.Config{}
		meta, err := toml.Decode(string(presetData), presetCfg)
		if err != nil {
			slog.Error("voice-agent preset decode failed", "profile", profile, "err", err)
			http.Error(w, "preset decode error", http.StatusInternalServerError)
			return
		}

		oldCfg := *cfg
		applyPresetOverlay(cfg, presetCfg, meta)

		if err := config.Save(cfgPath, cfg); err != nil {
			slog.Error("voice-agent preset save failed", "profile", profile, "err", err)
			http.Error(w, "config save error", http.StatusInternalServerError)
			return
		}

		// Update in-memory runtime state so the change takes effect without restart.
		if state != nil {
			state.applyRuntimeSettings(
				oldCfg, *cfg,
				cfg.General.DictateEnabled,
				cfg.General.AssistEnabled,
				cfg.General.VoiceAgentEnabled,
				cfg.General.DictateHotkey,
				cfg.General.AssistHotkey,
				cfg.General.VoiceAgentHotkey,
				cfg.General.DictateHotkeyBehavior,
				cfg.General.AssistHotkeyBehavior,
				cfg.General.VoiceAgentHotkeyBehavior,
				cfg.General.ActiveMode,
				cfg.Audio.DeviceID,
				nil, // providers — no STT router available here; caller can refresh separately
				cfg.UI.Visualizer,
				cfg.UI.Design,
				cfg.UI.AssistOverlayMode,
				cfg.UI.VoiceAgentOverlayMode,
				cfg.UI.OverlayPosition,
				cfg.Vocabulary.Dictionary,
				cfg.UI.OverlayMovable,
				cfg.UI.OverlayFreeX,
				cfg.UI.OverlayFreeY,
				cfg.UI.OverlayMonitorPositions,
			)
		}

		// Emit audit event — errors are non-fatal per audit-log convention.
		if err := auditlog.AppendEvent(r.Context(), auditlog.Record{
			Event: auditlog.EventSettingsChanged,
			Resource: map[string]any{
				"key_path": "preset_applied",
				"profile":  profile,
			},
		}); err != nil {
			slog.Warn("voice-agent preset audit event failed", "err", err)
		}

		writeJSON(w, voiceAgentApplyProfileResponse{Applied: true, Profile: profile})
	}
}

// applyPresetOverlay overlays only the TOML keys explicitly set in the preset
// onto dst, leaving all other dst fields untouched. This preserves
// customer-specific values (API keys, model paths, hotkeys, etc.) that the
// preset intentionally leaves commented-out.
func applyPresetOverlay(dst, src *config.Config, meta toml.MetaData) {
	if meta.IsDefined("update", "enabled") {
		dst.Update.Enabled = src.Update.Enabled
	}
	if meta.IsDefined("telemetry", "update_check") {
		dst.Telemetry.UpdateCheck = src.Telemetry.UpdateCheck
	}
	if meta.IsDefined("logging", "max_file_size_mb") {
		dst.Logging.MaxFileSizeMB = src.Logging.MaxFileSizeMB
	}
	if meta.IsDefined("logging", "max_files") {
		dst.Logging.MaxFiles = src.Logging.MaxFiles
	}
	if meta.IsDefined("audit", "enabled") {
		dst.Audit.Enabled = src.Audit.Enabled
	}
	if meta.IsDefined("audit", "retention_days") {
		dst.Audit.RetentionDays = src.Audit.RetentionDays
	}
	if meta.IsDefined("routing", "strategy") {
		dst.Routing.Strategy = src.Routing.Strategy
	}
	if meta.IsDefined("voice_agent", "provider") {
		dst.VoiceAgent.Provider = src.VoiceAgent.Provider
	}
	if meta.IsDefined("voice_agent", "model") {
		dst.VoiceAgent.Model = src.VoiceAgent.Model
	}
	if meta.IsDefined("providers", "google", "enabled") {
		dst.Providers.Google.Enabled = src.Providers.Google.Enabled
	}
	// Note: api_key is intentionally never overwritten by a preset — the customer
	// fills it in separately. The cloud-byok preset's [providers.google] section
	// is a placeholder comment in the TOML and does not set any fields.
}

// registerVoiceAgentPresetRoute wires POST /api/v1/voice-agent/apply-profile
// into the given mux. Called from registerAPIV1Routes.
func registerVoiceAgentPresetRoute(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState) {
	mux.Handle("/api/v1/voice-agent/apply-profile", handleVoiceAgentApplyProfile(cfgPath, cfg, state))
}
