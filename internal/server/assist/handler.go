//go:build linux

// Package assist implements the POST /v1/assist/process handler. It accepts
// either an audio payload (→ STT → Assist pipeline) or a text transcript
// directly, runs the Framework's Assist Pipeline, and returns the result
// plus optional TTS audio as base64 in the JSON response.
//
// Host-side tool execution (clipboard, selection, quick-note) is NOT done
// server-side — the server returns an `action: "execute"` signal and the
// calling client performs the action. This keeps the Server-Target safe
// for multi-tenant deployments where host-level side effects would be
// nonsensical.
package assist

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

	assistpkg "github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/server/audio"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

// Transcriber is the STT surface the handler needs when the caller sends
// audio. If nil, only text-only requests are accepted.
type Transcriber interface {
	Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error)
}

// Processor is the Assist-pipeline surface the handler needs. The production
// implementation is `internal/assist.Pipeline`; tests supply a fake.
type Processor interface {
	Process(ctx context.Context, transcript string, opts assistpkg.ProcessOpts) (*assistpkg.Result, error)
}

// Options configures a single Handler instance.
type Options struct {
	Processor     Processor
	Transcriber   Transcriber // optional; nil disables audio input
	MaxUploadMB   int
	DefaultLocale string
}

// Handler implements the /v1/assist/process HTTP surface.
type Handler struct {
	processor     Processor
	transcriber   Transcriber
	maxBytes      int64
	defaultLocale string
}

// New constructs a Handler. processor must be non-nil. Transcriber is
// optional — when omitted, the handler rejects requests that carry audio
// with a 400/missing-transcriber code.
func New(opts Options) (*Handler, error) {
	if opts.Processor == nil {
		return nil, errors.New("assist: processor must not be nil")
	}
	maxBytes := int64(25) << 20
	if opts.MaxUploadMB > 0 {
		maxBytes = int64(opts.MaxUploadMB) << 20
	}
	return &Handler{
		processor:     opts.Processor,
		transcriber:   opts.Transcriber,
		maxBytes:      maxBytes,
		defaultLocale: strings.TrimSpace(opts.DefaultLocale),
	}, nil
}

// Mount registers the handler at /v1/assist/process.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.Handle("/v1/assist/process", h)
	mux.HandleFunc("/v1/assist/self-test", h.ServeSelfTest)
}

// ── request / response shapes ───────────────────────────────────────────────

type processJSONRequest struct {
	Text        string `json:"text"`
	AudioBase64 string `json:"audio_base64"`
	Format      string `json:"format"`
	Locale      string `json:"locale"`
	Selection   string `json:"selection"`
	Context     string `json:"context"`
	// TTS overrides — the Pipeline already knows whether TTS is globally
	// enabled; these fields let the caller opt out per-request.
	TTS       *bool   `json:"tts,omitempty"`
	TTSFormat string  `json:"tts_format,omitempty"`
	TTSVoice  string  `json:"tts_voice,omitempty"`
	TTSSpeed  float64 `json:"tts_speed,omitempty"`
}

type processResponse struct {
	Text        string      `json:"text"`
	SpeakText   string      `json:"speak_text,omitempty"`
	Action      string      `json:"action"`
	Locale      string      `json:"locale,omitempty"`
	Shortcut    string      `json:"shortcut,omitempty"`
	Surface     string      `json:"surface,omitempty"`
	Kind        string      `json:"kind,omitempty"`
	Transcript  string      `json:"transcript,omitempty"`
	AudioBase64 string      `json:"audio_base64,omitempty"`
	AudioFormat string      `json:"audio_format,omitempty"`
	LatencyMs   int64       `json:"latency_ms"`
	SourceInfo  *sourceMeta `json:"source,omitempty"`
}

type selfTestResponse struct {
	Status    string         `json:"status"`
	Text      string         `json:"text,omitempty"`
	Action    string         `json:"action,omitempty"`
	Locale    string         `json:"locale,omitempty"`
	LatencyMs int64          `json:"latency_ms"`
	Details   map[string]any `json:"details,omitempty"`
}

