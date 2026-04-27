//go:build linux

package middleware

import (
	"bufio"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Logging emits a structured slog record per request including method, path,
// status, bytes written, and wall-clock duration. Kept dependency-free so the
// adapter stays transportable to any Go runtime that ships slog.
func Logging() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			slog.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
				"user_agent", r.Header.Get("User-Agent"),
			)
		})
	}
}

// responseRecorder captures status + byte counters without interfering with
// the underlying writer's optional interfaces (Hijacker, Flusher). We expose
// those explicitly so WebSocket upgrades work through the chain.
type responseRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (r *responseRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		// Implicit 200 per net/http contract.
		r.wroteHeader = true
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// errHijackUnsupported is returned when the wrapped writer does not implement
// http.Hijacker (e.g. HTTP/2 responses).
var errHijackUnsupported = errors.New("middleware: ResponseWriter does not support hijacking")

// Hijack is required for WebSocket upgrades. Delegates to the underlying
// writer's http.Hijacker implementation when present.
func (r *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errHijackUnsupported
	}
	return hijacker.Hijack()
}

// Flush forwards to the underlying writer when supported (SSE, chunked
// encoding).
func (r *responseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
