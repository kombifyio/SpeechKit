package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/auth"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/features"
	"github.com/kombifyio/SpeechKit/internal/frontendassets"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/secrets"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// AppVersion is the current release version. Updated at release time.
var AppVersion = "0.1.3"

var revealAudioFileInShell = func(path string) error {
	return exec.Command("explorer.exe", "/select,", filepath.Clean(path)).Start()
}

func assetHandler(cfg *config.Config, cfgPath string, state *appState, sttRouter *router.Router, feedbackStore store.Store, installState *config.InstallState) http.Handler {
	mux := http.NewServeMux()
	registerOverlayRoutes(mux, cfg, state)
	registerSettingsRoutes(mux, cfgPath, cfg, state, sttRouter)
	registerDashboardRoutes(mux, state, feedbackStore)
	registerQuickNoteRoutes(mux, cfg, state, feedbackStore)
	registerFeatureRoutes(mux, installState)
	registerAuthRoutes(mux)
	registerAppRoutes(mux, installState)
	mux.Handle("/", http.FileServer(http.FS(frontendassets.Files())))
	return mux
}

func registerOverlayRoutes(mux *http.ServeMux, cfg *config.Config, state *appState) {
	mux.HandleFunc("/overlay/pill-panel/show", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		state.showPillPanel()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/overlay/pill-panel/hide", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		state.hidePillPanel()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/overlay/radial/show", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		state.showRadialMenu()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/overlay/radial/hide", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		state.hideRadialMenu()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/overlay/show-dashboard", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if state.dashboard != nil {
			showSettingsWindow(state.dashboard)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/overlay/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(state.overlaySnapshot())
	})
	_ = cfg
}

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
			_ = state.audioSession.ReconfigureDevice(deviceID)
		}
		_ = config.Save(cfgPath, cfg)
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
			_ = config.Save(cfgPath, cfg)
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"profiles":       models.DefaultCatalog().Profiles,
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

		// Look up profile in catalog
		catalog := models.DefaultCatalog()
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

		// Apply the profile to the runtime
		switch profile.Modality {
		case models.ModalitySTT:
			if state.sttRouter != nil {
				switch profile.ExecutionMode {
				case models.ExecutionModeHFRouted:
					if cfg.HuggingFace.Enabled {
						token, _, _ := config.ResolveHuggingFaceToken(cfg)
						if token != "" {
							state.sttRouter.SetHuggingFace(stt.NewHuggingFaceProvider(profile.ModelID, token))
							log.Printf("STT switched to %s (%s)", profile.Name, profile.ModelID)
						} else {
							http.Error(w, "HuggingFace token not configured", http.StatusBadRequest)
							return
						}
					} else {
						http.Error(w, "HuggingFace not enabled", http.StatusBadRequest)
						return
					}
				case models.ExecutionModeOpenAI:
					apiKey := config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv)
					if apiKey == "" {
						http.Error(w, "OpenAI API key not configured", http.StatusBadRequest)
						return
					}
					state.sttRouter.SetCloud("openai", stt.NewOpenAICompatibleProvider("openai", "https://api.openai.com", apiKey, profile.ModelID))
					log.Printf("STT switched to %s (%s)", profile.Name, profile.ModelID)
				case models.ExecutionModeGroq:
					apiKey := config.ResolveSecret(cfg.Providers.Groq.APIKeyEnv)
					if apiKey == "" {
						http.Error(w, "Groq API key not configured", http.StatusBadRequest)
						return
					}
					state.sttRouter.SetCloud("groq", stt.NewOpenAICompatibleProvider("groq", "https://api.groq.com/openai", apiKey, profile.ModelID))
					log.Printf("STT switched to %s (%s)", profile.Name, profile.ModelID)
				case models.ExecutionModeGoogle:
					apiKey := config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)
					if apiKey == "" {
						http.Error(w, "Google API key not configured", http.StatusBadRequest)
						return
					}
					state.sttRouter.SetCloud("google", stt.NewGoogleSTTProvider(apiKey, profile.ModelID))
					log.Printf("STT switched to %s (%s)", profile.Name, profile.ModelID)
				case models.ExecutionModeLocal:
					log.Printf("STT profile %s selected (local mode)", profile.Name)
				default:
					http.Error(w, "unsupported execution mode for STT", http.StatusBadRequest)
					return
				}
			}
		case models.ModalityUtility:
			log.Printf("Utility LLM profile %s selected (%s)", profile.Name, profile.ModelID)
		case models.ModalityAgent:
			log.Printf("Agent LLM profile %s selected (%s)", profile.Name, profile.ModelID)
		default:
			log.Printf("Profile %s activated (modality: %s)", profile.Name, profile.Modality)
		}

		state.setActiveProfile(modality, profileID)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"modality":  modality,
			"profileId": profileID,
			"model":     profile.ModelID,
		})
	})
}

