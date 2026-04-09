package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/store"
)

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

