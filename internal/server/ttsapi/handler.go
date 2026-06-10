//go:build linux

package ttsapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/tts"
)

type Handler struct {
	cfg    *config.Config
	router *tts.Router
}

func New(cfg *config.Config, router *tts.Router) *Handler {
	return &Handler{cfg: cfg, router: router}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/tts/synthesize", h.synthesize)
	mux.HandleFunc("/v1/tts/voices", h.voices)
}

type synthesizeRequest struct {
	Text   string  `json:"text"`
	Locale string  `json:"locale"`
	Voice  string  `json:"voice"`
	Speed  float64 `json:"speed"`
	Format string  `json:"format"`
}

func (h *Handler) synthesize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if h.router == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "tts_unavailable", "TTS router is not configured")
		return
	}
	var body synthesizeRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Text == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_text", "text is required")
		return
	}
	result, err := h.router.Synthesize(r.Context(), body.Text, tts.SynthesizeOpts{
		Locale: strings.TrimSpace(body.Locale),
		Voice:  strings.TrimSpace(body.Voice),
		Speed:  body.Speed,
		Format: strings.TrimSpace(body.Format),
	})
	if err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "tts_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"audio_base64": base64.StdEncoding.EncodeToString(result.Audio),
		"format":       result.Format,
		"sample_rate":  result.SampleRate,
		"duration_ms":  result.Duration.Milliseconds(),
		"provider":     result.Provider,
		"voice":        result.Voice,
	})
}

func (h *Handler) voices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	voices := make([]map[string]any, 0)
	if h.cfg != nil && h.cfg.TTS.OpenAI.Enabled {
		voices = append(voices, map[string]any{
			"provider":  "openai",
			"id":        firstNonEmpty(h.cfg.TTS.OpenAI.Voice, h.cfg.Providers.OpenAI.TTSVoice, "nova"),
			"locale":    "auto",
			"default":   true,
			"discovery": "configured",
		})
	}
	if h.cfg != nil && h.cfg.TTS.Google.Enabled {
		voices = append(voices, map[string]any{
			"provider":  "google",
			"id":        firstNonEmpty(h.cfg.TTS.Google.Voice, "en-US-Neural2-J"),
			"locale":    "auto",
			"default":   len(voices) == 0,
			"discovery": "configured",
		})
	}
	if h.cfg != nil && h.cfg.TTS.HuggingFace.Enabled {
		voices = append(voices, map[string]any{
			"provider":  "huggingface",
			"id":        firstNonEmpty(h.cfg.TTS.HuggingFace.Model, "Qwen/Qwen3-TTS-12Hz-1.7B-Base"),
			"locale":    "auto",
			"default":   len(voices) == 0,
			"discovery": "configured",
		})
	}
	if h.cfg != nil && h.cfg.TTS.Local.Enabled {
		voices = append(voices, map[string]any{
			"provider":  "local",
			"id":        firstNonEmpty(h.cfg.TTS.Local.Model, "hexgrad/Kokoro-82M"),
			"locale":    "auto",
			"default":   len(voices) == 0,
			"discovery": "configured",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"voices": voices})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer func() { _ = r.Body.Close() }()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(dst); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func methodNotAllowed(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on this resource")
}
