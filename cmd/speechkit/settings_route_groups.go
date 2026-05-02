package main

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/store"
)

func registerSettingsCoreRoutes(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router, feedbackStore store.Store) {
	mux.HandleFunc("/settings/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, state.settingsSnapshot(cfg))
	})
	mux.HandleFunc("/settings/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		message := saveSettings(r.Context(), r, cfgPath, cfg, state, sttRouter, feedbackStore)
		writeJSON(w, map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/overlay-position/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		message := resetOverlayPosition(cfgPath, cfg, state)
		writeJSON(w, map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/huggingface/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		message := saveHuggingFaceToken(r.Context(), r, cfg, state, sttRouter)
		writeJSON(w, map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/huggingface/token/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		message := clearHuggingFaceToken(r.Context(), cfg, state, sttRouter)
		writeJSON(w, map[string]string{"message": message})
	})
}

func registerSettingsCredentialRoutes(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router) {
	mux.HandleFunc("/settings/provider-credentials/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, msgFormParseError, http.StatusBadRequest)
			return
		}
		message, err := saveProviderCredential(r.Context(), r.FormValue("provider"), r.FormValue("credential"), cfg, state, sttRouter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/provider-integrations/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, msgFormParseError, http.StatusBadRequest)
			return
		}
		enabled := parseSettingsBool(r.FormValue("enabled"))
		message, err := updateProviderIntegration(r.Context(), cfgPath, r.FormValue("provider"), enabled, cfg, state, sttRouter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/provider-credentials/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, msgFormParseError, http.StatusBadRequest)
			return
		}
		message, err := clearProviderCredential(r.Context(), r.FormValue("provider"), cfg, state, sttRouter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/provider-credentials/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, msgFormParseError, http.StatusBadRequest)
			return
		}
		message, err := testProviderCredential(r.Context(), r.FormValue("provider"), r.FormValue("credential"), cfg)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]string{"message": message})
	})
}

func registerAudioDeviceRoutes(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState) {
	mux.HandleFunc("/audio/devices", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		devices, err := audio.ListCaptureDevices(audio.Config{
			Backend: audio.Backend(cfg.Audio.Backend),
		})
		if err != nil {
			writeJSON(w, map[string]any{
				"selectedDeviceId": cfg.Audio.DeviceID,
				"devices":          []audio.DeviceInfo{},
				"error":            err.Error(),
			})
			return
		}
		state.mu.Lock()
		selectedDeviceID := state.audioDeviceID
		state.mu.Unlock()
		writeJSON(w, map[string]any{
			"selectedDeviceId": selectedDeviceID,
			"devices":          devices,
		})
	})
	mux.HandleFunc("/audio/device", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		deviceID := strings.TrimSpace(r.FormValue("device_id"))
		if deviceID == "" {
			deviceID = strings.TrimSpace(r.FormValue("selected_audio_device_id"))
		}
		cfg.Audio.DeviceID = deviceID
		state.setAudioDevice(deviceID)
		if state.audioSession != nil {
			if err := state.audioSession.ReconfigureDevice(deviceID); err != nil {
				slog.Warn("audio device reconfigure", "err", err)
			}
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			slog.Warn("save audio device config", "err", err)
		}
		writeJSON(w, map[string]string{"selectedDeviceId": deviceID})
	})
	mux.HandleFunc("/audio/output/devices", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		devices, err := audio.ListOutputDevices(audio.Config{
			Backend: audio.Backend(cfg.Audio.Backend),
		})
		if err != nil {
			writeJSON(w, map[string]any{
				"selectedDeviceId": cfg.Audio.OutputDeviceID,
				"devices":          []audio.DeviceInfo{},
				"error":            err.Error(),
			})
			return
		}
		state.mu.Lock()
		selectedDeviceID := state.audioOutputDeviceID
		state.mu.Unlock()
		if selectedDeviceID == "" {
			selectedDeviceID = cfg.Audio.OutputDeviceID
		}
		writeJSON(w, map[string]any{
			"selectedDeviceId": selectedDeviceID,
			"devices":          devices,
		})
	})
	mux.HandleFunc("/audio/output/device", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		deviceID := strings.TrimSpace(r.FormValue("device_id"))
		if deviceID == "" {
			deviceID = strings.TrimSpace(r.FormValue("selected_output_device_id"))
		}
		cfg.Audio.OutputDeviceID = deviceID
		state.setAudioOutputDevice(r.Context(), deviceID)
		if err := config.Save(cfgPath, cfg); err != nil {
			slog.Warn("save audio output device config", "err", err)
		}
		writeJSON(w, map[string]string{"selectedDeviceId": deviceID})
	})
}

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
		if err := config.Save(cfgPath, cfg); err != nil {
			slog.Warn("save mode enabled config", "err", err)
		}

		writeJSON(w, map[string]any{
			"mode":    mode,
			"enabled": appliedEnabled,
		})
	})
}

type desktopModeSettings struct {
	dictateEnabled bool
	assistEnabled  bool
	voiceEnabled   bool
	dictateHotkey  string
	assistHotkey   string
	voiceHotkey    string
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
		audioDeviceID:  audioDeviceID,
		overlayEnabled: overlayEnabled,
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

func registerModelRoutes(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router) {
	mux.HandleFunc("/models/available", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		state.mu.Lock()
		rt := state.genkitRT
		state.mu.Unlock()
		if rt == nil {
			writeJSON(w, []struct{}{})
			return
		}
		writeJSON(w, rt.ModelInfos())
	})
	mux.HandleFunc("/models/profiles", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		state.mu.Lock()
		activeProfiles := cloneStringMap(state.activeProfiles)
		state.mu.Unlock()
		catalog := filteredModelCatalog()
		writeJSON(w, map[string]any{
			"profiles":       catalog.Profiles,
			"activeProfiles": activeProfiles,
		})
	})
	mux.HandleFunc("/models/profiles/activate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		modality := strings.TrimSpace(r.FormValue("modality"))
		profileID := strings.TrimSpace(r.FormValue("profile_id"))
		if modality == "" || profileID == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		profile := findModelProfile(profileID)
		if profile == nil {
			http.Error(w, "profile not found", http.StatusNotFound)
			return
		}
		if profile.ExecutionMode == models.ExecutionModeHFRouted && !config.ManagedHuggingFaceAvailableInBuild() {
			http.Error(w, msgHFUnavailableBuild, http.StatusBadRequest)
			return
		}
		if err := applyModelProfile(r.Context(), cfgPath, cfg, state, sttRouter, *profile); err != nil {
			slog.Warn("apply model profile", "profileId", profileID, "err", err) //nolint:gosec // G706: profileID is a model catalog ID, not user-controlled input
			http.Error(w, "failed to apply model profile", http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]string{
			"modality":  modality,
			"profileId": profileID,
			"model":     profile.ModelID,
		})
	})
}

func findModelProfile(profileID string) *models.Profile {
	catalog := filteredModelCatalog()
	for _, p := range catalog.Profiles {
		if p.ID == profileID {
			p := p
			return &p
		}
	}
	return nil
}