func registerDashboardRoutes(mux *http.ServeMux, state *appState, feedbackStore store.Store) {
	mux.HandleFunc("/dashboard/audio", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		path, filename, err := resolveDashboardAudio(r.Context(), feedbackStore, r.URL.Query().Get("kind"), r.URL.Query().Get("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		http.ServeFile(w, r, path)
	})
	mux.HandleFunc("/dashboard/audio/reveal", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, msgFormParseError, http.StatusBadRequest)
			return
		}
		path, _, err := resolveDashboardAudio(r.Context(), feedbackStore, r.FormValue("kind"), r.FormValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if err := revealAudioFileInShell(path); err != nil {
			http.Error(w, fmt.Sprintf("reveal audio: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Audio opened in folder"})
	})
	mux.HandleFunc("/dashboard/history", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if feedbackStore == nil {
			_ = json.NewEncoder(w).Encode([]struct{}{})
			return
		}
		records, err := feedbackStore.ListTranscriptions(r.Context(), store.ListOpts{Limit: 20})
		if err != nil {
			_ = json.NewEncoder(w).Encode([]struct{}{})
			return
		}
		type historyEntry struct {
			ID         int64             `json:"id"`
			Text       string            `json:"text"`
			Language   string            `json:"language"`
			Provider   string            `json:"provider"`
			Model      string            `json:"model,omitempty"`
			DurationMs int64             `json:"durationMs"`
			LatencyMs  int64             `json:"latencyMs"`
			Audio      *store.AudioAsset `json:"audio,omitempty"`
			CreatedAt  string            `json:"createdAt"`
		}
		entries := make([]historyEntry, len(records))
		for i, rec := range records {
			entries[i] = historyEntry{
				ID:         rec.ID,
				Text:       rec.Text,
				Language:   rec.Language,
				Provider:   rec.Provider,
				Model:      rec.Model,
				DurationMs: rec.DurationMs,
				LatencyMs:  rec.LatencyMs,
				Audio:      rec.Audio,
				CreatedAt:  rec.CreatedAt.Format(time.RFC3339),
			}
		}
		_ = json.NewEncoder(w).Encode(entries)
	})
	mux.HandleFunc("/dashboard/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		state.mu.Lock()
		entries := make([]logEntry, len(state.logEntries))
		copy(entries, state.logEntries)
		state.mu.Unlock()
		_ = json.NewEncoder(w).Encode(entries)
	})
	mux.HandleFunc("/dashboard/quicknotes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if feedbackStore == nil {
			_ = json.NewEncoder(w).Encode([]struct{}{})
			return
		}
		notes, err := feedbackStore.ListQuickNotes(r.Context(), store.ListOpts{Limit: 20})
		if err != nil {
			_ = json.NewEncoder(w).Encode([]struct{}{})
			return
		}
		type noteEntry struct {
			ID         int64             `json:"id"`
			Text       string            `json:"text"`
			Language   string            `json:"language"`
			Provider   string            `json:"provider"`
			DurationMs int64             `json:"durationMs"`
			LatencyMs  int64             `json:"latencyMs"`
			Audio      *store.AudioAsset `json:"audio,omitempty"`
			Pinned     bool              `json:"pinned"`
			CreatedAt  string            `json:"createdAt"`
			UpdatedAt  string            `json:"updatedAt"`
		}
		entries := make([]noteEntry, len(notes))
		for i, n := range notes {
			entries[i] = noteEntry{
				ID:         n.ID,
				Text:       n.Text,
				Language:   n.Language,
				Provider:   n.Provider,
				DurationMs: n.DurationMs,
				LatencyMs:  n.LatencyMs,
				Audio:      n.Audio,
				Pinned:     n.Pinned,
				CreatedAt:  n.CreatedAt.Format(time.RFC3339),
				UpdatedAt:  n.UpdatedAt.Format(time.RFC3339),
			}
		}
		_ = json.NewEncoder(w).Encode(entries)
	})
	mux.HandleFunc("/dashboard/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		zero := map[string]interface{}{
			"transcriptions":        0,
			"quickNotes":            0,
			"totalWords":            0,
			"totalAudioDurationMs":  0,
			"averageWordsPerMinute": 0,
			"averageLatencyMs":      0,
		}
		if feedbackStore == nil {
			_ = json.NewEncoder(w).Encode(zero)
			return
		}
		stats, err := feedbackStore.Stats(r.Context())
		if err != nil {
			_ = json.NewEncoder(w).Encode(zero)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"transcriptions":        stats.Transcriptions,
			"quickNotes":            stats.QuickNotes,
			"totalWords":            stats.TotalWords,
			"totalAudioDurationMs":  stats.TotalAudioDurationMs,
			"averageWordsPerMinute": stats.AverageWordsPerMinute,
			"averageLatencyMs":      stats.AverageLatencyMs,
		})
	})
}

