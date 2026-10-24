package sherpa

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/internal/logutil"
)

// Transcriber is what [NewHandler] serves; engine.Recognizer implements it.
type Transcriber interface {
	Transcribe(samples []float32) (Result, error)
}

// ServerOptions configures [NewHandler].
type ServerOptions struct {
	// InferencePath is the transcription route, as whisper-server's
	// --inference-path; empty means "/inference". "/inference" is always
	// served as well.
	InferencePath string
	// PublicDir, when set, is served at the root like whisper-server's
	// --public directory: GET /<name> returns a regular file directly inside
	// it. SpeechKit's local provider proves the identity of its child this
	// way.
	PublicDir string
	// Languages restricts the request language; a pinned language outside it
	// is refused. Empty accepts every language.
	Languages []string
	// ModelName names the model in errors and logs.
	ModelName string
	// MaxUploadBytes bounds a request body; 0 means 128 MiB (about 70
	// minutes of 16 kHz mono PCM).
	MaxUploadBytes int64
	// MaxInFlight bounds the transcription requests held at once (one
	// decodes, the rest wait with their audio in memory); a request beyond
	// it gets 503 with Retry-After. 0 means 8.
	MaxInFlight int
	// Logger receives request diagnostics; transcripts and audio are never
	// logged. Nil uses slog.Default().
	Logger *slog.Logger
}

const (
	defaultMaxUploadBytes = 128 << 20
	defaultMaxInFlight    = 8
)

// publicFileName admits plain file names and no dotfiles.
var publicFileName = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]*$`)

// NewHandler serves t over whisper-server's HTTP contract: GET /health,
// GET / and public-directory files, and a multipart POST with a "file" (a
// 16-bit PCM WAV), optional "language" and "response_format" json (default),
// text or verbose_json. Other fields, such as whisper's "prompt", are
// ignored: Parakeet's greedy decoder takes no text conditioning.
func NewHandler(t Transcriber, opts ServerOptions) http.Handler {
	inference := strings.TrimSpace(opts.InferencePath)
	if inference == "" {
		inference = "/inference"
	}
	if opts.MaxUploadBytes <= 0 {
		opts.MaxUploadBytes = defaultMaxUploadBytes
	}
	if opts.MaxInFlight <= 0 {
		opts.MaxInFlight = defaultMaxInFlight
	}
	h := &handler{t: t, opts: opts, inference: inference, slots: make(chan struct{}, opts.MaxInFlight)}
	return h
}

type handler struct {
	t         Transcriber
	opts      ServerOptions
	inference string
	slots     chan struct{}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == h.inference || r.URL.Path == "/inference":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, "use POST")
			return
		}
		select {
		case h.slots <- struct{}{}:
			defer func() { <-h.slots }()
			h.transcribe(w, r)
		default:
			w.Header().Set("Retry-After", "5")
			writeError(w, http.StatusServiceUnavailable, "busy: too many transcriptions in progress")
		}
	case r.Method != http.MethodGet && r.Method != http.MethodHead:
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "use GET")
	case r.URL.Path == "/health":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case r.URL.Path == "/":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "speechkit-stt-sherpa\n")
	default:
		h.servePublic(w, r)
	}
}

func (h *handler) servePublic(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if h.opts.PublicDir == "" || !publicFileName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(filepath.Join(h.opts.PublicDir, name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close() //nolint:errcheck // read-only
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	// ServeContent, not ServeFile: ServeFile redirects ".../index.html".
	http.ServeContent(w, r, name, info.ModTime(), file)
}

type verboseSegment struct {
	ID    int     `json:"id"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

func (h *handler) transcribe(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.opts.MaxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("audio exceeds %d bytes", h.opts.MaxUploadBytes))
			return
		}
		writeError(w, http.StatusBadRequest, "expected a multipart/form-data body: "+err.Error())
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck // temp-file cleanup is best effort

	format := strings.ToLower(strings.TrimSpace(r.FormValue("response_format")))
	switch format {
	case "", "json", "text", "verbose_json":
	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("response_format %q is not supported; use json, text or verbose_json", format))
		return
	}
	language := r.FormValue("language")
	if !SupportsLanguage(h.opts.Languages, language) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("language %q is not supported by %s; it transcribes %s",
			language, h.modelName(), strings.Join(h.opts.Languages, ", ")))
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing audio in the \"file\" field")
		return
	}
	raw, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil {
		writeError(w, http.StatusBadRequest, "read audio: "+err.Error())
		return
	}
	samples, err := DecodeWAV(raw)
	if err != nil {
		writeError(w, http.StatusUnsupportedMediaType, "audio must be a 16-bit PCM WAV: "+err.Error())
		return
	}
	// A client that gave up while uploading or waiting for a slot gets no
	// decode spent on it.
	if r.Context().Err() != nil {
		return
	}

	start := time.Now()
	result, err := h.t.Transcribe(samples)
	if err != nil {
		h.log().Error("transcription failed", "model", h.modelName(), "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.log().Info("transcribed", "model", h.modelName(), "audio_seconds", result.Duration,
		"segments", len(result.Segments), "decode_ms", time.Since(start).Milliseconds())

	switch format {
	case "text":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, result.Text+"\n")
	case "verbose_json":
		segments := make([]verboseSegment, len(result.Segments))
		for i, s := range result.Segments {
			segments[i] = verboseSegment{ID: i, Start: s.Start, End: s.End, Text: s.Text}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"task":     "transcribe",
			"language": NormalizeLanguage(language),
			"duration": result.Duration,
			"text":     result.Text,
			"segments": segments,
		})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"text": result.Text})
	}
}

func (h *handler) modelName() string {
	if h.opts.ModelName != "" {
		return h.opts.ModelName
	}
	return "this model"
}

func (h *handler) log() *slog.Logger { return logutil.Resolve(h.opts.Logger) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError answers in whisper-server's error shape.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
