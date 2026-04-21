package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/downloads"
	"github.com/kombifyio/SpeechKit/internal/models"
)

func registerDownloadRoutes(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState) {
	dm := state.downloads

	// GET /models/downloads/catalog — list all downloadable models with availability.
	mux.HandleFunc("/models/downloads/catalog", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		catalog := downloads.Catalog(r.Context(), cfg)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(catalog)
	})

	// GET /models/downloads/jobs — list active / recent download jobs.
	mux.HandleFunc("/models/downloads/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(dm.AllJobs())
	})

	// POST /models/downloads/start — start a download by catalog model_id.
	mux.HandleFunc("/models/downloads/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		modelID := r.FormValue("model_id")
		if modelID == "" {
			http.Error(w, "model_id required", http.StatusBadRequest)
			return
		}
		catalog := downloads.Catalog(r.Context(), cfg)
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

		destDir := downloadDestinationDir(*found, cfg)
		snap := dm.Start(*found, destDir, func(item downloads.Item) { //nolint:contextcheck // completion callback runs after request context is gone; uses context.Background() internally
			switch item.Kind {
			case downloads.KindHTTP:
				_ = selectDownloadedHTTPModel(context.Background(), cfgPath, cfg, state, item)
			case downloads.KindOllama:
				_ = selectDownloadedOllamaModel(context.Background(), cfgPath, cfg, state, item)
			}
		})

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(snap)
	})

	// POST /models/downloads/cancel — cancel a download by job_id.
	mux.HandleFunc("/models/downloads/cancel", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
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

	// POST /models/downloads/select — select an already-downloaded local model.
	mux.HandleFunc("/models/downloads/select", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		modelID := r.FormValue("model_id")
		if modelID == "" {
			http.Error(w, "model_id required", http.StatusBadRequest)
			return
		}

		item, ok := downloadCatalogItem(r.Context(), cfg, modelID)
		if !ok {
			http.Error(w, "unknown model_id", http.StatusNotFound)
			return
		}
		if !item.Available {
			http.Error(w, "model not downloaded", http.StatusBadRequest)
			return
		}

		modelName := filepath.Base(item.URL)
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
			modelName = item.OllamaModel
		default:
			http.Error(w, "unsupported model download kind", http.StatusBadRequest)
			return
		}

		if modelName == "" {
			modelName = item.ID
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message": "selected",
			"modelId": item.ID,
			"model":   modelName,
		})
	})
}

func downloadCatalogItem(ctx context.Context, cfg *config.Config, modelID string) (downloads.Item, bool) {
	catalog := downloads.Catalog(ctx, cfg)
	for i := range catalog {
		if catalog[i].ID == modelID {
			return catalog[i], true
		}
	}
	return downloads.Item{}, false
}

func downloadDestinationDir(item downloads.Item, cfg *config.Config) string {
	profile, ok := downloadProfileForItem(item)
	if ok && profile.ExecutionMode == models.ExecutionModeLocal {
		switch profile.Modality {
		case models.ModalityAssist, models.ModalityUtility, models.ModalityRealtimeVoice:
			return downloads.ResolveLocalLLMModelsDir(cfg)
		}
	}
	return downloads.ResolveWhisperModelsDir(cfg)
}

func downloadProfileForItem(item downloads.Item) (models.Profile, bool) {
	return findCatalogProfile(filteredModelCatalog(), item.ProfileID)
}

func selectDownloadedHTTPModel(ctx context.Context, cfgPath string, cfg *config.Config, state *appState, item downloads.Item) error {
	profile, ok := downloadProfileForItem(item)
	if !ok || profile.ExecutionMode != models.ExecutionModeLocal {
		return &downloadProfileError{profileID: item.ProfileID}
	}

	switch profile.Modality {
	case models.ModalitySTT:
		return selectDownloadedLocalModel(ctx, cfgPath, cfg, state, item)
	case models.ModalityAssist, models.ModalityUtility, models.ModalityRealtimeVoice:
		return selectDownloadedLocalLLMModel(ctx, cfgPath, cfg, state, item, profile)
	default:
		return fmt.Errorf("unsupported local download modality %q", profile.Modality)
	}
}