func resolveDashboardAudio(ctx context.Context, feedbackStore store.Store, kind string, idRaw string) (string, string, error) {
	if feedbackStore == nil {
		return "", "", fmt.Errorf("store not available")
	}
	id, err := strconv.ParseInt(strings.TrimSpace(idRaw), 10, 64)
	if err != nil || id <= 0 {
		return "", "", fmt.Errorf("invalid id")
	}

	switch strings.TrimSpace(kind) {
	case "transcription":
		rec, err := feedbackStore.GetTranscription(ctx, id)
		if err != nil {
			return "", "", fmt.Errorf("transcription not found")
		}
		if rec.AudioPath == "" {
			return "", "", fmt.Errorf("audio not available")
		}
		return rec.AudioPath, fmt.Sprintf("transcription-%d.wav", rec.ID), nil
	case "quicknote":
		note, err := feedbackStore.GetQuickNote(ctx, id)
		if err != nil {
			return "", "", fmt.Errorf("quick note not found")
		}
		if note.AudioPath == "" {
			return "", "", fmt.Errorf("audio not available")
		}
		return note.AudioPath, fmt.Sprintf("quicknote-%d.wav", note.ID), nil
	default:
		return "", "", fmt.Errorf("unsupported audio kind")
	}
}

func registerQuickNoteRoutes(mux *http.ServeMux, cfg *config.Config, state *appState, feedbackStore store.Store) {
	service := desktopQuickNoteService{
		cfg:           cfg,
		state:         state,
		feedbackStore: feedbackStore,
		host:          wailsQuickNoteHost{state: state},
	}
	mux.HandleFunc("/quicknotes/create", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if feedbackStore == nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Store not available"})
			return
		}
		if err := r.ParseForm(); err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": msgFormParseError})
			return
		}
		text := strings.TrimSpace(r.FormValue("text"))
		if text == "" {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Text is required"})
			return
		}
		id, err := feedbackStore.SaveQuickNote(r.Context(), text, cfg.General.Language, "manual", 0, 0, nil)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Save failed: %v", err)})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "message": "Quick Note saved"})
	})
	mux.HandleFunc("/quicknotes/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if feedbackStore == nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Store not available"})
			return
		}
		if err := r.ParseForm(); err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": msgFormParseError})
			return
		}
		idStr := r.FormValue("id")
		text := strings.TrimSpace(r.FormValue("text"))
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Invalid ID"})
			return
		}
		if err := feedbackStore.UpdateQuickNote(r.Context(), id, text); err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Update failed: %v", err)})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Updated"})
	})
	mux.HandleFunc("/quicknotes/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if feedbackStore == nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Store not available"})
			return
		}
		if err := r.ParseForm(); err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": msgFormParseError})
			return
		}
		id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Invalid ID"})
			return
		}
		if err := feedbackStore.DeleteQuickNote(r.Context(), id); err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Delete failed: %v", err)})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Deleted"})
	})
	mux.HandleFunc("/quicknotes/pin", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if feedbackStore == nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Store not available"})
			return
		}
		if err := r.ParseForm(); err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": msgFormParseError})
			return
		}
		id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Invalid ID"})
			return
		}
		pinned := r.FormValue("pinned") == "1"
		if err := feedbackStore.PinQuickNote(r.Context(), id, pinned); err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Pin failed: %v", err)})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Updated"})
	})
	mux.HandleFunc("/quicknotes/summary", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"summary": "AI summary will be available soon."})
	})
	mux.HandleFunc("/quicknotes/email", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"email": "Email draft generation will be available soon."})
	})
	mux.HandleFunc("/quicknotes/get", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if feedbackStore == nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"text": ""})
			return
		}
		idStr := r.URL.Query().Get("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"text": ""})
			return
		}
		n, err := feedbackStore.GetQuickNote(r.Context(), id)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"text": ""})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": n.ID, "text": n.Text, "language": n.Language,
			"provider": n.Provider, "durationMs": n.DurationMs, "audio": n.Audio,
			"createdAt": n.CreatedAt.Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/quicknotes/record-mode", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		noteID, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		if err := dispatchQuickNoteCommand(r.Context(), state, speechkit.Command{
			Type:   speechkit.CommandArmQuickNoteRecording,
			NoteID: noteID,
		}); err != nil {
			service.ArmRecording(noteID)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Quick Note recording armed"})
	})
	mux.HandleFunc("/quicknotes/open-editor", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var noteID int64
		if idParam := r.URL.Query().Get("id"); idParam != "" {
			parsedID, err := strconv.ParseInt(idParam, 10, 64)
			if err == nil {
				noteID = parsedID
			}
		}
		err := dispatchQuickNoteCommand(r.Context(), state, speechkit.Command{
			Type:   speechkit.CommandOpenQuickNote,
			NoteID: noteID,
		})
		if err == speechkit.ErrCommandHandlerUnavailable {
			err = service.OpenEditor(noteID)
		}
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Failed to open editor"})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Editor opened"})
	})
	mux.HandleFunc("/quicknotes/open-capture", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		err := dispatchQuickNoteCommand(r.Context(), state, speechkit.Command{
			Type: speechkit.CommandOpenQuickCapture,
		})
		if err == speechkit.ErrCommandHandlerUnavailable {
			_, err = service.OpenCapture(r.Context())
		}
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Failed to create note"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Capture opened"})
	})
	mux.HandleFunc("/quicknotes/close-capture", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		err := dispatchQuickNoteCommand(r.Context(), state, speechkit.Command{
			Type: speechkit.CommandCloseQuickCapture,
		})
		if err == speechkit.ErrCommandHandlerUnavailable {
			err = service.CloseCapture()
		}
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Failed to close capture"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Closed"})
	})
}

