//go:build linux

// Package dictation implements the POST /v1/dictation/transcribe handler.
// It is a thin adapter around the Framework's STT router: accept audio bytes
// and metadata, normalize to canonical PCM via internal/server/audio, and
// delegate to the router. The handler has no knowledge of which provider
// serves the request; that's the router's job.
package dictation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/audio"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

// Transcriber is the minimal surface the handler needs from an STT router.
// The production implementation is `internal/router.Router`; tests provide
// a fake.
type Transcriber interface {
	Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error)
}

// Options configures a single handler instance.
type Options struct {
	Router        Transcriber
	MaxUploadMB   int    // request body ceiling; 0 disables the limit (discouraged)
	DefaultPrompt string // applied when the request does not provide a prompt
}

// Handler implements the dictation HTTP surface.
type Handler struct {
	router        Transcriber
	maxBytes      int64
	defaultPrompt string
}

// New constructs a Handler. The router must be non-nil; a zero maxBytes
// defaults to 25 MB to match the documented API contract.
func New(opts Options) (*Handler, error) {
	if opts.Router == nil {
		return nil, errors.New("dictation: router must not be nil")
	}
	maxBytes := int64(25) << 20
	if opts.MaxUploadMB > 0 {
		maxBytes = int64(opts.MaxUploadMB) << 20
	}
	return &Handler{
		router:        opts.Router,
		maxBytes:      maxBytes,
		defaultPrompt: strings.TrimSpace(opts.DefaultPrompt),
	}, nil
}

// Mount registers the handler on the given mux at /v1/dictation/transcribe.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.Handle("/v1/dictation/transcribe", h)
}

// response body shape — kept stable across versions so kombify-AI and future
// OSS consumers can pin to this contract.
type transcribeResponse struct {
	Text       string      `json:"text"`
	Language   string      `json:"language,omitempty"`
	DurationMs int64       `json:"duration_ms"`
	LatencyMs  int64       `json:"latency_ms"`
	Provider   string      `json:"provider,omitempty"`
	Model      string      `json:"model,omitempty"`
	Confidence float64     `json:"confidence,omitempty"`
	SourceInfo *sourceMeta `json:"source,omitempty"`
}

