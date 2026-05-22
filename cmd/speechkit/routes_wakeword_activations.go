package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/wakeword"
)

// registerWakewordActivationRoutes wires the v0.37.6 local Settings-UI
// endpoints that surface activation clips captured by TrainingCapture
// (v0.37.4) so the user can browse, label, delete, or playback them
// without leaving the SpeechKit window.
//
// All routes are LOCAL to the Wails control plane (the same loopback
// HTTP server that handles /api/wakeword/selftest etc). They read
// directly from the configured `[wakeword.training_data].local_capture_dir`
// — they do NOT talk to the remote v0.37.5 REST endpoints. The remote
// upload pipeline is independent: enabling upload in Settings sends
// the same files to the server in the background.
//
// Endpoints
//
//	GET    /api/wakeword/activations              — list local captures
//	GET    /api/wakeword/activations/{id}/audio   — stream WAV bytes
//	PATCH  /api/wakeword/activations/{id}         — update label
//	DELETE /api/wakeword/activations/{id}         — remove .wav + .json
//
// Privacy gate: when local_capture_enabled is false (the default) the
// directory may not exist or may be empty; the list endpoint then
// returns `{"activations":[], "count":0}` rather than a 404 so the UI
// can render an empty-state hint instead of erroring out.
func registerWakewordActivationRoutes(mux *http.ServeMux, cfg *config.Config) {
	mux.HandleFunc("/api/wakeword/activations", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListWakewordActivations(w, r, cfg)
		default:
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/wakeword/activations/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/wakeword/activations/")
		if path == "" {
			http.Error(w, "activation id missing", http.StatusNotFound)
			return
		}
		id, sub := splitActivationPath(path)
		if id == "" {
			http.Error(w, "activation id missing", http.StatusNotFound)
			return
		}
		switch {
		case sub == "audio" && r.Method == http.MethodGet:
			handleStreamWakewordActivationAudio(w, r, cfg, id)
		case sub == "" && r.Method == http.MethodPatch:
			handlePatchWakewordActivationLabel(w, r, cfg, id)
		case sub == "" && r.Method == http.MethodDelete:
			handleDeleteWakewordActivation(w, r, cfg, id)
		default:
			w.Header().Set("Allow", "GET, PATCH, DELETE")
			http.Error(w, "unsupported method or sub-resource", http.StatusMethodNotAllowed)
		}
	})
}

// LocalWakewordActivation is the JSON shape returned to the Settings UI.
// It mirrors the on-disk wakeword.TrainingRecord but adds the
// derived `audio_url` field the frontend uses to fetch playable bytes.
type LocalWakewordActivation struct {
	ID         string  `json:"id"`
	PhraseID   string  `json:"phrase_id"`
	Phrase     string  `json:"phrase"`
	Backend    string  `json:"backend"`
	Score      float32 `json:"score"`
	CapturedAt string  `json:"captured_at"`
	PreRollMs  int     `json:"pre_roll_ms"`
	PostRollMs int     `json:"post_roll_ms"`
	SampleRate int     `json:"sample_rate"`
	AudioBytes int     `json:"audio_bytes"`
	Label      string  `json:"label,omitempty"`
	Uploaded   bool    `json:"uploaded"`
	AudioURL   string  `json:"audio_url"`
}

type listWakewordActivationsResponse struct {
	Activations []LocalWakewordActivation `json:"activations"`
	Count       int                       `json:"count"`
	Dir         string                    `json:"dir"`
	Enabled     bool                      `json:"enabled"`
}

