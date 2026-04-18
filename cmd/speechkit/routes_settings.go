package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/secrets"
)

var errMissingHuggingFaceToken = fmt.Errorf("missing hugging face token")
var errHFUnavailableBuild = errors.New("hugging face is not available in this build")

func registerSettingsRoutes(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router) {
	mux.HandleFunc("/settings/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(state.settingsSnapshot(cfg))
	})
	mux.HandleFunc("/settings/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		message := saveSettings(r.Context(), r, cfgPath, cfg, state, sttRouter)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/overlay-position/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		message := resetOverlayPosition(cfgPath, cfg, state)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/huggingface/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		message := saveHuggingFaceToken(r.Context(), r, cfg, state, sttRouter)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/huggingface/token/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		message := clearHuggingFaceToken(r.Context(), cfg, state, sttRouter)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
	})
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
	})
	mux.HandleFunc("/audio/devices", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		devices, err := audio.ListCaptureDevices(audio.Config{
			Backend: audio.Backend(cfg.Audio.Backend),
		})
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"selectedDeviceId": cfg.Audio.DeviceID,
				"devices":          []audio.DeviceInfo{},
				"error":            err.Error(),
			})
			return
		}
		state.mu.Lock()
		selectedDeviceID := state.audioDeviceID
		state.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"selectedDeviceId": deviceID})
	})
	mux.HandleFunc("/mode/active", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			state.mu.Lock()
			activeMode := sanitizeActiveModeForBindings(
				state.activeMode,
				cfg.General.AgentMode,
				state.dictateEnabled,
				state.assistEnabled,
				state.voiceAgentEnabled,
				state.dictateHotkey,
				state.assistHotkey,
				state.voiceAgentHotkey,
			)
			state.mu.Unlock()
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]string{"activeMode": activeMode})
		case http.MethodPost:
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			if err := r.ParseForm(); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			requestedMode := strings.TrimSpace(r.FormValue("mode"))
			state.mu.Lock()
			dictateEnabled := state.dictateEnabled
			assistEnabled := state.assistEnabled
			voiceAgentEnabled := state.voiceAgentEnabled
			dictateHotkey := strings.TrimSpace(state.dictateHotkey)
			assistHotkey := strings.TrimSpace(state.assistHotkey)
			voiceAgentHotkey := strings.TrimSpace(state.voiceAgentHotkey)
			state.mu.Unlock()
			mode := sanitizeActiveModeForBindings(
				requestedMode,
				cfg.General.AgentMode,
				dictateEnabled,
				assistEnabled,
				voiceAgentEnabled,
				dictateHotkey,
				assistHotkey,
				voiceAgentHotkey,
			)
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
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]string{"activeMode": mode})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
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
		oldDictateEnabled := cfg.General.DictateEnabled
		oldAssistEnabled := cfg.General.AssistEnabled
		oldVoiceAgentEnabled := cfg.General.VoiceAgentEnabled
		oldDictateHotkey := cfg.General.DictateHotkey
		oldAssistHotkey := cfg.General.AssistHotkey
		oldVoiceAgentHotkey := cfg.General.VoiceAgentHotkey
		state.mu.Lock()
		audioDeviceID := state.audioDeviceID
		overlayEnabled := state.overlayEnabled
		state.mu.Unlock()
		switch mode {
		case modeDictate:
			enabled = enabled && strings.TrimSpace(cfg.General.DictateHotkey) != ""
			cfg.General.DictateEnabled = enabled
		case modeAssist:
			enabled = enabled && strings.TrimSpace(cfg.General.AssistHotkey) != ""
			cfg.General.AssistEnabled = enabled
		case modeVoiceAgent:
			enabled = enabled && strings.TrimSpace(cfg.General.VoiceAgentHotkey) != ""
			cfg.General.VoiceAgentEnabled = enabled
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		state.setModeEnabled(mode, enabled)
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
		state.applyDesktopSettings(
			oldDictateEnabled,
			oldAssistEnabled,
			oldVoiceAgentEnabled,
			oldDictateHotkey,
			oldAssistHotkey,
			oldVoiceAgentHotkey,
			cfg.General.DictateEnabled,
			cfg.General.AssistEnabled,
			cfg.General.VoiceAgentEnabled,
			cfg.General.DictateHotkey,
			cfg.General.AssistHotkey,
			cfg.General.VoiceAgentHotkey,
			audioDeviceID,
			audioDeviceID,
			overlayEnabled,
		)
		if err := config.Save(cfgPath, cfg); err != nil {
			slog.Warn("save mode enabled config", "err", err)
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mode":    mode,
			"enabled": enabled,
		})
	})
	mux.HandleFunc("/models/available", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		state.mu.Lock()
		rt := state.genkitRT
		state.mu.Unlock()
		if rt == nil {
			_ = json.NewEncoder(w).Encode([]struct{}{})
			return
		}
		_ = json.NewEncoder(w).Encode(rt.ModelInfos())
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
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

		catalog := filteredModelCatalog()
		var profile *models.Profile
		for _, p := range catalog.Profiles {
			if p.ID == profileID {
				p := p
				profile = &p
				break
			}
		}
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"modality":  modality,
			"profileId": profileID,
			"model":     profile.ModelID,
		})
	})
}

