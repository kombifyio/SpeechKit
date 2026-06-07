//go:build linux

package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

// RequestIDHeader is the request/response header carrying the correlation id.
const RequestIDHeader = "X-Request-Id"

type requestIDKey struct{}

// RequestID attaches a correlation id to every request: it reuses a safe
// inbound X-Request-Id (so an edge/proxy can propagate one) or generates a
// fresh one, stores it in the context, and echoes it on the response. The
// logging middleware and any tracing read it from the context.
//
// Place it early in the chain (after Recover, before Logging) so every
// log line and span carries the id.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := strings.TrimSpace(r.Header.Get(RequestIDHeader))
			if !validRequestID(id) {
				id = newRequestID()
			}
			w.Header().Set(RequestIDHeader, id)
			ctx := context.WithValue(r.Context(), requestIDKey{}, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestIDFromContext returns the request id attached by RequestID, or "".
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// validRequestID accepts a bounded, log-safe inbound id: 1..128 chars of
// [A-Za-z0-9._-]. Anything else (empty, oversized, control chars, header
// injection attempts) is replaced with a generated id.
func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}