func handleListWakewordActivations(w http.ResponseWriter, _ *http.Request, cfg *config.Config) {
	dir := resolveLocalCaptureDir(cfg)
	enabled := cfg != nil && cfg.Wakeword.TrainingData.LocalCaptureEnabled
	resp := listWakewordActivationsResponse{
		Activations: []LocalWakewordActivation{},
		Dir:         dir,
		Enabled:     enabled,
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		// Dir not yet created (capture disabled or fresh install) is
		// not an error — return an empty list with the dir + enabled
		// flag so the UI can render its empty-state hint.
		if errors.Is(err, os.ErrNotExist) {
			writeWakewordActivationJSON(w, http.StatusOK, resp)
			return
		}
		http.Error(w, fmt.Sprintf("read capture dir: %v", err), http.StatusInternalServerError)
		return
	}

	type pair struct{ jsonPath, wavPath string }
	pairs := []pair{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		jsonPath := filepath.Join(dir, e.Name())
		wavPath := strings.TrimSuffix(jsonPath, ".json") + ".wav"
		if _, err := os.Stat(wavPath); err != nil {
			continue
		}
		pairs = append(pairs, pair{jsonPath, wavPath})
	}
	// Newest-first ordering — filenames embed the timestamp so a
	// reverse lexicographic sort gives us reverse-chronological.
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].jsonPath > pairs[j].jsonPath })

	for _, p := range pairs {
		raw, err := os.ReadFile(p.jsonPath)
		if err != nil {
			continue
		}
		var rec wakeword.TrainingRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			continue
		}
		resp.Activations = append(resp.Activations, LocalWakewordActivation{
			ID:         rec.ID,
			PhraseID:   rec.PhraseID,
			Phrase:     rec.Phrase,
			Backend:    rec.Backend,
			Score:      rec.Score,
			CapturedAt: rec.CapturedAt.Format("2006-01-02T15:04:05Z07:00"),
			PreRollMs:  rec.PreRollMs,
			PostRollMs: rec.PostRollMs,
			SampleRate: rec.SampleRate,
			AudioBytes: rec.AudioBytes,
			Label:      rec.Label,
			Uploaded:   rec.Uploaded,
			AudioURL:   "/api/wakeword/activations/" + rec.ID + "/audio",
		})
	}
	resp.Count = len(resp.Activations)
	writeWakewordActivationJSON(w, http.StatusOK, resp)
}

func handleStreamWakewordActivationAudio(w http.ResponseWriter, _ *http.Request, cfg *config.Config, id string) {
	dir := resolveLocalCaptureDir(cfg)
	pair, err := findActivationPair(dir, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	f, err := os.Open(pair.wavPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusGone)
		return
	}
	defer func() { _ = f.Close() }()
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filepath.Base(pair.wavPath)))
	_, _ = io.Copy(w, f)
}

type patchActivationLabelRequest struct {
	Label string `json:"label"`
}

func handlePatchWakewordActivationLabel(w http.ResponseWriter, r *http.Request, cfg *config.Config, id string) {
	var req patchActivationLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if !isValidActivationLabel(req.Label) {
		http.Error(w, "label must be 'correct', 'false_positive', or empty", http.StatusBadRequest)
		return
	}
	dir := resolveLocalCaptureDir(cfg)
	pair, err := findActivationPair(dir, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	raw, err := os.ReadFile(pair.jsonPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var rec wakeword.TrainingRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		http.Error(w, "sidecar JSON malformed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	rec.Label = req.Label
	// Label edit makes the record "fresh" for the uploader so it
	// re-syncs to the server. Resetting uploaded=false lets the
	// background uploader pick the changed clip up on its next tick.
	rec.Uploaded = false
	out, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(pair.jsonPath, out, 0o600); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleDeleteWakewordActivation(w http.ResponseWriter, _ *http.Request, cfg *config.Config, id string) {
	dir := resolveLocalCaptureDir(cfg)
	pair, err := findActivationPair(dir, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := os.Remove(pair.wavPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		http.Error(w, "remove wav: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Remove(pair.jsonPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		http.Error(w, "remove json: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── helpers ────────────────────────────────────────────────────────────────

type activationPair struct {
	jsonPath string
	wavPath  string
}

// findActivationPair walks dir for a *.json sidecar whose `id` field
// matches the requested id. Returns the json + wav paths.
func findActivationPair(dir, id string) (activationPair, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return activationPair{}, fmt.Errorf("activation dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		jsonPath := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(jsonPath)
		if err != nil {
			continue
		}
		var rec wakeword.TrainingRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			continue
		}
		if rec.ID == id {
			wavPath := strings.TrimSuffix(jsonPath, ".json") + ".wav"
			return activationPair{jsonPath: jsonPath, wavPath: wavPath}, nil
		}
	}
	return activationPair{}, errors.New("activation not found")
}

// resolveLocalCaptureDir mirrors the resolver used by the sidecar
// spawn path so the UI sees the same files the sidecar writes.
func resolveLocalCaptureDir(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return resolveTrainingCaptureDir(cfg.Wakeword.TrainingData.LocalCaptureDir)
}

// splitActivationPath separates "<id>" from "<id>/<subresource>" in
// the URL path tail.
func splitActivationPath(path string) (id, sub string) {
	if slash := strings.Index(path, "/"); slash >= 0 {
		return path[:slash], path[slash+1:]
	}
	return path, ""
}

func isValidActivationLabel(s string) bool {
	switch s {
	case "", "correct", "false_positive":
		return true
	}
	return false
}

// writeWakewordActivationJSON is the activation-route-local helper.
// (routes_api_v1.go already exposes a package-level writeJSON without
// a status arg; ours needs explicit status codes.)
func writeWakewordActivationJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
