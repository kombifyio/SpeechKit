package sherpa

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// StreamTranscriber is a [Transcriber] that can also decode a recording while
// it arrives. [NewHandler] serves the stream routes only for one.
type StreamTranscriber interface {
	Transcriber
	OpenStream(sampleRate int) (Stream, error)
}

// Stream is one recording decoded while it arrives. Its methods are called
// from one goroutine at a time.
type Stream interface {
	// Feed appends mono samples at the stream's rate and returns the segments
	// the engine completed with them.
	Feed(samples []float32) ([]Segment, error)
	// Partial is a provisional transcript of the audio not yet in a segment.
	Partial() (string, error)
	// Finish decodes what remains and returns the segments that added and the
	// transcript of the whole recording. The stream takes no audio after it.
	Finish() ([]Segment, Result, error)
	Close()
}

const (
	// maxStreams bounds the open streams; a dictation holds one.
	maxStreams = 4
	// streamIdleTimeout closes a stream its client stopped calling.
	streamIdleTimeout = 2 * time.Minute
	// maxStreamFeedBytes bounds one audio post: about two minutes of 16 kHz
	// mono PCM, far above what a client sends between two calls.
	maxStreamFeedBytes = 4 << 20
	maxStreamRate      = 192000
)

var streamPath = regexp.MustCompile(`^/stream/([0-9a-f]{32})(/audio|/finish)?$`)

// streams holds the open streams of one handler.
type streams struct {
	mu   sync.Mutex
	open map[string]*openStream
	// opening counts the slots taken by streams being opened.
	opening int
}

type openStream struct {
	mu       sync.Mutex
	s        Stream
	closed   bool
	lastUsed time.Time
}

type streamSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

func streamSegments(in []Segment) []streamSegment {
	out := make([]streamSegment, len(in))
	for i, s := range in {
		out[i] = streamSegment{Start: s.Start, End: s.End, Text: s.Text}
	}
	return out
}

