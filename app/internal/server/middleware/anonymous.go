//go:build linux

package middleware

import (
	"net/http"
	"strings"
)

// IsAnonymousEdge identifies the restricted identity minted by Gateway for
// its public guide. It is never an account or a service principal.
func IsAnonymousEdge(id Identity) bool {
	return id.Source == "edge_hmac" && id.Role == "anonymous" && id.Plan == "anonymous" && strings.HasPrefix(id.UserID, "anonymous:")
}

func anonymousRouteAllowed(r *http.Request, id Identity) bool {
	path := strings.TrimPrefix(r.URL.Path, "/api")
	public := path == "/v1/anonymous/dictation/transcribe" || path == "/v1/anonymous/voiceagent/sessions"
	claimsAnonymous := id.Role == "anonymous" || id.Plan == "anonymous" || strings.HasPrefix(id.UserID, "anonymous:")
	if public {
		return r.Method == http.MethodPost && IsAnonymousEdge(id)
	}
	return !claimsAnonymous
}