func saveSettings(ctx context.Context, req *http.Request, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router) string {
	if err := req.ParseForm(); err != nil {
		return msgFormParseError
	}

	form, errMsg := parseSettingsForm(req, cfg)
	if errMsg != "" {
		return errMsg
	}

	nextCfg := buildNextConfig(form, cfg)
	applySelectedVoiceAgentProfile(&nextCfg, filteredModelCatalog())
	oldDictateEnabled := cfg.General.DictateEnabled
	oldAssistEnabled := cfg.General.AssistEnabled
	oldVoiceAgentEnabled := cfg.General.VoiceAgentEnabled
	oldDictateHotkey := cfg.General.DictateHotkey
	oldAssistHotkey := cfg.General.AssistHotkey
	oldVoiceAgentHotkey := cfg.General.VoiceAgentHotkey
	oldAudioDeviceID := cfg.Audio.DeviceID

	managedHFEnabled := config.ApplyManagedIntegrationDefaults(&nextCfg)
	needsHFRefresh := managedHFEnabled ||
		!cfg.HuggingFace.Enabled ||
		form.HFModel != cfg.HuggingFace.Model
	shouldValidateHF := nextCfg.HuggingFace.Enabled && needsHFRefresh
	if shouldValidateHF {
		if err := refreshHuggingFaceProvider(ctx, &nextCfg, sttRouter, managedHFEnabled); err != nil {
			if errors.Is(err, errMissingHuggingFaceToken) {
				return msgHFTokenMissing
			}
			return fmt.Sprintf(msgModelUnreachable, err)
		}
	}
	if !nextCfg.HuggingFace.Enabled {
		sttRouter.SetHuggingFace(nil)
	}

	if err := refreshProviderRuntimes(ctx, &nextCfg, state, sttRouter); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}

	*cfg = nextCfg

	if err := config.Save(cfgPath, cfg); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}

	state.applyRuntimeSettings(
		form.DictateEnabled,
		form.AssistEnabled,
		form.VoiceAgentEnabled,
		form.DictateHotkey,
		form.AssistHotkey,
		form.VoiceAgentHotkey,
		form.DictateHotkeyBehavior,
		form.AssistHotkeyBehavior,
		form.VoiceAgentHotkeyBehavior,
		form.ActiveMode,
		form.AudioDeviceID,
		runtimeAvailableProviders(ctx, sttRouter),
		form.Visualizer,
		form.Design,
		form.OverlayPosition,
		form.VocabularyDictionary,
		form.OverlayMovable,
		form.OverlayFreeX,
		form.OverlayFreeY,
		form.OverlayMonitorPositions,
	)
	state.applyDesktopSettings(
		oldDictateEnabled,
		oldAssistEnabled,
		oldVoiceAgentEnabled,
		oldDictateHotkey,
		oldAssistHotkey,
		oldVoiceAgentHotkey,
		form.DictateEnabled,
		form.AssistEnabled,
		form.VoiceAgentEnabled,
		form.DictateHotkey,
		form.AssistHotkey,
		form.VoiceAgentHotkey,
		oldAudioDeviceID,
		form.AudioDeviceID,
		form.OverlayEnabled,
	)

	return msgSaved
}

