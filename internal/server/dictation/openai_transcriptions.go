//go:build linux

package dictation

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// OpenAITranscriptionsPath is the OpenAI-compatible transcription route. It
// lets OpenAI-audio clients (Open WebUI with AUDIO_STT_ENGINE=openai, the
// OpenAI SDKs) use SpeechKit as their STT base URL. It runs the same kernel
// path as /v1/dictation/transcribe; only the request and response shapes
// follow the OpenAI audio API.
const OpenAITranscriptionsPath = "/v1/audio/transcriptions"

// MountOpenAICompat registers the OpenAI-compatible transcription route.
func (h *Handler) MountOpenAICompat(mux *http.ServeMux) {
	mux.HandleFunc(OpenAITranscriptionsPath, h.serveOpenAITranscription)
}

// openAITranscriptionJSON is the JSON request variant Open WebUI sends when
// AUDIO_STT_OPENAI_API_REQUEST_FORMAT=json.
type openAITranscriptionJSON struct {
	Model          string `json:"model"`
	Language       string `json:"language"`
	Prompt         string `json:"prompt"`
	ResponseFormat string `json:"response_format"`
	InputAudio     struct {
		Data   string `json:"data"`
		Format string `json:"format"`
	} `json:"input_audio"`
}

type openAITranscriptionRequest struct {
	raw            []byte
	contentType    string
	model          string
	language       string
	prompt         string
	responseFormat string
}

func (h *Handler) serveOpenAITranscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"only POST is accepted on this endpoint")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBytes)
	defer func() { _ = r.Body.Close() }()

	var (
		req     *openAITranscriptionRequest
		failure *transcribeFailure
	)
	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		req, failure = h.readOpenAIMultipart(r)
	case strings.HasPrefix(ct, "application/json"):
		req, failure = h.readOpenAIJSON(r)
	default:
		failure = &transcribeFailure{http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Content-Type must be multipart/form-data or application/json"}
	}
	if failure != nil {
		httpx.WriteError(w, failure.status, failure.code, failure.message)
		return
	}

	format := strings.ToLower(req.responseFormat)
	switch format {
	case "", "json", "text", "verbose_json":
	default:
		httpx.WriteError(w, http.StatusBadRequest, "unsupported_response_format",
			fmt.Sprintf("response_format %q is not supported; use json, text or verbose_json", req.responseFormat))
		return
	}

	profileRef, err := h.resolveProviderProfile(r.Context(), openAIModelProfile(req.model))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_provider_profile", err.Error())
		return
	}
	opts := stt.TranscribeOpts{
		Language:          req.language,
		Prompt:            h.resolvePrompt(req.prompt),
		ProviderProfileID: profileRef,
	}
	resp, failure := h.transcribe(r, req.raw, req.contentType, opts)
	text, language, durationMs := "", opts.Language, int64(0)
	switch {
	case failure == nil:
		text, language, durationMs = resp.Text, resp.Language, resp.DurationMs
	case failure.code == "empty_transcript":
		// OpenAI returns an empty transcript for silence rather than an
		// error; OpenAI clients treat a non-200 as a failed request.
	default:
		httpx.WriteError(w, failure.status, failure.code, failure.message)
		return
	}

	switch format {
	case "text":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, text) // #nosec G705 -- transcript served as nosniff text/plain, never rendered as HTML.
	case "verbose_json":
		writeOpenAIJSON(w, map[string]any{
			"task":     "transcribe",
			"language": language,
			"duration": float64(durationMs) / 1000.0,
			"text":     text,
			"segments": []any{},
		})
	default:
		writeOpenAIJSON(w, map[string]any{"text": text})
	}
}

