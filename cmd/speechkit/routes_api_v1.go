package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/downloads"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type apiV1ModeState struct {
	Contracts []speechkit.ModeContract `json:"contracts"`
	Settings  speechkit.ModeSettings   `json:"settings"`
}

type apiV1ProfilesResponse struct {
	Profiles       []models.Profile         `json:"profiles"`
	ActiveProfiles map[string]string        `json:"activeProfiles"`
	Groups         map[string][]string      `json:"groups"`
	Contracts      []speechkit.ModeContract `json:"contracts"`
}

type apiV1DictionaryEntry struct {
	ID         int64  `json:"id,omitempty"`
	Spoken     string `json:"spoken"`
	Canonical  string `json:"canonical"`
	Language   string `json:"language,omitempty"`
	Source     string `json:"source,omitempty"`
	Enabled    bool   `json:"enabled"`
	UsageCount int    `json:"usageCount"`
}

type apiV1DictionaryResponse struct {
	Language string                 `json:"language"`
	Entries  []apiV1DictionaryEntry `json:"entries"`
}

type apiV1DictionaryImportRequest struct {
	Language string                 `json:"language"`
	Entries  []apiV1DictionaryEntry `json:"entries"`
}

func registerAPIV1Routes(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router, feedbackStore store.Store) {
	mux.HandleFunc("/api/v1/modes", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/modes" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, apiV1ModeState{
			Contracts: speechkit.DefaultModeContracts(),
			Settings:  apiV1ModeSettingsFromConfig(cfg),
		})
	})

	mux.HandleFunc("/api/v1/modes/", func(w http.ResponseWriter, r *http.Request) {
		mode, action, ok := parseAPIV1ModePath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		switch action {
		case "settings":
			handleAPIV1ModeSettings(w, r, cfgPath, cfg, state, sttRouter, mode)
		case "start":
			handleAPIV1ModeCommand(w, r, state, mode, speechkit.CommandStartMode)
		case "stop":
			handleAPIV1ModeCommand(w, r, state, mode, speechkit.CommandStopMode)
		default:
			http.NotFound(w, r)
		}
	})

	mux.HandleFunc("/api/v1/providers/profiles", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		catalog := filteredModelCatalog()
		activeProfiles := activeProfilesFromConfig(cfg, catalog)
		if state != nil {
			state.mu.Lock()
			if state.activeProfiles != nil {
				activeProfiles = cloneStringMap(state.activeProfiles)
			}
			state.mu.Unlock()
		}
		writeJSON(w, apiV1ProfilesResponse{
			Profiles:       catalog.Profiles,
			ActiveProfiles: activeProfiles,
			Groups:         apiV1ProviderGroups(catalog),
			Contracts:      speechkit.DefaultModeContracts(),
		})
	})

	mux.HandleFunc("/api/v1/providers/readiness", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, apiV1ReadinessForCatalog(r.Context(), cfg, filteredModelCatalog(), sttRouter))
	})

	mux.HandleFunc("/api/v1/dictionary", func(w http.ResponseWriter, r *http.Request) {
		handleAPIV1Dictionary(w, r, cfgPath, cfg, state, feedbackStore)
	})

	mux.HandleFunc("/api/v1/voice-sessions", func(w http.ResponseWriter, r *http.Request) {
		handleAPIV1VoiceSessions(w, r, feedbackStore)
	})

	mux.HandleFunc("/api/v1/providers/artifacts", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/providers/artifacts" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, apiV1ProviderArtifactsResponse{
			Artifacts: downloads.Catalog(r.Context(), cfg),
			Jobs:      apiV1DownloadJobs(state),
		})
	})

	mux.HandleFunc("/api/v1/providers/artifacts/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, apiV1DownloadJobs(state))
	})

	mux.HandleFunc("/api/v1/providers/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		artifactID, action, ok := parseAPIV1ProviderArtifactPath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		handleAPIV1ProviderArtifactAction(w, r, cfgPath, cfg, state, artifactID, action)
	})

	mux.HandleFunc("/api/v1/providers/", func(w http.ResponseWriter, r *http.Request) {
		profileID, action, ok := parseAPIV1ProviderPath(r.URL.Path)
		if !ok || action != "activate" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		profile, ok := findCatalogProfile(filteredModelCatalog(), profileID)
		if !ok {
			http.Error(w, "profile not found", http.StatusNotFound)
			return
		}
		if err := applyModelProfile(r.Context(), cfgPath, cfg, state, sttRouter, profile); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{
			"profileId": profile.ID,
			"mode":      apiV1ModeForModality(profile.Modality),
			"model":     profile.ModelID,
		})
	})
}