// serveStream answers the stream routes:
//
//   - POST /stream?rate=16000&language=de opens a stream: {"id": "..."};
//   - POST /stream/<id>/audio with 16-bit little-endian mono PCM appends it:
//     {"segments": [...]} plus "partial" with ?partial=1;
//   - POST /stream/<id>/finish ends it: {"segments": [...], "text": ...,
//     "duration": ..., "words": [...]};
//   - DELETE /stream/<id> drops it.
func (h *handler) serveStream(w http.ResponseWriter, r *http.Request, st StreamTranscriber) {
	if r.URL.Path == "/stream" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, "use POST")
			return
		}
		h.openStream(w, r, st)
		return
	}
	m := streamPath.FindStringSubmatch(r.URL.Path)
	if m == nil {
		http.NotFound(w, r)
		return
	}
	id, action := m[1], m[2]
	switch {
	case action == "" && r.Method == http.MethodDelete:
		h.dropStream(id)
		w.WriteHeader(http.StatusNoContent)
		return
	case action != "" && r.Method == http.MethodPost:
	default:
		writeError(w, http.StatusMethodNotAllowed, "use POST, or DELETE on the stream")
		return
	}
	// The audio is read before the stream is taken, so a slow upload holds
	// no lock.
	var samples []float32
	if action == "/audio" {
		r.Body = http.MaxBytesReader(w, r.Body, maxStreamFeedBytes)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("audio exceeds %d bytes per call", maxStreamFeedBytes))
				return
			}
			writeError(w, http.StatusBadRequest, "read audio: "+err.Error())
			return
		}
		if len(raw)%2 != 0 {
			writeError(w, http.StatusBadRequest, "audio must be 16-bit PCM: odd byte count")
			return
		}
		samples = make([]float32, len(raw)/2)
		for i := range samples {
			samples[i] = float32(int16(binary.LittleEndian.Uint16(raw[2*i:]))) / 32768
		}
	}
	held := h.lookupStream(id)
	if held == nil {
		writeError(w, http.StatusNotFound, "no such stream")
		return
	}
	held.mu.Lock()
	defer held.mu.Unlock()
	if held.closed {
		writeError(w, http.StatusNotFound, "no such stream")
		return
	}
	held.lastUsed = time.Now()
	if action == "/finish" {
		segments, result, err := held.s.Finish()
		h.dropStream(id)
		if err != nil {
			h.log().Error("stream finish failed", "model", h.modelName(), "error", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		words := make([]verboseWord, len(result.Words))
		for i, word := range result.Words {
			words[i] = verboseWord{Word: word.Text, Start: word.Start, End: word.End}
		}
		h.log().Info("stream finished", "model", h.modelName(), "audio_seconds", result.Duration, "segments", len(result.Segments))
		writeJSON(w, http.StatusOK, map[string]any{
			"segments": streamSegments(segments),
			"text":     result.Text,
			"duration": result.Duration,
			"words":    words,
		})
		return
	}

	var segments []Segment
	if len(samples) > 0 {
		var err error
		if segments, err = held.s.Feed(samples); err != nil {
			h.log().Error("stream feed failed", "model", h.modelName(), "error", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	answer := map[string]any{"segments": streamSegments(segments)}
	if r.URL.Query().Get("partial") == "1" {
		partial, err := held.s.Partial()
		if err != nil {
			h.log().Error("stream partial failed", "model", h.modelName(), "error", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		answer["partial"] = partial
	}
	writeJSON(w, http.StatusOK, answer)
}

func (h *handler) openStream(w http.ResponseWriter, r *http.Request, st StreamTranscriber) {
	query := r.URL.Query()
	language := query.Get("language")
	if !SupportsLanguage(h.opts.Languages, language) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("language %q is not supported by %s; it transcribes %s",
			language, h.modelName(), strings.Join(h.opts.Languages, ", ")))
		return
	}
	rate := SampleRate
	if raw := query.Get("rate"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 8000 || n > maxStreamRate {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("rate %q is not a sample rate between 8000 and %d", raw, maxStreamRate))
			return
		}
		rate = n
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		writeError(w, http.StatusInternalServerError, "stream id: "+err.Error())
		return
	}
	id := hex.EncodeToString(b[:])

	// The slot is taken under the lock and the stream opened outside it: the
	// engine may be busy decoding, and the other streams' calls must not wait
	// for that.
	h.streams.mu.Lock()
	h.expireStreamsLocked()
	if len(h.streams.open)+h.streams.opening >= maxStreams {
		h.streams.mu.Unlock()
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, "busy: too many open streams")
		return
	}
	h.streams.opening++
	h.streams.mu.Unlock()

	s, err := st.OpenStream(rate)
	h.streams.mu.Lock()
	h.streams.opening--
	if err == nil {
		if h.streams.open == nil {
			h.streams.open = make(map[string]*openStream)
		}
		h.streams.open[id] = &openStream{s: s, lastUsed: time.Now()}
	}
	h.streams.mu.Unlock()
	if err != nil {
		h.log().Error("stream open failed", "model", h.modelName(), "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (h *handler) lookupStream(id string) *openStream {
	h.streams.mu.Lock()
	defer h.streams.mu.Unlock()
	return h.streams.open[id]
}

// dropStream forgets the stream and closes it once no call uses it.
func (h *handler) dropStream(id string) {
	h.streams.mu.Lock()
	held := h.streams.open[id]
	delete(h.streams.open, id)
	h.streams.mu.Unlock()
	if held != nil {
		go held.close()
	}
}

func (held *openStream) close() {
	held.mu.Lock()
	defer held.mu.Unlock()
	if !held.closed {
		held.closed = true
		held.s.Close()
	}
}

// expireStreamsLocked closes the streams nobody called for streamIdleTimeout,
// so a client that vanished does not hold a slot. The caller holds
// h.streams.mu.
func (h *handler) expireStreamsLocked() {
	for id, held := range h.streams.open {
		if !held.mu.TryLock() {
			continue // in use
		}
		idle := time.Since(held.lastUsed) > streamIdleTimeout
		held.mu.Unlock()
		if idle {
			delete(h.streams.open, id)
			go held.close()
		}
	}
}