func (h *Handler) readOpenAIMultipart(r *http.Request) (*openAITranscriptionRequest, *transcribeFailure) {
	// #nosec G120 -- serveOpenAITranscription wraps the body in http.MaxBytesReader(maxBytes).
	if err := r.ParseMultipartForm(32 << 10); err != nil {
		return nil, bodyReadFailure(err, h.maxBytes, "invalid_multipart", "failed to parse multipart body: ")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, &transcribeFailure{http.StatusBadRequest, "missing_audio",
			"multipart body must include a 'file' part"}
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, h.maxBytes+1))
	if err != nil {
		return nil, &transcribeFailure{http.StatusBadRequest, "read_failed", "failed to read audio: " + err.Error()}
	}
	if int64(len(raw)) > h.maxBytes {
		return nil, &transcribeFailure{http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("audio exceeds maximum %d bytes", h.maxBytes)}
	}
	return &openAITranscriptionRequest{
		raw:            raw,
		contentType:    openAIAudioContentType(header.Header.Get("Content-Type"), filepath.Ext(header.Filename)),
		model:          strings.TrimSpace(r.FormValue("model")),
		language:       strings.TrimSpace(r.FormValue("language")),
		prompt:         strings.TrimSpace(r.FormValue("prompt")),
		responseFormat: strings.TrimSpace(r.FormValue("response_format")),
	}, nil
}

func (h *Handler) readOpenAIJSON(r *http.Request) (*openAITranscriptionRequest, *transcribeFailure) {
	var body openAITranscriptionJSON
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, bodyReadFailure(err, h.maxBytes, "invalid_json", "failed to decode request JSON: ")
	}
	if strings.TrimSpace(body.InputAudio.Data) == "" {
		return nil, &transcribeFailure{http.StatusBadRequest, "missing_audio",
			"JSON body must include 'input_audio.data'"}
	}
	raw, err := base64.StdEncoding.DecodeString(body.InputAudio.Data)
	if err != nil {
		return nil, &transcribeFailure{http.StatusBadRequest, "invalid_base64",
			"input_audio.data is not valid base64: " + err.Error()}
	}
	return &openAITranscriptionRequest{
		raw:            raw,
		contentType:    openAIAudioContentType("", "."+strings.TrimSpace(body.InputAudio.Format)),
		model:          strings.TrimSpace(body.Model),
		language:       strings.TrimSpace(body.Language),
		prompt:         strings.TrimSpace(body.Prompt),
		responseFormat: strings.TrimSpace(body.ResponseFormat),
	}, nil
}

func bodyReadFailure(err error, maxBytes int64, code, prefix string) *transcribeFailure {
	if errors.As(err, new(*http.MaxBytesError)) {
		return &transcribeFailure{http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("request body exceeds maximum %d bytes", maxBytes)}
	}
	return &transcribeFailure{http.StatusBadRequest, code, prefix + err.Error()}
}

// openAIModelProfile maps the OpenAI `model` field onto a SpeechKit Dictation
// provider profile. OpenAI model names such as "whisper-1" (or the empty
// model Open WebUI sends by default) carry no SpeechKit meaning, so only an
// exact Dictation profile ID pins a provider; anything else leaves the
// server's configured routing in charge.
func openAIModelProfile(model string) string {
	normalized := speechkit.NormalizeProviderProfileID(strings.TrimSpace(model))
	if normalized != "" && dictationProfileExists(normalized) {
		return normalized
	}
	return ""
}

// openAIAudioContentType prefers a recognizable part Content-Type and falls
// back to the file extension. Open WebUI streams the upload without a part
// Content-Type (application/octet-stream) but keeps the original extension.
// An unknown result leaves the decoder to sniff magic bytes.
func openAIAudioContentType(partContentType, ext string) string {
	if mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(partContentType)); err == nil &&
		strings.HasPrefix(strings.ToLower(mediaType), "audio/") {
		return partContentType
	}
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), ".")) {
	case "wav", "wave":
		return "audio/wav"
	case "mp3", "mpeg", "mpga":
		return "audio/mpeg"
	case "webm":
		return "audio/webm"
	case "ogg", "oga", "opus":
		return "audio/ogg"
	default:
		return ""
	}
}

func writeOpenAIJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}
