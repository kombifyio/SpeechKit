//go:build linux

// Package wakewordmodels serves SpeechKit's wake-word MODEL catalog to devices,
// so a wake phrase trained via SpeechKit can be individualized on both device
// families from one contract:
//
//   - ESPHome voice satellites and the Kombify-Box firmware consume the
//     microWakeWord v2 manifest (GET .../{id}/manifest.json) + its TFLite
//     (GET .../{id}/model.tflite). The manifest shape is byte-compatible with
//     esphome/micro-wake-word-models so an ESPHome `micro_wake_word:` block can
//     reference the manifest URL directly.
//   - Host consumers (desktop, box companion) read the openWakeWord ONNX
//     triplet (GET .../{id}/openwakeword) — the same artifacts the desktop
//     download catalog ships.
//
// Endpoints are PUBLIC (no bearer): a low-power device has no credential to
// present, and every payload is already-public model metadata / a redirect to
// already-public model bytes. See app/internal/server/core/testui.go
// serverPublicRoutes for the auth carve-out. The activation-collector endpoints
// (app/internal/server/wakewordtraining) stay authenticated — do not widen those.
//
// The package serves metadata and redirects only. It never reads or streams a
// model file: the catalog URLs name the Cloudflare R2 origin
// (wakewordcatalog.modelsBase), so no byte of a model passes through the
// server process (D-2026-10-25-03).
//
// Contract vs. consumer: this package SERVES the manifest/redirect contract.
// Producing the microWakeWord .tflite (tools/wakeword-training/microwakeword)
// and consuming it on-device (ESPHome / esp-tflite-micro) are separate
// workstreams. Until a phrase's microWakeWord variant is published (registry
// Available flag), the manifest/tflite routes report it as pending.
package wakewordmodels

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/app/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/app/internal/wakewordcatalog"
)

// Options configures the handler.
type Options struct {
	// Enabled mounts the routes but, when false, every request returns 503 so
	// a device can detect the operator disabled model serving.
	Enabled bool
	// Author / Website populate the microWakeWord manifest attribution. Default
	// to a framework-neutral name so no product branding leaks into the OSS
	// server surface.
	Author  string
	Website string
	// PublicBaseURL is the externally-reachable origin (scheme://host[:port])
	// used to build absolute manifest/model URLs in the list response. Empty =
	// derive from each request's scheme + Host.
	PublicBaseURL string
}

// Handler is the HTTP mount.
type Handler struct {
	enabled       bool
	author        string
	website       string
	publicBaseURL string
}

// New returns a ready-to-mount handler.
func New(opts Options) *Handler {
	h := &Handler{
		enabled:       opts.Enabled,
		author:        strings.TrimSpace(opts.Author),
		website:       strings.TrimSpace(opts.Website),
		publicBaseURL: strings.TrimRight(strings.TrimSpace(opts.PublicBaseURL), "/"),
	}
	if h.author == "" {
		h.author = "SpeechKit"
	}
	return h
}

// Mount wires the handler onto mux.
//
//	GET /v1/wakeword/models                     — list phrases + formats
//	GET /v1/wakeword/models/{id}/manifest.json  — microWakeWord v2 manifest
//	GET /v1/wakeword/models/{id}/model.tflite   — redirect to the TFLite bytes
//	GET /v1/wakeword/models/{id}/openwakeword   — openWakeWord ONNX triplet URLs
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/wakeword/models", h.collection)
	mux.HandleFunc("/v1/wakeword/models/", h.item)
}

func (h *Handler) collection(w http.ResponseWriter, r *http.Request) {
	if !h.guard(w, r) {
		return
	}
	h.list(w, r)
}

func (h *Handler) item(w http.ResponseWriter, r *http.Request) {
	if !h.guard(w, r) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/v1/wakeword/models/")
	id, sub := splitItemPath(rest)
	if id == "" {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "wake-word model id missing from path")
		return
	}
	model, ok := wakewordcatalog.ByID(id)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "unknown wake-word model id: "+id)
		return
	}
	switch sub {
	case "manifest.json":
		h.manifest(w, r, model)
	case "model.tflite":
		h.tflite(w, r, model)
	case "openwakeword":
		h.openWakeWord(w, r, model)
	default:
		httpx.WriteError(w, http.StatusNotFound, "not_found",
			"unknown sub-resource; expected manifest.json, model.tflite, or openwakeword")
	}
}

