package sherpa

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/internal/logutil"
)

// maxTextRunes bounds one synthesis request. Piper voices are utterance
// models; a paragraph is already long, and a book must not pin the sidecar.
const maxTextRunes = 4000

// Speaker synthesizes one utterance. engine.Engine implements it.
type Speaker interface {
	Speak(text, locale string, speed float32) (samples []float32, sampleRate int, voiceID string, err error)
}

// ServerOptions configures [NewHandler].
type ServerOptions struct {
	// SpeechPath is the synthesis route; empty means "/v1/audio/speech".
	SpeechPath string
	// Logger receives request diagnostics. Text and audio are never logged.
	Logger *slog.Logger
}

// NewHandler serves s: GET /health and a JSON POST that returns a WAV body.
func NewHandler(s Speaker, opts ServerOptions) http.Handler {
	path := strings.TrimSpace(opts.SpeechPath)
	if path == "" {
		path = "/v1/audio/speech"
	}
	return &handler{s: s, opts: opts, path: path}
}

type handler struct {
	s    Speaker
	opts ServerOptions
	path string
}

type speechRequest struct {
	Text   string  `json:"text"`
	Locale string  `json:"locale"`
	Voice  string  `json:"voice"`
	Speed  float64 `json:"speed"`
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == h.path:
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, "use POST")
			return
		}
		h.speak(w, r)
	case r.Method != http.MethodGet && r.Method != http.MethodHead:
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "use GET")
	case r.URL.Path == "/health":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case r.URL.Path == "/":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "speechkit-tts-sherpa\n")
	default:
		http.NotFound(w, r)
	}
}

func (h *handler) speak(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req speechRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "expected a JSON body with text: "+err.Error())
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if len([]rune(text)) > maxTextRunes {
		writeError(w, http.StatusBadRequest, "text is too long")
		return
	}
	locale := strings.TrimSpace(req.Locale)
	if locale == "" {
		locale = strings.TrimSpace(req.Voice)
	}
	speed := float32(req.Speed)
	if r.Context().Err() != nil {
		return
	}
	start := time.Now()
	samples, rate, voiceID, err := h.s.Speak(text, locale, speed)
	if err != nil {
		h.log().Error("synthesis failed", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	wav := EncodeWAV(samples, rate)
	h.log().Info("synthesized", "voice", voiceID, "samples", len(samples), "sample_rate", rate, "synth_ms", time.Since(start).Milliseconds())
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("X-SpeechKit-Voice", voiceID)
	w.Header().Set("X-SpeechKit-Sample-Rate", itoa(rate))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(wav)
}

func (h *handler) log() *slog.Logger { return logutil.Resolve(h.opts.Logger) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
