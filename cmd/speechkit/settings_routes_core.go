package main

import (
	"net/http"

	"github.com/kombifyio/SpeechKit/internal/config"
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