func resetOverlayPosition(cfgPath string, cfg *config.Config, state *appState) string {
	if cfg == nil || state == nil {
		return fmt.Sprintf(msgSaveFailed, errors.New("settings state unavailable"))
	}

	cfg.UI.OverlayFreeX = 0
	cfg.UI.OverlayFreeY = 0
	cfg.UI.OverlayMonitorPositions = map[string]config.OverlayFreePosition{}

	if err := config.Save(cfgPath, cfg); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}

	state.mu.Lock()
	state.overlayFreeX = 0
	state.overlayFreeY = 0
	state.overlayMonitorCenters = map[string]config.OverlayFreePosition{}
	state.syncSpeechKitSnapshotLocked()
	state.mu.Unlock()

	state.refreshOverlayWindows()

	return msgSaved
}

// settingsFormData holds parsed and validated form values from the settings page.
type settingsFormData struct {
	DictateEnabled             bool
	AssistEnabled              bool
	VoiceAgentEnabled          bool
	DictateHotkey              string
	AssistHotkey               string
	VoiceAgentHotkey           string
	DictateHotkeyBehavior      string
	AssistHotkeyBehavior       string
	VoiceAgentHotkeyBehavior   string
	VoiceAgentCloseBehavior    string
	VoiceAgentRefinementPrompt string
	AutoStartOnLaunch          bool
	AgentHotkey                string
	AgentMode                  string
	ActiveMode                 string
	AudioDeviceID              string
	HFModel                    string
	OverlayEnabled             bool
	Visualizer                 string
	Design                     string
	OverlayPosition            string
	OverlayMovable             bool
	OverlayFreeX               int
	OverlayFreeY               int
	OverlayMonitorPositions    map[string]config.OverlayFreePosition
	StoreBackend               string
	StoreSQLitePath            string
	StorePostgresDSN           string
	StoreSaveAudio             bool
	StoreAudioRetention        int
	StoreMaxAudioStorage       int
	VocabularyDictionary       string
	Language                   string
	DictatePrimaryProfileID    string
	DictateFallbackProfileID   string
	AssistPrimaryProfileID     string
	AssistFallbackProfileID    string
	VoicePrimaryProfileID      string
	VoiceFallbackProfileID     string
}

