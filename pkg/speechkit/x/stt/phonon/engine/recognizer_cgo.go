//go:build cgo && (amd64 || arm64)

package engine

/*
#cgo CFLAGS: -O3 -ffast-math -std=c11 -D_GNU_SOURCE
#cgo amd64 CFLAGS: -march=x86-64-v3
#cgo arm64 CFLAGS: -march=armv8.2-a+dotprod
#cgo !windows LDFLAGS: -lm -pthread
#cgo windows LDFLAGS: -lm -Wl,-Bstatic -lwinpthread -Wl,-Bdynamic

#include <stdio.h>
#include <stdlib.h>
#include "phonon2.h"

// The engine reads its thread count from P2_THREADS when its pool starts. It is
// set through the C runtime here because on Windows Go's os.Setenv does not
// reach the C runtime's copy of the environment.
static void sk_set_threads(int n) {
	char buf[16];
	snprintf(buf, sizeof buf, "%d", n);
#ifdef _WIN32
	_putenv_s("P2_THREADS", buf);
#else
	setenv("P2_THREADS", buf, 1);
#endif
}
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"unsafe"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/phonon"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/sherpa"
)

// threadsOnce sets the engine's process-wide thread count once: its pool
// starts with the first model and lives until the process exits.
var threadsOnce sync.Once

// Recognizer is a loaded model. Its methods are safe for concurrent use;
// decodes run one recording at a time.
type Recognizer struct {
	mu     sync.Mutex
	engine *C.P2Engine
	// weights is the engine's weight image in C memory; the engine reads its
	// tensors from it for its whole life.
	weights unsafe.Pointer
	// streams are the open streams, closed before the engine they run on.
	streams map[*Stream]struct{}
}

// NewRecognizer loads the model in cfg.ModelDir: it checks Fermion's container
// against its manifest, builds the engine's weight image from it (a few
// seconds, 255 MB) and opens the engine on it.
func NewRecognizer(cfg phonon.Config) (*Recognizer, error) {
	if !phonon.CPUSupported() {
		return nil, ErrUnsupportedCPU
	}
	// cgo forbids C from keeping a Go pointer, so the weight image is built
	// straight into C memory; the manifest and tokens are parsed during the
	// open call.
	var weights unsafe.Pointer
	image, err := phonon.LoadInto(cfg.ModelDir, func(n int) []byte {
		weights = C.malloc(C.size_t(n))
		if weights == nil {
			return nil
		}
		return unsafe.Slice((*byte)(weights), n)
	})
	if err != nil {
		C.free(weights)
		return nil, err
	}
	// Give back the unused tail of the buffer, then the converted container.
	binLen := len(image.Bin)
	if shrunk := C.realloc(weights, C.size_t(binLen)); shrunk != nil {
		weights = shrunk
	}
	image.Bin = nil
	debug.FreeOSMemory()
	threadsOnce.Do(func() {
		C.sk_set_threads(C.int(max(cfg.Threads, 1)))
		// The encoder's per-block progress line would land in the sidecar's
		// log with every decode, drafts included.
		C.p2_verbose = 0
	})

	engine := C.p2_engine_open_mem(weights, C.size_t(binLen),
		(*C.char)(unsafe.Pointer(&image.Manifest[0])), C.size_t(len(image.Manifest)),
		(*C.char)(unsafe.Pointer(&image.Tokens[0])), C.size_t(len(image.Tokens)))
	runtime.KeepAlive(image)
	if engine == nil {
		C.free(weights)
		return nil, errors.New("phonon: the engine refused the model's weights")
	}
	return &Recognizer{engine: engine, weights: weights}, nil
}

// Transcribe decodes 16 kHz mono samples. Audio over 35 seconds is cut into
// pause-aligned windows by the engine, each decoded on its own.
func (r *Recognizer) Transcribe(samples []float32) (sherpa.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.engine == nil {
		return sherpa.Result{}, errors.New("phonon: recognizer is closed")
	}
	result := sherpa.Result{Duration: float64(len(samples)) / sherpa.SampleRate}
	if len(samples) == 0 {
		return result, nil
	}
	var out C.P2Result
	rc := C.p2_transcribe(r.engine, (*C.float)(unsafe.Pointer(&samples[0])), C.int(len(samples)), C.int(sherpa.SampleRate), &out)
	if rc != 0 {
		return sherpa.Result{}, fmt.Errorf("phonon: the engine could not decode the audio (code %d)", int(rc))
	}
	defer C.p2_result_free(&out)

	fillResult(&result, &out)
	return result, nil
}

// fillResult copies the engine's words, segments and text into result.
func fillResult(result *sherpa.Result, out *C.P2Result) {
	for _, w := range unsafe.Slice(out.words, int(out.n_words)) {
		result.Words = append(result.Words, sherpa.Word{Text: C.GoString(w.text), Start: float64(w.start), End: float64(w.end)})
	}
	for _, s := range unsafe.Slice(out.segments, int(out.n_segments)) {
		result.Segments = append(result.Segments, segment(&s))
	}
	result.Text = strings.TrimSpace(C.GoString(out.text))
}

func segment(s *C.P2Segment) sherpa.Segment {
	return sherpa.Segment{Start: float64(s.start), End: float64(s.end), Text: strings.TrimSpace(C.GoString(s.text))}
}

// Stream is one recording decoded while it arrives, at the engine's window
// granularity: a segment completes whenever 35 s have gathered since the last
// cut (cut at the best pause from 25 s on), and Partial decodes the rest for a
// provisional transcript. Fed a whole recording, it gives the same transcript
// as [Recognizer.Transcribe]. Decodes share the recognizer's lock with
// Transcribe.
type Stream struct {
	r *Recognizer
	s *C.P2Stream
}

// OpenStream starts a stream of mono samples at sampleRate, which the engine
// resamples to 16 kHz as they arrive.
func (r *Recognizer) OpenStream(sampleRate int) (sherpa.Stream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.engine == nil {
		return nil, errors.New("phonon: recognizer is closed")
	}
	s := C.p2_stream_open(r.engine, C.int(sampleRate))
	if s == nil {
		return nil, errors.New("phonon: the engine could not open a stream")
	}
	st := &Stream{r: r, s: s}
	if r.streams == nil {
		r.streams = make(map[*Stream]struct{})
	}
	r.streams[st] = struct{}{}
	return st, nil
}

// next collects the segments the engine completed since the last call.
func (st *Stream) next() []sherpa.Segment {
	var out []sherpa.Segment
	for seg := C.p2_stream_next(st.s); seg != nil; seg = C.p2_stream_next(st.s) {
		out = append(out, segment(seg))
	}
	return out
}

// Feed appends samples and returns the segments that completed with them.
func (st *Stream) Feed(samples []float32) ([]sherpa.Segment, error) {
	st.r.mu.Lock()
	defer st.r.mu.Unlock()
	if st.s == nil {
		return nil, errors.New("phonon: stream is closed")
	}
	if len(samples) == 0 {
		return nil, nil
	}
	if C.p2_stream_feed(st.s, (*C.float)(unsafe.Pointer(&samples[0])), C.int(len(samples))) < 0 {
		return nil, errors.New("phonon: the engine could not decode the stream")
	}
	return st.next(), nil
}

// Partial decodes the audio since the last cut for a provisional transcript.
func (st *Stream) Partial() (string, error) {
	st.r.mu.Lock()
	defer st.r.mu.Unlock()
	if st.s == nil {
		return "", errors.New("phonon: stream is closed")
	}
	text := C.p2_stream_partial(st.s)
	if text == nil {
		return "", errors.New("phonon: the engine could not decode the stream")
	}
	defer C.free(unsafe.Pointer(text))
	return strings.TrimSpace(C.GoString(text)), nil
}

// Finish decodes the rest and returns the segments that added and the
// transcript of the whole recording.
func (st *Stream) Finish() ([]sherpa.Segment, sherpa.Result, error) {
	st.r.mu.Lock()
	defer st.r.mu.Unlock()
	if st.s == nil {
		return nil, sherpa.Result{}, errors.New("phonon: stream is closed")
	}
	if C.p2_stream_finish(st.s) < 0 {
		return nil, sherpa.Result{}, errors.New("phonon: the engine could not decode the stream")
	}
	segments := st.next()
	out := C.p2_stream_result(st.s)
	result := sherpa.Result{Duration: float64(out.duration_seconds)}
	fillResult(&result, out)
	return segments, result, nil
}

// Close releases the stream.
func (st *Stream) Close() {
	st.r.mu.Lock()
	defer st.r.mu.Unlock()
	st.closeLocked()
}

func (st *Stream) closeLocked() {
	if st.s != nil {
		C.p2_stream_close(st.s)
		st.s = nil
		delete(st.r.streams, st)
	}
}

// Close releases the model. Transcribe fails afterwards.
func (r *Recognizer) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.engine != nil {
		for st := range r.streams {
			st.closeLocked()
		}
		C.p2_engine_close(r.engine)
		C.free(r.weights)
		r.engine, r.weights = nil, nil
	}
}
