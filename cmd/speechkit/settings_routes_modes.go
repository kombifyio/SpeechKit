package main

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func registerModeRoutes(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState) {
	registerActiveModeRoute(mux, cfgPath, cfg, state)
	registerModeEnabledRoute(mux, cfgPath, cfg, state)
}

func registerActiveModeRoute(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState) {
	mux.HandleFunc("/mode/active", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			activeMode := currentActiveMode(cfg, state)
			writeJSON(w, map[string]string{"activeMode": activeMode})
		case http.MethodPost:
			updateActiveMode(w, r, cfgPath, cfg, state)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func updateActiveMode(w http.ResponseWriter, r *http.Request, cfgPath string, cfg *config.Config, state *appState) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	requestedMode := strings.TrimSpace(r.FormValue("mode"))
	mode := sanitizeRequestedActiveMode(requestedMode, cfg, state)
	if requestedMode == "" || (mode == modeNone && normalizeRuntimeMode(requestedMode, cfg.General.AgentMode) != modeNone) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	state.setActiveMode(mode)
	cfg.General.ActiveMode = mode
	cfg.General.AgentMode = deriveLegacyAgentModeFromBindings(cfg.General.AssistHotkey, cfg.General.VoiceAgentHotkey, mode, cfg.General.AgentMode)
	cfg.General.AgentHotkey = legacyAgentHotkeyFromModeBindings(cfg.General.AssistHotkey, cfg.General.VoiceAgentHotkey, cfg.General.AgentMode)
	if err := config.Save(cfgPath, cfg); err != nil {
		slog.Warn("save active mode config", "err", err)
	}
	writeJSON(w, map[string]string{"activeMode": mode})
}

func currentActiveMode(cfg *config.Config, state *appState) string {
	state.mu.Lock()
	defer state.mu.Unlock()
	return sanitizeActiveModeForBindings(
		state.activeMode,
		cfg.General.AgentMode,
		state.dictateEnabled,
		state.assistEnabled,
		state.voiceAgentEnabled,
		state.dictateHotkey,
		state.assistHotkey,
		state.voiceAgentHotkey,
	)
}

func sanitizeRequestedActiveMode(requestedMode string, cfg *config.Config, state *appState) string {
	state.mu.Lock()
	dictateEnabled := state.dictateEnabled
	assistEnabled := state.assistEnabled
	voiceAgentEnabled := state.voiceAgentEnabled
	dictateHotkey := strings.TrimSpace(state.dictateHotkey)
	assistHotkey := strings.TrimSpace(state.assistHotkey)
	voiceAgentHotkey := strings.TrimSpace(state.voiceAgentHotkey)
	state.mu.Unlock()
	return sanitizeActiveModeForBindings(
		requestedMode,
		cfg.General.AgentMode,
		dictateEnabled,
		assistEnabled,
		voiceAgentEnabled,
		dictateHotkey,
		assistHotkey,
		voiceAgentHotkey,
	)
}

func registerModeEnabledRoute(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState) {
	mux.HandleFunc("/mode/enabled", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mode := strings.TrimSpace(r.FormValue("mode"))
		enabled := strings.TrimSpace(r.FormValue("enabled")) == "1"
		started := time.Now()
		oldSettings := currentDesktopModeSettings(cfg, state)
		appliedEnabled, ok := applyModeEnabled(cfg, mode, enabled)
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		state.setModeEnabled(mode, appliedEnabled)
		cfg.General.ActiveMode = sanitizeActiveModeForBindings(
			cfg.General.ActiveMode,
			cfg.General.AgentMode,
			cfg.General.DictateEnabled,
			cfg.General.AssistEnabled,
			cfg.General.VoiceAgentEnabled,
			cfg.General.DictateHotkey,
			cfg.General.AssistHotkey,
			cfg.General.VoiceAgentHotkey,
		)
		state.setActiveMode(cfg.General.ActiveMode)
		applyDesktopModeSettings(state, cfg, oldSettings)

		// Drive the lifecycle.Registry from the same toggle. Emits
		// speechkit.mode.{start,stop} audit events via the bridge
		// subscriber.
		if bridge := desktopLifecycle(state); bridge != nil {
			if err := bridge.Apply(r.Context(), cfg); err != nil {
				slog.Warn("lifecycle bridge apply on mode toggle",
					"mode", mode,
					"enabled", appliedEnabled,
					"err", err)
				restoreDesktopModeSettings(cfg, state, oldSettings)
				writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{
					"mode":      mode,
					"enabled":   oldSettings.enabledFor(mode),
					"status":    "reverted",
					"error":     err.Error(),
					"latencyMs": time.Since(started).Milliseconds(),
				})
				return
			}
		}

		// M2b phase 2: complete the hot path after the registry transition.
		// This creates Assist pipeline state after a cold Assist toggle-on
		// and hot-tears down TTS when Assist and Voice Agent are both off.
		syncDesktopModeRuntimeForCfg(r.Context(), cfg, state, nil)
		if err := config.Save(cfgPath, cfg); err != nil {
			slog.Warn("save mode enabled config", "err", err)
		}

		writeJSON(w, map[string]any{
			"mode":      mode,
			"enabled":   appliedEnabled,
			"status":    "applied",
			"latencyMs": time.Since(started).Milliseconds(),
		})
	})
}

