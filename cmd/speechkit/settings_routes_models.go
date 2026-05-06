package main

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
)

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
