package main

import (
	"context"
	"net/http"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/downloads"
)

type apiV1ProviderArtifactsResponse struct {
	Artifacts []downloads.Item    `json:"artifacts"`
	Jobs      []downloads.JobView `json:"jobs"`
}

func handleAPIV1ProviderArtifactAction(w http.ResponseWriter, r *http.Request, cfgPath string, cfg *config.Config, state *appState, artifactID, action string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	item, ok := downloadCatalogItem(r.Context(), cfg, artifactID)
	if !ok {
		http.Error(w, "artifact not found", http.StatusNotFound)
		return
	}

	switch action {
	case "download":
		manager := apiV1DownloadManager(state)
		if manager == nil {
			http.Error(w, "download manager unavailable", http.StatusServiceUnavailable)
			return
		}
		destDir := downloadDestinationDir(item, cfg)
		snap := manager.Start(item, destDir, func(done downloads.Item) { //nolint:contextcheck // completion callback runs after request context is gone; uses context.Background() internally
			switch done.Kind {
			case downloads.KindHTTP:
				_ = selectDownloadedHTTPModel(context.Background(), cfgPath, cfg, state, done)
			case downloads.KindOllama:
				_ = selectDownloadedOllamaModel(context.Background(), cfgPath, cfg, state, done)
			}
		})
		writeJSON(w, snap)
	case "select":
		if !item.Available {
			http.Error(w, "artifact not available", http.StatusBadRequest)
			return
		}
		switch item.Kind {
		case downloads.KindHTTP:
			if err := selectDownloadedHTTPModel(r.Context(), cfgPath, cfg, state, item); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		case downloads.KindOllama:
			if err := selectDownloadedOllamaModel(r.Context(), cfgPath, cfg, state, item); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		default:
			http.Error(w, "unsupported artifact kind", http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]string{
			"message":    "selected",
			"artifactId": item.ID,
			"profileId":  item.ProfileID,
		})
	default:
		http.NotFound(w, r)
	}
}

func apiV1DownloadManager(state *appState) *downloads.Manager {
	if state == nil {
		return nil
	}
	return state.downloads
}

func apiV1DownloadJobs(state *appState) []downloads.JobView {
	manager := apiV1DownloadManager(state)
	if manager == nil {
		return []downloads.JobView{}
	}
	return manager.AllJobs()
}