func writeJSONStatus(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	writeJSON(w, payload)
}

type desktopModeSettings struct {
	dictateEnabled bool
	assistEnabled  bool
	voiceEnabled   bool
	dictateHotkey  string
	assistHotkey   string
	voiceHotkey    string
	activeMode     string
	audioDeviceID  string
	overlayEnabled bool
}

func currentDesktopModeSettings(cfg *config.Config, state *appState) desktopModeSettings {
	state.mu.Lock()
	audioDeviceID := state.audioDeviceID
	overlayEnabled := state.overlayEnabled
	state.mu.Unlock()
	return desktopModeSettings{
		dictateEnabled: cfg.General.DictateEnabled,
		assistEnabled:  cfg.General.AssistEnabled,
		voiceEnabled:   cfg.General.VoiceAgentEnabled,
		dictateHotkey:  cfg.General.DictateHotkey,
		assistHotkey:   cfg.General.AssistHotkey,
		voiceHotkey:    cfg.General.VoiceAgentHotkey,
		activeMode:     cfg.General.ActiveMode,
		audioDeviceID:  audioDeviceID,
		overlayEnabled: overlayEnabled,
	}
}

func (s desktopModeSettings) enabledFor(mode string) bool {
	switch mode {
	case modeDictate:
		return s.dictateEnabled
	case modeAssist:
		return s.assistEnabled
	case modeVoiceAgent:
		return s.voiceEnabled
	default:
		return false
	}
}

func applyModeEnabled(cfg *config.Config, mode string, enabled bool) (bool, bool) {
	switch mode {
	case modeDictate:
		appliedEnabled := enabled && strings.TrimSpace(cfg.General.DictateHotkey) != ""
		cfg.General.DictateEnabled = appliedEnabled
		return appliedEnabled, true
	case modeAssist:
		appliedEnabled := enabled && strings.TrimSpace(cfg.General.AssistHotkey) != ""
		cfg.General.AssistEnabled = appliedEnabled
		return appliedEnabled, true
	case modeVoiceAgent:
		appliedEnabled := enabled && strings.TrimSpace(cfg.General.VoiceAgentHotkey) != ""
		cfg.General.VoiceAgentEnabled = appliedEnabled
		return appliedEnabled, true
	default:
		return false, false
	}
}

func applyDesktopModeSettings(state *appState, cfg *config.Config, old desktopModeSettings) {
	state.applyDesktopSettings(
		old.dictateEnabled,
		old.assistEnabled,
		old.voiceEnabled,
		old.dictateHotkey,
		old.assistHotkey,
		old.voiceHotkey,
		cfg.General.DictateEnabled,
		cfg.General.AssistEnabled,
		cfg.General.VoiceAgentEnabled,
		cfg.General.DictateHotkey,
		cfg.General.AssistHotkey,
		cfg.General.VoiceAgentHotkey,
		old.audioDeviceID,
		old.audioDeviceID,
		old.overlayEnabled,
	)
}

func restoreDesktopModeSettings(cfg *config.Config, state *appState, old desktopModeSettings) {
	if cfg != nil {
		cfg.General.DictateEnabled = old.dictateEnabled
		cfg.General.AssistEnabled = old.assistEnabled
		cfg.General.VoiceAgentEnabled = old.voiceEnabled
		cfg.General.ActiveMode = old.activeMode
	}
	if state != nil {
		state.setModeEnabled(modeDictate, old.dictateEnabled)
		state.setModeEnabled(modeAssist, old.assistEnabled)
		state.setModeEnabled(modeVoiceAgent, old.voiceEnabled)
		state.setActiveMode(old.activeMode)
		applyDesktopModeSettings(state, cfg, old)
	}
}