func selectDownloadedOllamaModel(ctx context.Context, cfgPath string, cfg *config.Config, state *appState, item downloads.Item) error {
	if item.OllamaModel == "" {
		return errors.New("ollama model missing")
	}

	profile, ok := downloadProfileForItem(item)
	if !ok || profile.ExecutionMode != models.ExecutionModeOllama {
		return errUnknownOllamaProfile(item.ProfileID)
	}

	return applyModelProfile(ctx, cfgPath, cfg, state, nil, profile)
}

func errUnknownOllamaProfile(profileID string) error {
	return &downloadProfileError{profileID: profileID}
}

type downloadProfileError struct {
	profileID string
}

func (e *downloadProfileError) Error() string {
	if e.profileID == "" {
		return "download item has no model profile"
	}
	return "unknown model profile: " + e.profileID
}

func selectDownloadedLocalModel(ctx context.Context, cfgPath string, cfg *config.Config, state *appState, item downloads.Item) error {
	destDir := downloads.ResolveWhisperModelsDir(cfg)
	filename := filepath.Base(item.URL)
	modelPath := filepath.Join(destDir, filename)
	if err := validateLocalProviderActivation(cfg, modelPath); err != nil {
		return err
	}

	cfg.Local.Enabled = true
	cfg.Routing.Strategy = "local-only"
	cfg.Local.Model = filename
	cfg.Local.ModelPath = modelPath
	if cfgPath != "" {
		if err := config.Save(cfgPath, cfg); err != nil {
			return err
		}
	}
	if state != nil {
		syncConfiguredLocalProvider(ctx, cfg, state, nil)
		state.mu.Lock()
		state.activeProfiles = activeProfilesFromConfig(cfg, filteredModelCatalog())
		state.mu.Unlock()
		syncRuntimeProviders(ctx, state, state.sttRouter)
	}
	return nil
}

func selectDownloadedLocalLLMModel(ctx context.Context, cfgPath string, cfg *config.Config, state *appState, item downloads.Item, profile models.Profile) error {
	destDir := downloads.ResolveLocalLLMModelsDir(cfg)
	filename := filepath.Base(item.URL)
	if filename == "" || filename == "." {
		return errors.New("download model filename missing")
	}
	modelPath := filepath.Join(destDir, filename)
	if !downloads.FileIsPresent(modelPath) {
		return fmt.Errorf("model not downloaded: %s", modelPath)
	}

	switch profile.Modality {
	case models.ModalityAssist:
		clearAssistModels(cfg)
	case models.ModalityUtility:
		clearUtilityModels(cfg)
	case models.ModalityRealtimeVoice:
		clearAgentModels(cfg)
	default:
		return fmt.Errorf("unsupported local LLM download modality %q", profile.Modality)
	}

	cfg.LocalLLM.Enabled = true
	if cfg.LocalLLM.BaseURL == "" {
		cfg.LocalLLM.BaseURL = config.DefaultLocalLLMBaseURL
	}
	if cfg.LocalLLM.Port == 0 {
		cfg.LocalLLM.Port = 8082
	}
	cfg.LocalLLM.Model = filename
	cfg.LocalLLM.ModelPath = modelPath

	switch profile.Modality {
	case models.ModalityAssist:
		cfg.LocalLLM.AssistModel = filename
	case models.ModalityUtility:
		cfg.LocalLLM.UtilityModel = filename
	case models.ModalityRealtimeVoice:
		cfg.LocalLLM.AgentModel = filename
		cfg.VoiceAgent.Enabled = true
		cfg.VoiceAgent.Model = filename
		cfg.VoiceAgent.PipelineFallback = true
	}

	if cfgPath != "" {
		if err := config.Save(cfgPath, cfg); err != nil {
			return err
		}
	}
	if err := rebuildAIRuntime(ctx, state, cfg); err != nil {
		return err
	}
	if state != nil {
		state.mu.Lock()
		state.activeProfiles = activeProfilesFromConfig(cfg, filteredModelCatalog())
		state.mu.Unlock()
	}
	return nil
}
