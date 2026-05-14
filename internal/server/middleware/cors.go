//go:build linux

package middleware

import (
	"net/http"
	"strings"
)

// CORS returns a middleware that handles preflight and attaches CORS response
// headers when the request Origin matches one of the allowed entries.
//
// Empty allowedOrigins disables CORS entirely (safe default; internal
// service-to-service calls never trigger a browser preflight anyway).
// Special-case "*" opens CORS to all origins — use with caution and only for
// OSS dev mode, never when the server is behind auth that trusts cookies.
func CORS(allowedOrigins []string) Middleware {
	allowList := make(map[string]struct{}, len(allowedOrigins))
	wildcard := false
	for _, o := range allowedOrigins {
		trimmed := strings.TrimSpace(o)
		if trimmed == "" {
			continue
		}
		if trimmed == "*" {
			wildcard = true
			continue
		}
		allowList[trimmed] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (wildcard || originAllowed(origin, allowList)) {
				if wildcard {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-SpeechKit-Setup-Token, X-Edge-Auth-Version, X-Edge-Auth-Hmac, X-Edge-Auth-Timestamp, X-Edge-Auth-Nonce, X-Edge-User-Id, X-Edge-Org-Id, X-Edge-Plan, X-Edge-Role")
				w.Header().Set("Access-Control-Max-Age", "600")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func originAllowed(origin string, allow map[string]struct{}) bool {
	_, ok := allow[origin]
	return ok
}
