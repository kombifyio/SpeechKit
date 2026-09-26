//go:build linux

package assist

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func (h *Handler) ServeSelfTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"only POST is accepted on this endpoint")
		return
	}

	started := time.Now()
	result, err := h.processor.Process(r.Context(), speechkit.AssistRequest{
		Text:   "Reply with exactly the single word: pong.",
		Locale: "en",
	})
	latency := time.Since(started)
	if err != nil {
		slog.Warn("assist: self-test failed", "err", err)
		writePipelineError(w, err, latency)
		return
	}
	if strings.TrimSpace(result.Text) == "" {
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
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBytes)
	defer func() { _ = r.Body.Close() }()

	if err := r.ParseMultipartForm(32 << 10); err != nil {
		if errors.As(err, new(*http.MaxBytesError)) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
				fmt.Sprintf("request body exceeds maximum %d bytes", h.maxBytes))
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_multipart", "failed to parse multipart body: "+err.Error())
		return
	}

	locale := strings.TrimSpace(r.FormValue("locale"))
	selection := r.FormValue("selection")
	contextStr := r.FormValue("context")
	app := r.FormValue("app")
	windowTitle := r.FormValue("window_title")
	ttsOverride, ttsFormat, ttsVoice := parseTTSOverrides(r.FormValue("tts"), r.FormValue("tts_format"), r.FormValue("tts_voice"))
	speakerOpts := parseSpeakerOptionsFromForm(r)

	text := strings.TrimSpace(r.FormValue("text"))
	file, header, fileErr := r.FormFile("audio")

	switch {
	case text != "" && fileErr != nil:
		// Text-only path.
		h.processTranscript(r.Context(), w, text, nil, nil, processOptions{
			Locale:      h.resolveLocale(locale),
			Selection:   selection,
			Context:     contextStr,
			ActiveApp:   app,
			WindowTitle: windowTitle,
			SessionKey:  sessionKeyFromRequest(r),
		}, ttsOverride, ttsFormat, ttsVoice)
		return
	case fileErr == nil:
		defer func() { _ = file.Close() }()
		if header.Size > 0 && header.Size > h.maxBytes {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
				fmt.Sprintf("audio file %d bytes exceeds maximum %d bytes", header.Size, h.maxBytes))
			return
		}
		h.processAudio(r.Context(), w, file, header.Header.Get("Content-Type"), processOptions{
			// Deliberately NOT resolved here: on the audio path the locale
			// is handed to STT as a request-level override, so filling in
			// the server default would pin every transcription to it. The
			// default is applied after transcription instead, once the
			// provider has had its chance to report a language.
			Locale:      locale,
			Selection:   selection,
			Context:     contextStr,
			ActiveApp:   app,
			WindowTitle: windowTitle,
			SessionKey:  sessionKeyFromRequest(r),
		}, speakerOpts, ttsOverride, ttsFormat, ttsVoice)
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

	opts := processOptions{
		// Left unresolved on purpose. The audio branch hands this to STT as
		// a request-level override, where the server default would pin every
		// transcription; the text branch has no STT to learn from and
		// resolves it below.
		Locale:      body.Locale,
		Selection:   body.Selection,
		Context:     body.Context,
		ActiveApp:   body.App,
		WindowTitle: body.WindowTitle,
		SessionKey:  sessionKeyFromRequest(r),
	}

	text := strings.TrimSpace(body.Text)
	switch {
	case text != "":
		opts.Locale = h.resolveLocale(opts.Locale)
		h.processTranscript(r.Context(), w, text, nil, nil, opts, body.TTS, body.TTSFormat, body.TTSVoice)
	case strings.TrimSpace(body.AudioBase64) != "":
		raw, err := base64.StdEncoding.DecodeString(body.AudioBase64)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_base64",
				"audio_base64 is not valid base64: "+err.Error())
			return
		}
		formatHint := audioFormatHint(body.Format)
		h.processAudioBytes(r.Context(), w, raw, formatHint, opts, resolveSpeakerOptions(body.Speaker, body.SpeakerOptions), body.TTS, body.TTSFormat, body.TTSVoice)
	default:
		httpx.WriteError(w, http.StatusBadRequest, "missing_input",
			"JSON body must include either 'text' or 'audio_base64'")
	}
}