func dispatchQuickNoteCommand(ctx context.Context, state *appState, command speechkit.Command) error {
	if state == nil || state.engine == nil {
		return speechkit.ErrCommandHandlerUnavailable
	}
	return state.engine.Commands().Dispatch(ctx, command)
}

func registerFeatureRoutes(mux *http.ServeMux, installState *config.InstallState) {
	mux.HandleFunc("/features", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(features.Detect(installState))
	})
}

func registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/auth/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		provider := auth.GetAuthProvider()
		if provider == nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"available": false,
				"loggedIn":  false,
			})
			return
		}
		identity, _ := provider.GetIdentity(r.Context())
		resp := map[string]interface{}{
			"available": true,
			"loggedIn":  provider.IsLoggedIn(),
		}
		if identity != nil {
			resp["email"] = identity.Email
			resp["plan"] = identity.Plan
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		provider := auth.GetAuthProvider()
		if provider == nil {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Auth not available in this build. Use the kombify build for cloud features.",
			})
			return
		}
		resp, err := provider.StartDeviceCodeFlow(r.Context())
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/auth/poll", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		provider := auth.GetAuthProvider()
		if provider == nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Auth not available"})
			return
		}
		_ = r.ParseForm()
		deviceCode := r.FormValue("device_code")
		_, err := provider.PollDeviceCode(r.Context(), deviceCode)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"pending": true,
				"error":   err.Error(),
			})
			return
		}
		identity, err := provider.GetIdentity(r.Context())
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"pending":       false,
				"authenticated": provider.IsLoggedIn(),
				"error":         err.Error(),
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"pending":       false,
			"authenticated": provider.IsLoggedIn(),
			"identity":      identity,
		})
	})
	mux.HandleFunc("/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		provider := auth.GetAuthProvider()
		if provider != nil {
			_ = provider.Logout(r.Context())
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Logged out"})
	})
}