// parseSettingsForm extracts and validates all settings form values.
// Returns the parsed data and an empty string on success, or an error message.
func parseSettingsForm(req *http.Request, cfg *config.Config) (settingsFormData, string) {
	var f settingsFormData

	_, hasDictateHotkey := req.PostForm["dictate_hotkey"]
	if hasDictateHotkey {
		f.DictateHotkey = strings.TrimSpace(req.FormValue("dictate_hotkey"))
	} else {
		f.DictateHotkey = strings.TrimSpace(req.FormValue("hotkey"))
		if f.DictateHotkey == "" {
			f.DictateHotkey = strings.TrimSpace(cfg.General.DictateHotkey)
		}
		if f.DictateHotkey == "" {
			f.DictateHotkey = strings.TrimSpace(cfg.General.Hotkey)
		}
	}
	_, hasAssistHotkey := req.PostForm["assist_hotkey"]
	_, hasVoiceAgentHotkey := req.PostForm["voice_agent_hotkey"]
	f.DictateEnabled = cfg.General.DictateEnabled
	if raw := strings.TrimSpace(req.FormValue("dictate_enabled")); raw != "" {
		f.DictateEnabled = raw == "1"
	}
	f.AssistEnabled = cfg.General.AssistEnabled
	if raw := strings.TrimSpace(req.FormValue("assist_enabled")); raw != "" {
		f.AssistEnabled = raw == "1"
	}
	f.VoiceAgentEnabled = cfg.General.VoiceAgentEnabled
	if raw := strings.TrimSpace(req.FormValue("voice_agent_enabled")); raw != "" {
		f.VoiceAgentEnabled = raw == "1"
	}
	f.AssistHotkey = strings.TrimSpace(req.FormValue("assist_hotkey"))
	f.VoiceAgentHotkey = strings.TrimSpace(req.FormValue("voice_agent_hotkey"))
	legacyAgentMode := strings.TrimSpace(req.FormValue("agent_mode"))
	if legacyAgentMode == "" {
		legacyAgentMode = cfg.General.AgentMode
	}
	f.AgentMode = normalizeAgentMode(legacyAgentMode)
	f.AgentHotkey = strings.TrimSpace(req.FormValue("agent_hotkey"))
	legacyAgentHotkeyPosted := !hasAssistHotkey && !hasVoiceAgentHotkey && postFormIncludes(req, "agent_hotkey")
	if legacyAgentHotkeyPosted {
		f.AssistHotkey = ""
		f.VoiceAgentHotkey = ""
		switch f.AgentMode {
		case modeVoiceAgent:
			f.VoiceAgentHotkey = f.AgentHotkey
		default:
			f.AssistHotkey = f.AgentHotkey
		}
	}
	if !legacyAgentHotkeyPosted && !hasAssistHotkey && f.AssistHotkey == "" {
		f.AssistHotkey = strings.TrimSpace(cfg.General.AssistHotkey)
	}
	if !legacyAgentHotkeyPosted && !hasVoiceAgentHotkey && f.VoiceAgentHotkey == "" {
		f.VoiceAgentHotkey = strings.TrimSpace(cfg.General.VoiceAgentHotkey)
	}
	if strings.TrimSpace(f.DictateHotkey) == "" {
		f.DictateEnabled = false
	}
	if strings.TrimSpace(f.AssistHotkey) == "" {
		f.AssistEnabled = false
	}
	if strings.TrimSpace(f.VoiceAgentHotkey) == "" {
		f.VoiceAgentEnabled = false
	}
	f.DictateHotkeyBehavior = config.NormalizeHotkeyBehavior(
		req.FormValue("dictate_hotkey_behavior"),
		config.NormalizeHotkeyBehavior(cfg.General.DictateHotkeyBehavior, config.HotkeyBehaviorPushToTalk),
	)
	f.AssistHotkeyBehavior = config.NormalizeHotkeyBehavior(
		req.FormValue("assist_hotkey_behavior"),
		config.NormalizeHotkeyBehavior(cfg.General.AssistHotkeyBehavior, config.HotkeyBehaviorPushToTalk),
	)
	f.VoiceAgentHotkeyBehavior = config.NormalizeHotkeyBehavior(
		req.FormValue("voice_agent_hotkey_behavior"),
		config.NormalizeHotkeyBehavior(cfg.General.VoiceAgentHotkeyBehavior, config.HotkeyBehaviorPushToTalk),
	)
	f.VoiceAgentCloseBehavior = config.NormalizeVoiceAgentCloseBehavior(
		req.FormValue("voice_agent_close_behavior"),
		config.NormalizeVoiceAgentCloseBehavior(cfg.VoiceAgent.CloseBehavior, config.VoiceAgentCloseBehaviorContinue),
	)
	f.VoiceAgentRefinementPrompt = normalizeVoiceAgentPrompt(req.FormValue("voice_agent_refinement_prompt"))
	if !postFormIncludes(req, "voice_agent_refinement_prompt") {
		f.VoiceAgentRefinementPrompt = strings.TrimSpace(cfg.VoiceAgent.RefinementPrompt)
	}
	f.AutoStartOnLaunch = cfg.General.AutoStartOnLaunch
	if raw := strings.TrimSpace(req.FormValue("auto_start_on_launch")); raw != "" {
		f.AutoStartOnLaunch = raw == "1"
	} else if raw := strings.TrimSpace(req.FormValue("voice_agent_auto_start")); raw != "" {
		f.AutoStartOnLaunch = raw == "1"
	}
	for _, binding := range []string{f.DictateHotkey, f.AssistHotkey, f.VoiceAgentHotkey} {
		if strings.TrimSpace(binding) == "" {
			continue
		}
		if _, err := parseModeHotkey(binding); err != nil {
			return f, msgUnsupportedModeHotkey
		}
	}
	f.AgentHotkey = legacyAgentHotkeyFromModeBindings(f.AssistHotkey, f.VoiceAgentHotkey, f.AgentMode)
	activeMode := strings.TrimSpace(req.FormValue("active_mode"))
	if activeMode == "" && !postFormIncludes(req, "active_mode") {
		activeMode = cfg.General.ActiveMode
	}
	f.ActiveMode = sanitizeActiveModeForBindings(
		activeMode,
		f.AgentMode,
		f.DictateEnabled,
		f.AssistEnabled,
		f.VoiceAgentEnabled,
		f.DictateHotkey,
		f.AssistHotkey,
		f.VoiceAgentHotkey,
	)
	if !validateDistinctModeHotkeys(
		f.DictateEnabled,
		f.AssistEnabled,
		f.VoiceAgentEnabled,
		f.DictateHotkey,
		f.AssistHotkey,
		f.VoiceAgentHotkey,
	) {
		return f, msgDuplicateHotkeys
	}
	f.AudioDeviceID = strings.TrimSpace(req.FormValue("audio_device_id"))
	if f.AudioDeviceID == "" {
		f.AudioDeviceID = strings.TrimSpace(req.FormValue("selected_audio_device_id"))
	}
	if f.AudioDeviceID == "" {
		f.AudioDeviceID = cfg.Audio.DeviceID
	}
	f.HFModel = strings.TrimSpace(req.FormValue("hf_model"))
	if f.HFModel == "" {
		f.HFModel = cfg.HuggingFace.Model
	}
	hfAvailableInBuild := config.ManagedHuggingFaceAvailableInBuild()
	if hfAvailableInBuild && !isSupportedHFModel(f.HFModel) {
		return f, msgUnsupportedModel
	}

	f.OverlayEnabled = req.FormValue("overlay_enabled") == "1"
	f.Visualizer = strings.TrimSpace(req.FormValue("overlay_visualizer"))
	if f.Visualizer == "" {
		f.Visualizer = cfg.UI.Visualizer
	}
	if !isSupportedOverlayVisualizer(f.Visualizer) {
		return f, msgUnsupportedVis
	}
	f.Design = strings.TrimSpace(req.FormValue("overlay_design"))
	if f.Design == "" {
		f.Design = cfg.UI.Design
	}
	if !isSupportedOverlayDesign(f.Design) {
		return f, msgUnsupportedDesign
	}
	f.OverlayPosition = strings.TrimSpace(req.FormValue("overlay_position"))
	if f.OverlayPosition == "" {
		f.OverlayPosition = cfg.UI.OverlayPosition
	}
	if !isSupportedOverlayPosition(f.OverlayPosition) {
		return f, msgUnsupportedPos
	}
	f.OverlayMovable = cfg.UI.OverlayMovable
	if raw := strings.TrimSpace(req.FormValue("overlay_movable")); raw != "" {
		f.OverlayMovable = raw == "1"
	}
	f.OverlayFreeX = cfg.UI.OverlayFreeX
	if raw := strings.TrimSpace(req.FormValue("overlay_free_x")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			f.OverlayFreeX = parsed
		}
	}
	f.OverlayFreeY = cfg.UI.OverlayFreeY
	if raw := strings.TrimSpace(req.FormValue("overlay_free_y")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			f.OverlayFreeY = parsed
		}
	}
	f.OverlayMonitorPositions = cloneOverlayMonitorPositions(cfg.UI.OverlayMonitorPositions)
	storeSaveAudioRaw := strings.TrimSpace(req.FormValue("store_save_audio"))
	f.StoreSaveAudio = cfg.Store.SaveAudio
	if storeSaveAudioRaw != "" {
		f.StoreSaveAudio = storeSaveAudioRaw == "1"
	}
	f.StoreBackend = strings.TrimSpace(req.FormValue("store_backend"))
	if f.StoreBackend == "" {
		f.StoreBackend = cfg.Store.Backend
	}
	if f.StoreBackend == "" {
		f.StoreBackend = "sqlite"
	}
	switch f.StoreBackend {
	case "sqlite", "postgres":
	default:
		return f, msgUnsupportedStore
	}
	f.StoreSQLitePath = strings.TrimSpace(req.FormValue("store_sqlite_path"))
	if f.StoreSQLitePath == "" {
		f.StoreSQLitePath = cfg.Store.SQLitePath
	}
	f.StorePostgresDSN = strings.TrimSpace(req.FormValue("store_postgres_dsn"))
	if f.StorePostgresDSN == "" {
		f.StorePostgresDSN = cfg.Store.PostgresDSN
	}
	if f.StoreBackend == "postgres" && f.StorePostgresDSN == "" {
		return f, msgPostgresDSNReq
	}
	f.StoreAudioRetention = cfg.Store.AudioRetentionDays
	if raw := strings.TrimSpace(req.FormValue("store_audio_retention_days")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			f.StoreAudioRetention = parsed
		}
	}
	f.StoreMaxAudioStorage = cfg.Store.MaxAudioStorageMB
	if raw := strings.TrimSpace(req.FormValue("store_max_audio_storage_mb")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			f.StoreMaxAudioStorage = parsed
		}
	}
	_, hasVocabularyDictionary := req.PostForm["vocabulary_dictionary"]
	f.VocabularyDictionary = normalizeVocabularyDictionary(req.FormValue("vocabulary_dictionary"))
	if !hasVocabularyDictionary {
		f.VocabularyDictionary = cfg.Vocabulary.Dictionary
	}
	f.Language = strings.TrimSpace(req.FormValue("language"))
	if f.Language == "" {
		f.Language = cfg.General.Language
	}
	if f.Language == "" {
		f.Language = "de"
	}

	f.DictatePrimaryProfileID = strings.TrimSpace(req.FormValue("dictate_primary_profile_id"))
	if f.DictatePrimaryProfileID == "" {
		f.DictatePrimaryProfileID = strings.TrimSpace(cfg.ModelSelection.Dictate.PrimaryProfileID)
	}
	f.DictateFallbackProfileID = strings.TrimSpace(req.FormValue("dictate_fallback_profile_id"))
	if f.DictateFallbackProfileID == "" {
		f.DictateFallbackProfileID = strings.TrimSpace(cfg.ModelSelection.Dictate.FallbackProfileID)
	}
	f.AssistPrimaryProfileID = strings.TrimSpace(req.FormValue("assist_primary_profile_id"))
	if f.AssistPrimaryProfileID == "" {
		f.AssistPrimaryProfileID = strings.TrimSpace(cfg.ModelSelection.Assist.PrimaryProfileID)
	}
	f.AssistFallbackProfileID = strings.TrimSpace(req.FormValue("assist_fallback_profile_id"))
	if f.AssistFallbackProfileID == "" {
		f.AssistFallbackProfileID = strings.TrimSpace(cfg.ModelSelection.Assist.FallbackProfileID)
	}
	f.VoicePrimaryProfileID = strings.TrimSpace(req.FormValue("voice_primary_profile_id"))
	if f.VoicePrimaryProfileID == "" {
		f.VoicePrimaryProfileID = strings.TrimSpace(cfg.ModelSelection.VoiceAgent.PrimaryProfileID)
	}
	f.VoiceFallbackProfileID = strings.TrimSpace(req.FormValue("voice_fallback_profile_id"))
	if f.VoiceFallbackProfileID == "" {
		f.VoiceFallbackProfileID = strings.TrimSpace(cfg.ModelSelection.VoiceAgent.FallbackProfileID)
	}

	catalog := filteredModelCatalog()
	if err := validateModeSelection(cfg, catalog, modeDictate, config.ModeModelSelection{
		PrimaryProfileID:  f.DictatePrimaryProfileID,
		FallbackProfileID: f.DictateFallbackProfileID,
	}); err != nil {
		return f, err.Error()
	}
	if err := validateModeSelection(cfg, catalog, modeAssist, config.ModeModelSelection{
		PrimaryProfileID:  f.AssistPrimaryProfileID,
		FallbackProfileID: f.AssistFallbackProfileID,
	}); err != nil {
		return f, err.Error()
	}
	if err := validateModeSelection(cfg, catalog, modeVoiceAgent, config.ModeModelSelection{
		PrimaryProfileID:  f.VoicePrimaryProfileID,
		FallbackProfileID: f.VoiceFallbackProfileID,
	}); err != nil {
		return f, err.Error()
	}

	return f, ""
}

