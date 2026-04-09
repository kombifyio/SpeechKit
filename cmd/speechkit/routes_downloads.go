package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/downloads"
)

func registerDownloadRoutes(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState) {
	dm := state.downloads

	// GET /models/downloads/catalog â€” list all downloadable models with availability.
	mux.HandleFunc("/models/downloads/catalog", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		catalog := downloads.Catalog(cfg)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(catalog)
	})

	// GET /models/downloads/jobs â€” list active / recent download jobs.
	mux.HandleFunc("/models/downloads/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(dm.AllJobs())
	})

	// POST /models/downloads/start â€” start a download by catalog model_id.
	mux.HandleFunc("/models/downloads/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		modelID := r.FormValue("model_id")
		if modelID == "" {
			http.Error(w, "model_id required", http.StatusBadRequest)
			return
		}
		catalog := downloads.Catalog(cfg)
		var found *downloads.Item
		for i := range catalog {
			if catalog[i].ID == modelID {
				found = &catalog[i]
				break
			}
		}
		if found == nil {
			http.Error(w, "unknown model_id", http.StatusNotFound)
			return
		}

		destDir := downloads.ResolveWhisperModelsDir(cfg)
		snap := dm.Start(*found, destDir, func(item downloads.Item) {
			// After an HTTP (whisper) download: apply the model path to config.
			if item.Kind == downloads.KindHTTP {
				filename := filepath.Base(item.URL)
				cfg.Local.Model = filename
				cfg.Local.ModelPath = filepath.Join(destDir, filename)
				if cfgPath != "" {
					_ = config.Save(cfgPath, cfg)
				}
			}
		})

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(snap)
	})

	// POST /models/downloads/cancel â€” cancel a download by job_id.
	mux.HandleFunc("/models/downloads/cancel", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		jobID := r.FormValue("job_id")
		if !dm.CancelJob(jobID) {
			http.Error(w, "job not found or already completed", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "cancelled"})
	})
}