type sourceMeta struct {
	Format     string `json:"format"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	DurationMs int64  `json:"duration_ms"`
}

type transcribeJSONRequest struct {
	AudioBase64 string `json:"audio_base64"`
	Format      string `json:"format"`   // "wav" | "mp3" | "pcm16"
	Language    string `json:"language"` // "de" | "en" | "auto"
	Model       string `json:"model"`
	Prompt      string `json:"prompt"`
}

// ServeHTTP routes the request by Content-Type.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"only POST is accepted on this endpoint")
		return
	}

	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		h.handleMultipart(w, r)
	case strings.HasPrefix(ct, "application/json"):
		h.handleJSON(w, r)
	default:
		httpx.WriteError(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Content-Type must be multipart/form-data or application/json")
	}
}

func (h *Handler) handleMultipart(w http.ResponseWriter, r *http.Request) {
	// Bound memory: up to 32 KB in-memory before spilling to tmp.
	if err := r.ParseMultipartForm(32 << 10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_multipart",
			"failed to parse multipart body: "+err.Error())
		return
	}

	file, header, err := r.FormFile("audio")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "missing_audio",
			"multipart body must include an 'audio' file part")
		return
	}
	defer func() { _ = file.Close() }()

	if header.Size > 0 && header.Size > h.maxBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("audio file %d bytes exceeds maximum %d bytes", header.Size, h.maxBytes))
		return
	}

	// The part's Content-Type is more reliable than the outer request's.
	partCT := header.Header.Get("Content-Type")

	opts := stt.TranscribeOpts{
		Language: strings.TrimSpace(r.FormValue("language")),
		Model:    strings.TrimSpace(r.FormValue("model")),
		Prompt:   strings.TrimSpace(r.FormValue("prompt")),
	}
	opts.Prompt = h.resolvePrompt(opts.Prompt)
	h.transcribeAndReply(w, r.Context(), file, partCT, opts)
}

func (h *Handler) handleJSON(w http.ResponseWriter, r *http.Request) {
	bodyReader := http.MaxBytesReader(w, r.Body, h.maxBytes)
	defer func() { _ = r.Body.Close() }()

	var body transcribeJSONRequest
	if err := json.NewDecoder(bodyReader).Decode(&body); err != nil {
		// MaxBytesError sets a specific status on the writer; translate to
		// our error envelope.
		if errors.As(err, new(*http.MaxBytesError)) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
				fmt.Sprintf("request body exceeds maximum %d bytes", h.maxBytes))
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json",
			"failed to decode request JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.AudioBase64) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_audio",
			"JSON body must include 'audio_base64'")
		return
	}

	raw, err := base64.StdEncoding.DecodeString(body.AudioBase64)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_base64",
			"audio_base64 is not valid base64: "+err.Error())
		return
	}

	// Build a Content-Type hint from the explicit format field.
	formatHint := ""
	switch strings.ToLower(strings.TrimSpace(body.Format)) {
	case "wav":
		formatHint = "audio/wav"
	case "mp3":
		formatHint = "audio/mpeg"
	case "pcm16", "pcm":
		formatHint = "audio/L16;rate=16000;channels=1"
	}

	opts := stt.TranscribeOpts{
		Language: strings.TrimSpace(body.Language),
		Model:    strings.TrimSpace(body.Model),
		Prompt:   strings.TrimSpace(body.Prompt),
	}
	opts.Prompt = h.resolvePrompt(opts.Prompt)

	h.transcribeBytes(w, r.Context(), raw, formatHint, opts)
}

func (h *Handler) resolvePrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt != "" {
		return prompt
	}
	return h.defaultPrompt
}

// transcribeAndReply buffers the reader, decodes, and hands off.
func (h *Handler) transcribeAndReply(w http.ResponseWriter, ctx context.Context, reader io.Reader, contentType string, opts stt.TranscribeOpts) {
	lr := io.LimitReader(reader, h.maxBytes+1)
	raw, err := io.ReadAll(lr)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "read_failed",
			"failed to read audio: "+err.Error())
		return
	}
	if int64(len(raw)) > h.maxBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("audio exceeds maximum %d bytes", h.maxBytes))
		return
	}
	h.transcribeBytes(w, ctx, raw, contentType, opts)
}

func (h *Handler) transcribeBytes(w http.ResponseWriter, ctx context.Context, raw []byte, contentType string, opts stt.TranscribeOpts) {
	decoded, err := audio.Decode(raw, contentType)
	if err != nil {
		if errors.Is(err, audio.ErrUnsupportedFormat) {
			httpx.WriteError(w, http.StatusUnsupportedMediaType, "unsupported_audio_format",
				err.Error())
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_audio",
			"failed to decode audio: "+err.Error())
		return
	}
	if len(decoded.PCM) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "empty_audio",
			"decoded audio contains no samples")
		return
	}

	started := time.Now()
	durationSecs := float64(decoded.DurationMs) / 1000.0
	result, err := h.router.Route(ctx, decoded.PCM, durationSecs, opts)
	latency := time.Since(started)

	if err != nil {
		// Router errors map to 503 (provider unavailable) in v1 since we
		// don't yet distinguish transient vs permanent. Revisit in v1.1.
		slog.Warn("dictation: router error",
			"err", err,
			"duration_ms", decoded.DurationMs,
			"format", decoded.SourceFormat,
		)
		httpx.WriteError(w, http.StatusServiceUnavailable, "provider_unavailable",
			"no STT provider could satisfy the request: "+err.Error())
		return
	}
	if result == nil {
		httpx.WriteError(w, http.StatusInternalServerError, "empty_result",
			"STT router returned nil result")
		return
	}

	resp := transcribeResponse{
		Text:       result.Text,
		Language:   firstNonEmpty(result.Language, opts.Language),
		DurationMs: decoded.DurationMs,
		LatencyMs:  latency.Milliseconds(),
		Provider:   result.Provider,
		Model:      result.Model,
		Confidence: result.Confidence,
		SourceInfo: &sourceMeta{
			Format:     decoded.SourceFormat,
			SampleRate: decoded.SourceRate,
			Channels:   decoded.SourceCh,
			DurationMs: decoded.DurationMs,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