// buildNextConfig creates a new Config from the parsed form values.
func buildNextConfig(form settingsFormData, cfg *config.Config) config.Config {
	hfAvailableInBuild := config.ManagedHuggingFaceAvailableInBuild()
	nextCfg := *cfg
	nextCfg.General.Hotkey = form.DictateHotkey // keep legacy field in sync
	nextCfg.General.DictateHotkey = form.DictateHotkey
	nextCfg.General.AssistHotkey = form.AssistHotkey
	nextCfg.General.VoiceAgentHotkey = form.VoiceAgentHotkey
	nextCfg.General.DictateHotkeyBehavior = config.NormalizeHotkeyBehavior(form.DictateHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	nextCfg.General.AssistHotkeyBehavior = config.NormalizeHotkeyBehavior(form.AssistHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	nextCfg.General.VoiceAgentHotkeyBehavior = config.NormalizeHotkeyBehavior(form.VoiceAgentHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	nextCfg.General.DictateEnabled = form.DictateEnabled
	nextCfg.General.AssistEnabled = form.AssistEnabled
	nextCfg.General.VoiceAgentEnabled = form.VoiceAgentEnabled
	nextCfg.General.ActiveMode = sanitizeActiveModeForBindings(
		form.ActiveMode,
		form.AgentMode,
		form.DictateEnabled,
		form.AssistEnabled,
		form.VoiceAgentEnabled,
		form.DictateHotkey,
		form.AssistHotkey,
		form.VoiceAgentHotkey,
	)
	nextCfg.General.AgentMode = deriveLegacyAgentModeFromBindings(form.AssistHotkey, form.VoiceAgentHotkey, nextCfg.General.ActiveMode, form.AgentMode)
	nextCfg.General.AgentHotkey = legacyAgentHotkeyFromModeBindings(form.AssistHotkey, form.VoiceAgentHotkey, nextCfg.General.AgentMode)
	nextCfg.General.HotkeyMode = nextCfg.General.DictateHotkeyBehavior
	nextCfg.ModelSelection.Dictate = normalizeModeSelection(config.ModeModelSelection{
		PrimaryProfileID:  form.DictatePrimaryProfileID,
		FallbackProfileID: form.DictateFallbackProfileID,
	})
	nextCfg.ModelSelection.Assist = normalizeModeSelection(config.ModeModelSelection{
		PrimaryProfileID:  form.AssistPrimaryProfileID,
		FallbackProfileID: form.AssistFallbackProfileID,
	})
	nextCfg.ModelSelection.VoiceAgent = normalizeModeSelection(config.ModeModelSelection{
		PrimaryProfileID:  form.VoicePrimaryProfileID,
		FallbackProfileID: form.VoiceFallbackProfileID,
	})
	nextCfg.VoiceAgent.RefinementPrompt = form.VoiceAgentRefinementPrompt
	nextCfg.VoiceAgent.CloseBehavior = config.NormalizeVoiceAgentCloseBehavior(
		form.VoiceAgentCloseBehavior,
		config.NormalizeVoiceAgentCloseBehavior(cfg.VoiceAgent.CloseBehavior, config.VoiceAgentCloseBehaviorContinue),
	)
	nextCfg.General.AutoStartOnLaunch = form.AutoStartOnLaunch
	nextCfg.VoiceAgent.AutoStartOnLaunch = form.AutoStartOnLaunch
	nextCfg.Audio.DeviceID = form.AudioDeviceID
	nextCfg.HuggingFace.Enabled = cfg.HuggingFace.Enabled && hfAvailableInBuild
	nextCfg.HuggingFace.Model = form.HFModel
	nextCfg.UI.OverlayEnabled = form.OverlayEnabled
	nextCfg.UI.OverlayPosition = form.OverlayPosition
	nextCfg.UI.OverlayMovable = form.OverlayMovable
	nextCfg.UI.OverlayFreeX = form.OverlayFreeX
	nextCfg.UI.OverlayFreeY = form.OverlayFreeY
	nextCfg.UI.OverlayMonitorPositions = cloneOverlayMonitorPositions(form.OverlayMonitorPositions)
	nextCfg.UI.Visualizer = form.Visualizer
	nextCfg.UI.Design = form.Design
	nextCfg.Store.Backend = form.StoreBackend
	nextCfg.Store.SQLitePath = form.StoreSQLitePath
	nextCfg.Store.PostgresDSN = form.StorePostgresDSN
	nextCfg.Store.SaveAudio = form.StoreSaveAudio
	nextCfg.Store.AudioRetentionDays = form.StoreAudioRetention
	nextCfg.Store.MaxAudioStorageMB = form.StoreMaxAudioStorage
	nextCfg.Feedback.SaveAudio = form.StoreSaveAudio
	nextCfg.Feedback.AudioRetentionDays = form.StoreAudioRetention
	if form.StoreBackend == "sqlite" {
		nextCfg.Feedback.DBPath = form.StoreSQLitePath
	}
	nextCfg.Vocabulary.Dictionary = form.VocabularyDictionary
	nextCfg.General.Language = form.Language
	return nextCfg
}

func postFormIncludes(req *http.Request, key string) bool {
	if req == nil || req.PostForm == nil {
		return false
	}
	_, ok := req.PostForm[key]
	return ok
}

func normalizeVocabularyDictionary(input string) string {
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.TrimSpace(normalized)
}

func normalizeVoiceAgentPrompt(input string) string {
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.TrimSpace(normalized)
}

func refreshHuggingFaceProvider(ctx context.Context, cfg *config.Config, sttRouter *router.Router, skipHealthCheck bool) error {
	if !config.ManagedHuggingFaceAvailableInBuild() {
		return errHFUnavailableBuild
	}
	token, _, err := config.ResolveHuggingFaceToken(cfg)
	if err != nil {
		return err
	}
	if token == "" {
		return errMissingHuggingFaceToken
	}

	provider := newHuggingFaceProvider(cfg.HuggingFace.Model, token)
	if !skipHealthCheck {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := provider.Health(checkCtx)
		cancel()
		if err != nil {
			return err
		}
	}
	sttRouter.SetHuggingFace(provider)
	return nil
}

func saveHuggingFaceToken(ctx context.Context, req *http.Request, cfg *config.Config, state *appState, sttRouter *router.Router) string {
	if err := req.ParseForm(); err != nil {
		return msgFormParseError
	}
	if !config.ManagedHuggingFaceAvailableInBuild() {
		return msgHFUnavailableBuild
	}
	token := strings.TrimSpace(req.FormValue("hf_token"))
	if token == "" {
		return msgHFTokenRequired
	}
	if err := secrets.SetUserHuggingFaceToken(token); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}
	cfg.HuggingFace.Enabled = true
	if strings.TrimSpace(cfg.HuggingFace.Model) == "" {
		cfg.HuggingFace.Model = "openai/whisper-large-v3"
	}
	if cfg.HuggingFace.Enabled {
		if err := refreshHuggingFaceProvider(ctx, cfg, sttRouter, false); err != nil {
			if errors.Is(err, errMissingHuggingFaceToken) {
				return msgHFTokenMissing
			}
			return fmt.Sprintf(msgModelUnreachable, err)
		}
	}
	if err := refreshProviderRuntimes(ctx, cfg, state, sttRouter); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}
	return msgHFTokenSaved
}

func clearHuggingFaceToken(ctx context.Context, cfg *config.Config, state *appState, sttRouter *router.Router) string {
	if !config.ManagedHuggingFaceAvailableInBuild() {
		return msgHFUnavailableBuild
	}
	if err := secrets.ClearUserHuggingFaceToken(); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}
	if cfg.HuggingFace.Enabled {
		if err := refreshHuggingFaceProvider(ctx, cfg, sttRouter, true); err != nil {
			if errors.Is(err, errMissingHuggingFaceToken) {
				sttRouter.SetHuggingFace(nil)
				if refreshErr := refreshProviderRuntimes(ctx, cfg, state, sttRouter); refreshErr != nil {
					return fmt.Sprintf(msgSaveFailed, refreshErr)
				}
				return msgHFTokenCleared
			}
			return fmt.Sprintf(msgModelUnreachable, err)
		}
	}
	if err := refreshProviderRuntimes(ctx, cfg, state, sttRouter); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}
	return msgHFTokenCleared
}

func isSupportedHFModel(model string) bool {
	switch model {
	case "openai/whisper-large-v3-turbo", "openai/whisper-large-v3":
		return true
	default:
		return false
	}
}

func filteredModelCatalog() models.Catalog {
	catalog := models.DefaultCatalog()
	filtered := make([]models.Profile, 0, len(catalog.Profiles))
	for _, profile := range catalog.Profiles {
		switch profile.Modality {
		case models.ModalitySTT, models.ModalityUtility, models.ModalityAssist, models.ModalityRealtimeVoice:
		default:
			continue
		}
		if profile.ExecutionMode == models.ExecutionModeHFRouted && !config.ManagedHuggingFaceAvailableInBuild() {
			continue
		}
		filtered = append(filtered, profile)
	}
	catalog.Profiles = filtered
	return catalog
}

func isSupportedOverlayVisualizer(visualizer string) bool {
	switch visualizer {
	case "pill", "circle":
		return true
	default:
		return false
	}
}

func isSupportedOverlayDesign(design string) bool {
	switch design {
	case "default", "kombify":
		return true
	default:
		return false
	}
}

func isSupportedOverlayPosition(pos string) bool {
	switch pos {
	case "top", "bottom", "left", "right":
		return true
	default:
		return false
	}
}
