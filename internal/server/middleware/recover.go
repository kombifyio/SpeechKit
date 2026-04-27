//go:build linux

package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recover turns panics in downstream handlers into 500 responses with a JSON
// error envelope, logging the stack trace for post-mortem debugging. Without
// this, a single buggy handler crashes the whole process.
func Recover() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				slog.Error("handler panic",
					"err", rec,
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"code":    "internal",
						"message": "internal server error",
					},
				})
			}()
			next.ServeHTTP(w, r)
		})
	}
}