func handleAPIV1Dictionary(w http.ResponseWriter, r *http.Request, cfgPath string, cfg *config.Config, state *appState, feedbackStore store.Store) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, apiV1DictionaryFromStoreOrConfig(r.Context(), cfg, feedbackStore, r.URL.Query().Get("language")))
	case http.MethodPost:
		var req apiV1DictionaryImportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid dictionary payload", http.StatusBadRequest)
			return
		}
		language := strings.TrimSpace(req.Language)
		if language == "" && cfg != nil {
			language = strings.TrimSpace(cfg.General.Language)
		}
		raw := serializeAPIV1DictionaryEntries(req.Entries)
		if cfg != nil {
			cfg.Vocabulary.Dictionary = raw
			if err := config.Save(cfgPath, cfg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if state != nil {
			state.mu.Lock()
			state.vocabularyDictionary = raw
			state.mu.Unlock()
			state.syncSpeechKitSnapshot()
		}
		if err := syncVocabularyDictionaryStore(r.Context(), feedbackStore, language, raw); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, apiV1DictionaryFromStoreOrConfig(r.Context(), cfg, feedbackStore, language))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleAPIV1VoiceSessions(w http.ResponseWriter, r *http.Request, feedbackStore store.Store) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionStore, ok := feedbackStore.(store.VoiceAgentSessionStore)
	if !ok || sessionStore == nil {
		writeJSON(w, []store.VoiceAgentSession{})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	sessions, err := sessionStore.ListVoiceAgentSessions(r.Context(), store.ListOpts{Limit: limit})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, sessions)
}

func apiV1DictionaryFromStoreOrConfig(ctx context.Context, cfg *config.Config, feedbackStore store.Store, language string) apiV1DictionaryResponse {
	language = strings.TrimSpace(language)
	if language == "" && cfg != nil {
		language = strings.TrimSpace(cfg.General.Language)
	}
	if dictionaryStore := userDictionaryStoreFromFeedbackStore(feedbackStore); dictionaryStore != nil {
		entries, err := dictionaryStore.ListUserDictionaryEntries(ctx, language)
		if err == nil {
			return apiV1DictionaryResponse{
				Language: language,
				Entries:  apiV1DictionaryEntriesFromStore(entries),
			}
		}
	}

	raw := ""
	if cfg != nil {
		raw = cfg.Vocabulary.Dictionary
	}
	parsed := parseVocabularyDictionary(raw)
	entries := make([]apiV1DictionaryEntry, 0, len(parsed))
	for _, entry := range parsed {
		entries = append(entries, apiV1DictionaryEntry{
			Spoken:    entry.Spoken,
			Canonical: entry.Canonical,
			Language:  language,
			Source:    "settings",
			Enabled:   true,
		})
	}
	return apiV1DictionaryResponse{Language: language, Entries: entries}
}

func apiV1DictionaryEntriesFromStore(entries []store.UserDictionaryEntry) []apiV1DictionaryEntry {
	result := make([]apiV1DictionaryEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, apiV1DictionaryEntry{
			ID:         entry.ID,
			Spoken:     entry.Spoken,
			Canonical:  entry.Canonical,
			Language:   entry.Language,
			Source:     entry.Source,
			Enabled:    entry.Enabled,
			UsageCount: entry.UsageCount,
		})
	}
	return result
}

func serializeAPIV1DictionaryEntries(entries []apiV1DictionaryEntry) string {
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		spoken := strings.TrimSpace(entry.Spoken)
		canonical := strings.TrimSpace(entry.Canonical)
		if spoken == "" && canonical == "" {
			continue
		}
		if canonical == "" || strings.EqualFold(spoken, canonical) {
			if spoken != "" {
				lines = append(lines, spoken)
			} else {
				lines = append(lines, canonical)
			}
			continue
		}
		lines = append(lines, spoken+" => "+canonical)
	}
	return strings.Join(lines, "\n")
}

func handleAPIV1ModeCommand(w http.ResponseWriter, r *http.Request, state *appState, mode string, commandType speechkit.CommandType) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if state == nil || state.engine == nil {
		http.Error(w, "runtime command bus unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := state.engine.Commands().Dispatch(r.Context(), speechkit.Command{
		Type: commandType,
		Metadata: map[string]string{
			"mode": mode,
		},
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"mode": mode, "status": "accepted"})
}

func apiV1ProviderGroups(catalog models.Catalog) map[string][]string {
	groups := map[string][]string{
		modeDictate:    {},
		modeAssist:     {},
		modeVoiceAgent: {},
	}
	for _, profile := range catalog.Profiles {
		mode := apiV1ModeForModality(profile.Modality)
		if mode == "" {
			continue
		}
		key := mode + ":" + string(profile.ProviderKind)
		groups[key] = append(groups[key], profile.ID)
	}
	return groups
}

func parseAPIV1ModePath(path string) (mode, action string, ok bool) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/v1/modes/"), "/"), "/")
	if len(parts) != 2 {
		return "", "", false
	}
	mode = apiV1RuntimeModeAlias(parts[0])
	if mode == modeNone {
		return "", "", false
	}
	return mode, parts[1], true
}

func apiV1RuntimeModeAlias(value string) string {
	switch speechkit.NormalizeMode(speechkit.Mode(value)) {
	case speechkit.ModeDictation:
		return modeDictate
	case speechkit.ModeAssist:
		return modeAssist
	case speechkit.ModeVoiceAgent:
		return modeVoiceAgent
	default:
		return normalizeRuntimeMode(value, modeAssist)
	}
}

func parseAPIV1ProviderPath(path string) (profileID, action string, ok bool) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/v1/providers/"), "/"), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func parseAPIV1ProviderArtifactPath(path string) (artifactID, action string, ok bool) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/v1/providers/artifacts/"), "/"), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func apiV1ModeForModality(modality models.Modality) string {
	switch modality {
	case models.ModalitySTT:
		return modeDictate
	case models.ModalityAssist:
		return modeAssist
	case models.ModalityRealtimeVoice:
		return modeVoiceAgent
	default:
		return ""
	}
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(payload)
}
