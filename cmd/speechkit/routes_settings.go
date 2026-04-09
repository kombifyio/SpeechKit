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
		message := saveSettings(r.Context(), r, cfgPath, cfg, state, sttRouter)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/huggingface/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		message := saveHuggingFaceToken(r.Context(), r, cfg, sttRouter)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/huggingface/token/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		message := clearHuggingFaceToken(r.Context(), cfg, sttRouter)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/provider-credentials/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, msgFormParseError, http.StatusBadRequest)
			return
		}
		message, err := saveProviderCredential(r.Context(), r.FormValue("provider"), r.FormValue("credential"), cfg, sttRouter)
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
		if err := r.ParseForm(); err != nil {
			http.Error(w, msgFormParseError, http.StatusBadRequest)
			return
		}
		message, err := clearProviderCredential(r.Context(), r.FormValue("provider"), cfg, sttRouter)
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
			activeMode := state.activeMode
			state.mu.Unlock()
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]string{"activeMode": activeMode})
		case http.MethodPost:
			if err := r.ParseForm(); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mode := strings.TrimSpace(r.FormValue("mode"))
			if mode != "dictate" && mode != "agent" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			state.setActiveMode(mode)
			cfg.General.ActiveMode = mode
			if err := config.Save(cfgPath, cfg); err != nil {
				slog.Warn("save active mode config", "err", err)
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]string{"activeMode": mode})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
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
			slog.Warn("apply model profile", "profileId", profileID, "err", err)
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
	oldDictateHotkey := cfg.General.DictateHotkey
	oldAgentHotkey := cfg.General.AgentHotkey
	oldAudioDeviceID := cfg.Audio.DeviceID

	managedHFEnabled := config.ApplyManagedIntegrationDefaults(&nextCfg)
	needsHFRefresh := managedHFEnabled ||
		!cfg.HuggingFace.Enabled ||
		form.HFModel != cfg.HuggingFace.Model
	shouldValidateHF := nextCfg.HuggingFace.Enabled && needsHFRefresh
	if shouldValidateHF {
		if err := refreshHuggingFaceProvider(ctx, &nextCfg, sttRouter, managedHFEnabled); err != nil {
			if err == errMissingHuggingFaceToken {
				return msgHFTokenMissing
			}
			return fmt.Sprintf(msgModelUnreachable, err)
		}
	}
	if !nextCfg.HuggingFace.Enabled {
		sttRouter.SetHuggingFace(nil)
	}

	*cfg = nextCfg

	if err := config.Save(cfgPath, cfg); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}

	state.applyRuntimeSettings(
		form.DictateHotkey,
		form.AgentHotkey,
		form.ActiveMode,
		form.AudioDeviceID,
		runtimeAvailableProviders(sttRouter),
		form.Visualizer,
		form.Design,
		form.OverlayPosition,
		form.VocabularyDictionary,
		form.OverlayMovable,
		form.OverlayFreeX,
		form.OverlayFreeY,
	)
	state.applyDesktopSettings(oldDictateHotkey, oldAgentHotkey, form.DictateHotkey, form.AgentHotkey, oldAudioDeviceID, form.AudioDeviceID, form.OverlayEnabled)

	return msgSaved
}

// settingsFormData holds parsed and validated form values from the settings page.
type settingsFormData struct {
	DictateHotkey        string
	AgentHotkey          string
	ActiveMode           string
	AudioDeviceID        string
	HFModel              string
	OverlayEnabled       bool
	Visualizer           string
	Design               string
	OverlayPosition      string
	OverlayMovable       bool
	OverlayFreeX         int
	OverlayFreeY         int
	StoreBackend         string
	StoreSQLitePath      string
	StorePostgresDSN     string
	StoreSaveAudio       bool
	StoreAudioRetention  int
	StoreMaxAudioStorage int
	VocabularyDictionary string
}

// parseSettingsForm extracts and validates all settings form values.
// Returns the parsed data and an empty string on success, or an error message.
func parseSettingsForm(req *http.Request, cfg *config.Config) (settingsFormData, string) {
	var f settingsFormData

	f.DictateHotkey = strings.TrimSpace(req.FormValue("dictate_hotkey"))
	if f.DictateHotkey == "" {
		f.DictateHotkey = strings.TrimSpace(req.FormValue("hotkey"))
	}
	if f.DictateHotkey == "" {
		f.DictateHotkey = cfg.General.DictateHotkey
	}
	if f.DictateHotkey == "" {
		f.DictateHotkey = "win+alt"
	}
	f.AgentHotkey = strings.TrimSpace(req.FormValue("agent_hotkey"))
	if f.AgentHotkey == "" {
		f.AgentHotkey = cfg.General.AgentHotkey
	}
	if f.AgentHotkey == "" {
		f.AgentHotkey = "ctrl+shift+k"
	}
	f.ActiveMode = strings.TrimSpace(req.FormValue("active_mode"))
	if f.ActiveMode == "" {
		f.ActiveMode = cfg.General.ActiveMode
	}
	if f.ActiveMode != "agent" {
		f.ActiveMode = "dictate"
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

	return f, ""
}

// buildNextConfig creates a new Config from the parsed form values.
func buildNextConfig(form settingsFormData, cfg *config.Config) config.Config {
	hfAvailableInBuild := config.ManagedHuggingFaceAvailableInBuild()
	nextCfg := *cfg
	nextCfg.General.Hotkey = form.DictateHotkey // keep legacy field in sync
	nextCfg.General.DictateHotkey = form.DictateHotkey
	nextCfg.General.AgentHotkey = form.AgentHotkey
	nextCfg.General.ActiveMode = form.ActiveMode
	nextCfg.Audio.DeviceID = form.AudioDeviceID
	nextCfg.HuggingFace.Enabled = cfg.HuggingFace.Enabled && hfAvailableInBuild
	nextCfg.HuggingFace.Model = form.HFModel
	nextCfg.UI.OverlayEnabled = form.OverlayEnabled
	nextCfg.UI.OverlayPosition = form.OverlayPosition
	nextCfg.UI.OverlayMovable = form.OverlayMovable
	nextCfg.UI.OverlayFreeX = form.OverlayFreeX
	nextCfg.UI.OverlayFreeY = form.OverlayFreeY
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
	return nextCfg
}

func normalizeVocabularyDictionary(input string) string {
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

func saveHuggingFaceToken(ctx context.Context, req *http.Request, cfg *config.Config, sttRouter *router.Router) string {
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
			if err == errMissingHuggingFaceToken {
				return msgHFTokenMissing
			}
			return fmt.Sprintf(msgModelUnreachable, err)
		}
	}
	return msgHFTokenSaved
}

func clearHuggingFaceToken(ctx context.Context, cfg *config.Config, sttRouter *router.Router) string {
	if !config.ManagedHuggingFaceAvailableInBuild() {
		return msgHFUnavailableBuild
	}
	if err := secrets.ClearUserHuggingFaceToken(); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}
	if cfg.HuggingFace.Enabled {
		if err := refreshHuggingFaceProvider(ctx, cfg, sttRouter, true); err != nil {
			if err == errMissingHuggingFaceToken {
				sttRouter.SetHuggingFace(nil)
				return msgHFTokenCleared
			}
			return fmt.Sprintf(msgModelUnreachable, err)
		}
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
		case models.ModalitySTT, models.ModalityUtility, models.ModalityAgent, models.ModalityRealtimeVoice:
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