type sourceMeta struct {
	Format     string `json:"format"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	DurationMs int64  `json:"duration_ms"`
}

func (h *Handler) ServeSelfTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"only POST is accepted on this endpoint")
		return
	}

	started := time.Now()
	result, err := h.processor.Process(r.Context(), "Reply with exactly the single word: pong.", assistpkg.ProcessOpts{Locale: "en"})
	latency := time.Since(started)
	if err != nil {
		slog.Warn("assist: self-test failed", "err", err)
		writePipelineError(w, err, latency)
		return
	}
	if result == nil || strings.TrimSpace(result.Text) == "" {
		httpx.WriteErrorWithDetails(w, http.StatusServiceUnavailable, "pipeline_unavailable", "Assist self-test returned an empty result", map[string]any{
			"stage":      "llm",
			"category":   "empty_result",
			"retryable":  true,
			"latency_ms": latency.Milliseconds(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(selfTestResponse{
		Status:    "ok",
		Text:      result.Text,
		Action:    result.Action,
		Locale:    result.Locale,
		LatencyMs: latency.Milliseconds(),
	})
}

// ── ServeHTTP ───────────────────────────────────────────────────────────────

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
	if err := r.ParseMultipartForm(32 << 10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_multipart",
			"failed to parse multipart body: "+err.Error())
		return
	}

	locale := strings.TrimSpace(r.FormValue("locale"))
	selection := r.FormValue("selection")
	contextStr := r.FormValue("context")
	ttsOverride, ttsFormat, ttsVoice := parseTTSOverrides(r.FormValue("tts"), r.FormValue("tts_format"), r.FormValue("tts_voice"))

	text := strings.TrimSpace(r.FormValue("text"))
	file, header, fileErr := r.FormFile("audio")

	switch {
	case text != "" && fileErr != nil:
		// Text-only path.
		h.processTranscript(w, r.Context(), text, nil, assistpkg.ProcessOpts{
			Locale:    h.resolveLocale(locale),
			Selection: selection,
			Context:   contextStr,
		}, ttsOverride, ttsFormat, ttsVoice)
		return
	case fileErr == nil:
		defer func() { _ = file.Close() }()
		if header.Size > 0 && header.Size > h.maxBytes {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
				fmt.Sprintf("audio file %d bytes exceeds maximum %d bytes", header.Size, h.maxBytes))
			return
		}
		h.processAudio(w, r.Context(), file, header.Header.Get("Content-Type"), assistpkg.ProcessOpts{
			Locale:    h.resolveLocale(locale),
			Selection: selection,
			Context:   contextStr,
		}, ttsOverride, ttsFormat, ttsVoice)
		return
	default:
		httpx.WriteError(w, http.StatusBadRequest, "missing_input",
			"multipart body must include either an 'audio' file part or a 'text' form field")
	}
}

func (h *Handler) handleJSON(w http.ResponseWriter, r *http.Request) {
	bodyReader := http.MaxBytesReader(w, r.Body, h.maxBytes)
	defer func() { _ = r.Body.Close() }()

	var body processJSONRequest
	if err := json.NewDecoder(bodyReader).Decode(&body); err != nil {
		if errors.As(err, new(*http.MaxBytesError)) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
				fmt.Sprintf("request body exceeds maximum %d bytes", h.maxBytes))
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json",
			"failed to decode request JSON: "+err.Error())
		return
	}

	opts := assistpkg.ProcessOpts{
		Locale:    h.resolveLocale(body.Locale),
		Selection: body.Selection,
		Context:   body.Context,
	}

	text := strings.TrimSpace(body.Text)
	switch {
	case text != "":
		h.processTranscript(w, r.Context(), text, nil, opts, body.TTS, body.TTSFormat, body.TTSVoice)
	case strings.TrimSpace(body.AudioBase64) != "":
		raw, err := base64.StdEncoding.DecodeString(body.AudioBase64)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_base64",
				"audio_base64 is not valid base64: "+err.Error())
			return
		}
		formatHint := audioFormatHint(body.Format)
		h.processAudioBytes(w, r.Context(), raw, formatHint, opts, body.TTS, body.TTSFormat, body.TTSVoice)
	default:
		httpx.WriteError(w, http.StatusBadRequest, "missing_input",
			"JSON body must include either 'text' or 'audio_base64'")
	}
}

// ── core flow ───────────────────────────────────────────────────────────────

func (h *Handler) processAudio(w http.ResponseWriter, ctx context.Context, reader io.Reader, contentType string, opts assistpkg.ProcessOpts, ttsOverride *bool, ttsFormat, ttsVoice string) {
	lr := io.LimitReader(reader, h.maxBytes+1)
	raw, err := io.ReadAll(lr)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "read_failed", "failed to read audio: "+err.Error())
		return
	}
	if int64(len(raw)) > h.maxBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("audio exceeds maximum %d bytes", h.maxBytes))
		return
	}
	h.processAudioBytes(w, ctx, raw, contentType, opts, ttsOverride, ttsFormat, ttsVoice)
}

func (h *Handler) processAudioBytes(w http.ResponseWriter, ctx context.Context, raw []byte, contentType string, opts assistpkg.ProcessOpts, ttsOverride *bool, ttsFormat, ttsVoice string) {
	if h.transcriber == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "stt_unavailable",
			"this deployment does not expose an STT router; send 'text' instead of 'audio'")
		return
	}

	decoded, err := audio.Decode(raw, contentType)
	if err != nil {
		if errors.Is(err, audio.ErrUnsupportedFormat) {
			httpx.WriteError(w, http.StatusUnsupportedMediaType, "unsupported_audio_format", err.Error())
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_audio", "failed to decode audio: "+err.Error())
		return
	}
	if len(decoded.PCM) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "empty_audio", "decoded audio contains no samples")
		return
	}

	durationSecs := float64(decoded.DurationMs) / 1000.0
	sttResult, err := h.transcriber.Route(ctx, decoded.PCM, durationSecs, stt.TranscribeOpts{Language: opts.Locale})
	if err != nil {
		slog.Warn("assist: STT failed", "err", err, "duration_ms", decoded.DurationMs)
		httpx.WriteError(w, http.StatusServiceUnavailable, "stt_provider_unavailable",
			"no STT provider could satisfy the request: "+err.Error())
		return
	}
	if sttResult == nil || strings.TrimSpace(sttResult.Text) == "" {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "empty_transcript",
			"STT returned an empty transcript; the audio may contain no speech")
		return
	}

	if opts.Locale == "" && sttResult.Language != "" {
		opts.Locale = sttResult.Language
	}
	source := &sourceMeta{
		Format:     decoded.SourceFormat,
		SampleRate: decoded.SourceRate,
		Channels:   decoded.SourceCh,
		DurationMs: decoded.DurationMs,
	}
	h.processTranscript(w, ctx, sttResult.Text, source, opts, ttsOverride, ttsFormat, ttsVoice)
}

func (h *Handler) processTranscript(w http.ResponseWriter, ctx context.Context, transcript string, source *sourceMeta, opts assistpkg.ProcessOpts, ttsOverride *bool, ttsFormat, ttsVoice string) {
	if strings.TrimSpace(transcript) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "empty_transcript", "transcript or text field must not be empty")
		return
	}

	started := time.Now()
	result, err := h.processor.Process(ctx, transcript, opts)
	latency := time.Since(started)
	if err != nil {
		slog.Warn("assist: pipeline error", "err", err, "transcript_chars", len(transcript))
		writePipelineError(w, err, latency)
		return
	}
	if result == nil {
		httpx.WriteError(w, http.StatusInternalServerError, "empty_result",
			"Assist pipeline returned nil result")
		return
	}
	if strings.TrimSpace(result.Text) == "" {
		httpx.WriteErrorWithDetails(w, http.StatusServiceUnavailable, "pipeline_unavailable", "Assist pipeline returned an empty result", map[string]any{
			"stage":      "llm",
			"category":   "empty_result",
			"retryable":  true,
			"latency_ms": latency.Milliseconds(),
		})
		return
	}

	// Respect TTS opt-out from the request. We cannot opt IN when the
	// pipeline was built without a TTS router — Audio will already be empty
	// in that case.
	if ttsOverride != nil && !*ttsOverride {
		result.Audio = nil
		result.Format = ""
	}
	_ = ttsFormat // reserved for post-synthesis transcoding (v1.1)
	_ = ttsVoice  // reserved for per-request voice override (v1.1)

	resp := processResponse{
		Text:        result.Text,
		SpeakText:   result.SpeakText,
		Action:      result.Action,
		Locale:      firstNonEmptyString(result.Locale, opts.Locale),
		Shortcut:    result.Shortcut,
		Surface:     string(result.Surface),
		Kind:        string(result.Kind),
		Transcript:  transcript,
		LatencyMs:   latency.Milliseconds(),
		SourceInfo:  source,
		AudioFormat: result.Format,
	}
	if len(result.Audio) > 0 {
		resp.AudioBase64 = base64.StdEncoding.EncodeToString(result.Audio)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// ── helpers ─────────────────────────────────────────────────────────────────

func (h *Handler) resolveLocale(requested string) string {
	if trimmed := strings.TrimSpace(requested); trimmed != "" {
		return trimmed
	}
	return h.defaultLocale
}

func audioFormatHint(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "wav":
		return "audio/wav"
	case "mp3":
		return "audio/mpeg"
	case "pcm16", "pcm":
		return "audio/L16;rate=16000;channels=1"
	default:
		return ""
	}
}

// parseTTSOverrides interprets multipart form fields. "tts" is parsed as a
// permissive boolean; other fields pass through verbatim.
func parseTTSOverrides(ttsRaw, ttsFormat, ttsVoice string) (override *bool, format, voice string) {
	if trimmed := strings.TrimSpace(ttsRaw); trimmed != "" {
		switch strings.ToLower(trimmed) {
		case "1", "true", "yes", "on":
			t := true
			override = &t
		case "0", "false", "no", "off":
			f := false
			override = &f
		}
	}
	return override, strings.TrimSpace(ttsFormat), strings.TrimSpace(ttsVoice)
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func writePipelineError(w http.ResponseWriter, err error, latency time.Duration) {
	classification := classifyPipelineError(err)
	classification["latency_ms"] = latency.Milliseconds()
	message := "Assist pipeline failed"
	if stage, _ := classification["stage"].(string); stage != "" {
		message += " during " + stage
	}
	if category, _ := classification["category"].(string); category != "" {
		message += ": " + category
	}
	httpx.WriteErrorWithDetails(w, http.StatusServiceUnavailable, "pipeline_unavailable", message, classification)
}

func classifyPipelineError(err error) map[string]any {
	details := map[string]any{
		"stage":     "pipeline",
		"category":  "runtime_error",
		"retryable": true,
	}
	if err == nil {
		return details
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "llm failed") || strings.Contains(lower, "no llm flow") || strings.Contains(lower, "no models configured") {
		details["stage"] = "llm"
	}
	if strings.Contains(msg, "Invalid configuration type") || strings.Contains(msg, "GenerateContentConfig") || strings.Contains(msg, "GenerationCommonConfig") {
		details["stage"] = "llm"
		details["category"] = "provider_config"
		details["retryable"] = false
		details["provider"] = "googleai"
		return details
	}
	if strings.Contains(lower, "no llm flow") || strings.Contains(lower, "no models configured") {
		details["category"] = "missing_model"
		details["retryable"] = false
		return details
	}
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "429") {
		details["category"] = "rate_limited"
		return details
	}
	if strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden") || strings.Contains(lower, "401") || strings.Contains(lower, "403") {
		details["category"] = "credentials"
		details["retryable"] = false
		return details
	}
	return details
}