func saveSettings(ctx context.Context, req *http.Request, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router) string {
	if err := req.ParseForm(); err != nil {
		return msgFormParseError
	}

	dictateHotkey := strings.TrimSpace(req.FormValue("dictate_hotkey"))
	if dictateHotkey == "" {
		dictateHotkey = strings.TrimSpace(req.FormValue("hotkey"))
	}
	if dictateHotkey == "" {
		dictateHotkey = cfg.General.DictateHotkey
	}
	if dictateHotkey == "" {
		dictateHotkey = "win+alt"
	}
	agentHotkey := strings.TrimSpace(req.FormValue("agent_hotkey"))
	if agentHotkey == "" {
		agentHotkey = cfg.General.AgentHotkey
	}
	if agentHotkey == "" {
		agentHotkey = "ctrl+shift+k"
	}
	activeMode := strings.TrimSpace(req.FormValue("active_mode"))
	if activeMode == "" {
		activeMode = cfg.General.ActiveMode
	}
	if activeMode != "agent" {
		activeMode = "dictate"
	}
	audioDeviceID := strings.TrimSpace(req.FormValue("audio_device_id"))
	if audioDeviceID == "" {
		audioDeviceID = strings.TrimSpace(req.FormValue("selected_audio_device_id"))
	}
	if audioDeviceID == "" {
		audioDeviceID = cfg.Audio.DeviceID
	}
	modelValue := strings.TrimSpace(req.FormValue("hf_model"))
	if modelValue == "" {
		modelValue = cfg.HuggingFace.Model
	}
	if !isSupportedHFModel(modelValue) {
		return msgUnsupportedModel
	}

	overlayEnabled := req.FormValue("overlay_enabled") == "1"
	hfEnabledRaw := strings.TrimSpace(req.FormValue("hf_enabled"))
	hfEnabled := cfg.HuggingFace.Enabled
	if hfEnabledRaw != "" {
		hfEnabled = hfEnabledRaw == "1"
	}
	visualizerValue := strings.TrimSpace(req.FormValue("overlay_visualizer"))
	if visualizerValue == "" {
		visualizerValue = cfg.UI.Visualizer
	}
	if !isSupportedOverlayVisualizer(visualizerValue) {
		return msgUnsupportedVis
	}
	designValue := strings.TrimSpace(req.FormValue("overlay_design"))
	if designValue == "" {
		designValue = cfg.UI.Design
	}
	if !isSupportedOverlayDesign(designValue) {
		return msgUnsupportedDesign
	}
	overlayPosition := strings.TrimSpace(req.FormValue("overlay_position"))
	if overlayPosition == "" {
		overlayPosition = cfg.UI.OverlayPosition
	}
	if !isSupportedOverlayPosition(overlayPosition) {
		overlayPosition = "top"
	}
	storeSaveAudioRaw := strings.TrimSpace(req.FormValue("store_save_audio"))
	storeSaveAudio := cfg.Store.SaveAudio
	if storeSaveAudioRaw != "" {
		storeSaveAudio = storeSaveAudioRaw == "1"
	}
	storeBackend := strings.TrimSpace(req.FormValue("store_backend"))
	if storeBackend == "" {
		storeBackend = cfg.Store.Backend
	}
	if storeBackend == "" {
		storeBackend = "sqlite"
	}
	switch storeBackend {
	case "sqlite", "postgres":
	default:
		return msgUnsupportedStore
	}
	storeSQLitePath := strings.TrimSpace(req.FormValue("store_sqlite_path"))
	if storeSQLitePath == "" {
		storeSQLitePath = cfg.Store.SQLitePath
	}
	storePostgresDSN := strings.TrimSpace(req.FormValue("store_postgres_dsn"))
	if storePostgresDSN == "" {
		storePostgresDSN = cfg.Store.PostgresDSN
	}
	if storeBackend == "postgres" && storePostgresDSN == "" {
		return msgPostgresDSNReq
	}
	storeAudioRetentionDays := cfg.Store.AudioRetentionDays
	if raw := strings.TrimSpace(req.FormValue("store_audio_retention_days")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			storeAudioRetentionDays = parsed
		}
	}
	storeMaxAudioStorageMB := cfg.Store.MaxAudioStorageMB
	if raw := strings.TrimSpace(req.FormValue("store_max_audio_storage_mb")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			storeMaxAudioStorageMB = parsed
		}
	}

	nextCfg := *cfg
	nextCfg.General.Hotkey = dictateHotkey
	nextCfg.General.DictateHotkey = dictateHotkey
	nextCfg.General.AgentHotkey = agentHotkey
	nextCfg.General.ActiveMode = activeMode
	nextCfg.Audio.DeviceID = audioDeviceID
	nextCfg.HuggingFace.Enabled = hfEnabled
	nextCfg.HuggingFace.Model = modelValue
	nextCfg.UI.OverlayEnabled = overlayEnabled
	nextCfg.UI.OverlayPosition = overlayPosition
	nextCfg.UI.Visualizer = visualizerValue
	nextCfg.UI.Design = designValue
	nextCfg.Store.Backend = storeBackend
	nextCfg.Store.SQLitePath = storeSQLitePath
	nextCfg.Store.PostgresDSN = storePostgresDSN
	nextCfg.Store.SaveAudio = storeSaveAudio
	nextCfg.Store.AudioRetentionDays = storeAudioRetentionDays
	nextCfg.Store.MaxAudioStorageMB = storeMaxAudioStorageMB
	nextCfg.Feedback.SaveAudio = storeSaveAudio
	nextCfg.Feedback.AudioRetentionDays = storeAudioRetentionDays
	if storeBackend == "sqlite" {
		nextCfg.Feedback.DBPath = storeSQLitePath
	}
	oldDictateHotkey := cfg.General.DictateHotkey
	oldAgentHotkey := cfg.General.AgentHotkey
	oldAudioDeviceID := cfg.Audio.DeviceID

	managedHFEnabled := config.ApplyManagedIntegrationDefaults(&nextCfg)
	needsHFRefresh := managedHFEnabled ||
		!cfg.HuggingFace.Enabled ||
		modelValue != cfg.HuggingFace.Model
	shouldValidateHF := nextCfg.HuggingFace.Enabled && needsHFRefresh
	if shouldValidateHF {
		if err := refreshHuggingFaceProvider(ctx, &nextCfg, sttRouter, managedHFEnabled && hfEnabledRaw != "1"); err != nil {
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
		dictateHotkey,
		agentHotkey,
		activeMode,
		audioDeviceID,
		sttRouter.AvailableProviders(),
		visualizerValue,
		designValue,
		overlayPosition,
	)
	state.applyDesktopSettings(oldDictateHotkey, oldAgentHotkey, dictateHotkey, agentHotkey, oldAudioDeviceID, audioDeviceID, overlayEnabled)

	return msgSaved
}

var errMissingHuggingFaceToken = fmt.Errorf("missing hugging face token")

func refreshHuggingFaceProvider(ctx context.Context, cfg *config.Config, sttRouter *router.Router, skipHealthCheck bool) error {
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
	token := strings.TrimSpace(req.FormValue("hf_token"))
	if token == "" {
		return msgHFTokenRequired
	}
	if err := secrets.SetUserHuggingFaceToken(token); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
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

func registerAppRoutes(mux *http.ServeMux, installState *config.InstallState) {
	mux.HandleFunc("/app/version", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		resp := map[string]interface{}{
			"version": AppVersion,
		}
		// Check for updates (non-blocking, cached)
		if latest, url, ok := cachedLatestRelease(); ok && latest != "" && latest != AppVersion {
			resp["latestVersion"] = latest
			resp["updateURL"] = url
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/app/setup-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"setupDone": installState.SetupDone,
		})
	})

	mux.HandleFunc("/app/complete-setup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		installState.SetupDone = true
		_ = config.SaveInstallState(installState)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
}