// ── responses ────────────────────────────────────────────────────────────────

type listEntry struct {
	ID               string      `json:"id"`
	WakeWord         string      `json:"wake_word"`
	DisplayName      string      `json:"display_name"`
	Description      string      `json:"description,omitempty"`
	TrainedLanguages []string    `json:"trained_languages"`
	License          string      `json:"license"`
	Formats          []string    `json:"formats"`
	ManifestURL      string      `json:"manifest_url,omitempty"`
	TFLiteURL        string      `json:"tflite_url,omitempty"`
	OpenWakeWord     *owwListing `json:"openwakeword,omitempty"`
}

type owwListing struct {
	PhraseURL            string  `json:"phrase_url"`
	MelspecURL           string  `json:"melspec_url"`
	EmbeddingURL         string  `json:"embedding_url"`
	RecommendedThreshold float64 `json:"recommended_threshold"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	base := h.baseURL(r)
	models := wakewordcatalog.All()
	entries := make([]listEntry, 0, len(models))
	for _, m := range models {
		e := listEntry{
			ID:               m.ID,
			WakeWord:         m.WakeWord,
			DisplayName:      m.DisplayName,
			Description:      m.Description,
			TrainedLanguages: m.TrainedLanguages,
			License:          wakewordcatalog.License,
			Formats:          []string{},
		}
		if m.HasOpenWakeWord() {
			e.Formats = append(e.Formats, "openwakeword")
			e.OpenWakeWord = &owwListing{
				PhraseURL:            m.OpenWakeWord.File.URL,
				MelspecURL:           wakewordcatalog.SharedMelspec.URL,
				EmbeddingURL:         wakewordcatalog.SharedEmbedding.URL,
				RecommendedThreshold: m.OpenWakeWord.RecommendedThreshold,
			}
		}
		if m.HasMicroWakeWord() {
			e.Formats = append(e.Formats, "microwakeword")
			e.ManifestURL = base + "/v1/wakeword/models/" + m.ID + "/manifest.json"
			e.TFLiteURL = base + "/v1/wakeword/models/" + m.ID + "/model.tflite"
		}
		entries = append(entries, e)
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"models": entries,
		"count":  len(entries),
	})
}

// microManifest is the ESPHome micro_wake_word v2 model manifest. Field names +
// nesting match esphome/micro-wake-word-models (e.g. okay_nabu.json) so an
// ESPHome `micro_wake_word:` block can reference the served URL unchanged.
type microManifest struct {
	Type             string         `json:"type"`
	WakeWord         string         `json:"wake_word"`
	Author           string         `json:"author"`
	Website          string         `json:"website"`
	Model            string         `json:"model"`
	TrainedLanguages []string       `json:"trained_languages"`
	Version          int            `json:"version"`
	Micro            microManifestP `json:"micro"`
}

type microManifestP struct {
	ProbabilityCutoff     float64 `json:"probability_cutoff"`
	SlidingWindowSize     int     `json:"sliding_window_size"`
	FeatureStepSize       int     `json:"feature_step_size"`
	TensorArenaSize       int     `json:"tensor_arena_size"`
	MinimumESPHomeVersion string  `json:"minimum_esphome_version"`
}

func (h *Handler) manifest(w http.ResponseWriter, r *http.Request, m wakewordcatalog.Model) {
	if !m.HasMicroWakeWord() {
		httpx.WriteError(w, http.StatusNotFound, "microwakeword_pending",
			"microWakeWord variant for '"+m.ID+"' is not published yet; use the openWakeWord artifacts or check back after training")
		return
	}
	writeJSON(w, r, http.StatusOK, microManifestFor(m, h.author, h.website))
}

// microManifestFor builds the ESPHome micro_wake_word v2 manifest for a model.
// Extracted from the handler so the exact JSON shape can be asserted in tests
// without a live registry entry or HTTP round-trip. The Model field stays
// RELATIVE ("model.tflite"): ESPHome resolves it with urljoin(manifest_url,
// model) → .../{id}/model.tflite (served by Handler.tflite), so no absolute
// base URL is required on the device.
func microManifestFor(m wakewordcatalog.Model, author, website string) microManifest {
	minVer := m.MicroWakeWord.Params.MinimumESPHomeVersion
	if strings.TrimSpace(minVer) == "" {
		minVer = wakewordcatalog.DefaultMinimumESPHomeVersion
	}
	stepSize := m.MicroWakeWord.Params.FeatureStepSize
	if stepSize == 0 {
		stepSize = wakewordcatalog.MicroWakeWordFeatureStepSize
	}
	return microManifest{
		Type:             wakewordcatalog.MicroWakeWordType,
		WakeWord:         m.WakeWord,
		Author:           author,
		Website:          website,
		Model:            "model.tflite",
		TrainedLanguages: m.TrainedLanguages,
		Version:          wakewordcatalog.MicroWakeWordManifestVersion,
		Micro: microManifestP{
			ProbabilityCutoff:     m.MicroWakeWord.Params.ProbabilityCutoff,
			SlidingWindowSize:     m.MicroWakeWord.Params.SlidingWindowSize,
			FeatureStepSize:       stepSize,
			TensorArenaSize:       m.MicroWakeWord.Params.TensorArenaSize,
			MinimumESPHomeVersion: minVer,
		},
	}
}

func (h *Handler) tflite(w http.ResponseWriter, r *http.Request, m wakewordcatalog.Model) {
	if !m.HasMicroWakeWord() {
		httpx.WriteError(w, http.StatusNotFound, "microwakeword_pending",
			"microWakeWord TFLite for '"+m.ID+"' is not published yet")
		return
	}
	// Redirect to the published object-store URL rather than proxying the
	// bytes: SpeechKit serves only the small JSON manifest locally; the CDN
	// streams the model. ESPHome + esp-tflite-micro follow the 302.
	http.Redirect(w, r, m.MicroWakeWord.File.URL, http.StatusFound)
}

type owwResponse struct {
	ID                   string   `json:"id"`
	WakeWord             string   `json:"wake_word"`
	RecommendedThreshold float64  `json:"recommended_threshold"`
	TrainedLanguages     []string `json:"trained_languages"`
	License              string   `json:"license"`
	Phrase               fileRef  `json:"phrase"`
	Melspec              fileRef  `json:"melspec"`
	Embedding            fileRef  `json:"embedding"`
}

type fileRef struct {
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

func (h *Handler) openWakeWord(w http.ResponseWriter, r *http.Request, m wakewordcatalog.Model) {
	if !m.HasOpenWakeWord() {
		httpx.WriteError(w, http.StatusNotFound, "openwakeword_missing",
			"no openWakeWord model published for '"+m.ID+"'")
		return
	}
	writeJSON(w, r, http.StatusOK, owwResponse{
		ID:                   m.ID,
		WakeWord:             m.WakeWord,
		RecommendedThreshold: m.OpenWakeWord.RecommendedThreshold,
		TrainedLanguages:     m.TrainedLanguages,
		License:              wakewordcatalog.License,
		Phrase:               fileRefOf(m.OpenWakeWord.File),
		Melspec:              fileRefOf(wakewordcatalog.SharedMelspec),
		Embedding:            fileRefOf(wakewordcatalog.SharedEmbedding),
	})
}

// ── helpers ────────────────────────────────────────────────────────────────

// guard enforces the enabled gate and the GET/HEAD method allow-list shared by
// every route. Returns false when it has already written a response.
func (h *Handler) guard(w http.ResponseWriter, r *http.Request) bool {
	if !h.enabled {
		httpx.WriteError(w, http.StatusServiceUnavailable, "wakeword_models_disabled",
			"[server.wakeword_models].enabled is false; ask the operator to enable it")
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"only GET and HEAD are accepted at this endpoint")
		return false
	}
	return true
}

func fileRefOf(f wakewordcatalog.FileArtifact) fileRef {
	return fileRef{URL: f.URL, SHA256: f.SHA256, SizeBytes: f.SizeBytes}
}

// baseURL returns the origin used to build absolute URLs. Prefers the
// configured PublicBaseURL; otherwise reconstructs from the request.
func (h *Handler) baseURL(r *http.Request) string {
	if h.publicBaseURL != "" {
		return h.publicBaseURL
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// splitItemPath separates "{id}" from "{id}/{sub}" in the trailing path.
func splitItemPath(rest string) (id, sub string) {
	if slash := strings.Index(rest, "/"); slash >= 0 {
		return rest[:slash], rest[slash+1:]
	}
	return rest, ""
}
