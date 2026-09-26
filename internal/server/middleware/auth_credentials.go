//go:build linux

package middleware

import (
	"crypto/hmac"
	"net/http"
	"strings"
)

func verify(mode AuthMode, r *http.Request, bearerToken, edgeSecret, bearerRole string, requireAuth bool) (Identity, bool) {
	switch mode {
	case AuthModeNone:
		// Fail-closed defence-in-depth: when the operator bound the server
		// to a non-loopback address, refuse to issue the implicit
		// anonymous Identity even if cfg validation was somehow skipped.
		// Smoke-token and admin-session fallbacks already ran above and
		// returned their own Identities when applicable.
		if requireAuth {
			return Identity{}, false
		}
		return Identity{
			UserID: "anonymous",
			OrgID:  "public",
			Plan:   "public",
			Source: "none",
		}, true
	case AuthModeBearer:
		return verifyBearer(r, bearerToken, bearerRole)
	case AuthModeEdgeHMAC:
		return verifyEdgeHMAC(r, edgeSecret)
	case AuthModeBearerOrEdge:
		if id, ok := verifyBearer(r, bearerToken, bearerRole); ok {
			return id, true
		}
		return verifyEdgeHMAC(r, edgeSecret)
	default:
		return Identity{}, false
	}
}

// verifySmoke accepts a Bearer header matching the public smoke token.
// Unlike verifyBearer, smoke identities are explicitly low-trust:
//
//   - Source = "smoke" (distinguishable in audit logs)
//   - Plan   = "demo"  (downstream rate-limiters/quota gates can throttle)
//   - Role   = ""      (never admin)
//
// Returns (zero, false) when smoke auth is disabled (expected == "") or
// the token doesn't match. Constant-time compare to avoid timing leaks.
func verifySmoke(r *http.Request, expected string) (Identity, bool) {
	if expected == "" {
		return Identity{}, false
	}
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return Identity{}, false
	}
	presented := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if presented == "" {
		return Identity{}, false
	}
	if !hmacEqual([]byte(presented), []byte(expected)) {
		return Identity{}, false
	}
	return Identity{
		UserID: "smoke",
		OrgID:  "public",
		Plan:   "demo",
		Source: "smoke",
	}, true
}

func verifyBearer(r *http.Request, expected, role string) (Identity, bool) {
	if expected == "" {
		// Fail closed: an unset server token must never accept requests.
		return Identity{}, false
	}
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return Identity{}, false
	}
	presented := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if presented == "" {
		return Identity{}, false
	}
	// Constant-time compare to avoid token timing leaks.
	if !hmacEqual([]byte(presented), []byte(expected)) {
		return Identity{}, false
	}
	return Identity{
		UserID: "service",
		OrgID:  "default",
		Plan:   "internal",
		Role:   strings.TrimSpace(role),
		Source: "bearer",
	}, true
}

func hmacEqual(a, b []byte) bool {
	return hmac.Equal(a, b)
}
