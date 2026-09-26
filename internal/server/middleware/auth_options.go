//go:build linux

package middleware

import (
	"net/http"
)

// AuthMode selects which credential format the server accepts.
type AuthMode string

const (
	// AuthModeNone disables built-in server authentication. The request still
	// receives a stable anonymous identity so mode handlers can apply session
	// ownership and rate-limit logic without requiring an upstream auth layer.
	AuthModeNone AuthMode = "none"
	// AuthModeBearer requires a static bearer token from the configured env
	// var. Minimum viable auth; suitable for same-network service-to-service
	// calls (e.g. an upstream service calling SpeechKit over a private network).
	AuthModeBearer AuthMode = "bearer"
	// AuthModeEdgeHMAC trusts HMAC-signed headers from a known edge
	// (Cloudflare Worker / reverse proxy). The actual user identity comes
	// from the edge. Expected header set:
	//   X-Edge-Auth-Hmac, X-Edge-User-Id, X-Edge-Org-Id, X-Edge-Plan,
	//   and optional X-Edge-Role. Role is covered by the HMAC when present.
	AuthModeEdgeHMAC AuthMode = "edge_hmac"
	// AuthModeBearerOrEdge accepts either credential format; handy when a
	// single deployment serves both internal services (bearer) and
	// browser-originated traffic (edge-signed).
	AuthModeBearerOrEdge AuthMode = "bearer_or_edge"
	// AuthModeOIDC validates a Bearer JWT issued by an external identity
	// provider (Azure AD, Okta, Google Workspace, Auth0, ...) against a
	// configured JWKS endpoint. The caller identity — UserID, OrgID, Role —
	// is sourced from the token's claims, giving self-hosted deployments real
	// multi-tenancy without writing an edge-HMAC proxy. See oidc.go.
	AuthModeOIDC AuthMode = "oidc"
	// AuthModeBearerOrOIDC accepts either the static service bearer token or
	// an IdP-issued JWT on the same deployment. This is the mobile/native
	// onboarding shape: existing service callers (box, CI, smoke) keep the
	// static bearer while per-user clients present OIDC JWTs — no separate
	// deployment or edge proxy required. The static bearer is compared
	// first (constant-time, cheap); JWT validation runs only when it
	// doesn't match.
	AuthModeBearerOrOIDC AuthMode = "bearer_or_oidc"
)

// AuthOptions configures the Auth middleware.
type AuthOptions struct {
	Mode              string
	BearerTokenEnv    string
	EdgeSecretEnv     string
	BearerRole        string
	AllowPublicPaths  []string // exact path matches that skip auth entirely (e.g. /healthz)
	AllowPublicRoutes []PublicRoute
	// HTMLUnauthorizedPaths/Routes keep browser-facing admin UI failures out
	// of the JSON API envelope while still requiring normal credentials.
	HTMLUnauthorizedPaths  []string
	HTMLUnauthorizedRoutes []PublicRoute
	// Dynamic providers are evaluated for every request. They let first-run
	// setup generate a token without rebuilding the middleware chain.
	ModeProvider              func() string
	BearerTokenProvider       func() string
	EdgeSecretProvider        func() string
	BearerRoleProvider        func() string
	AdminUsernameProvider     func() string
	AdminPasswordHashProvider func() string
	// SmokeTokenProvider returns the optional public demo token used by the
	// smoke UI on `/`. When non-empty and matching the presented Bearer,
	// the middleware attaches a Source="smoke", Plan="demo" identity so
	// handlers and the rate-limiter can treat demo traffic accordingly.
	SmokeTokenProvider func() string
	// Bootstrap routes are public only while BootstrapAllowed returns true.
	// The server uses this for the first settings write when bearer auth is
	// configured but no bearer token exists yet.
	AllowBootstrapPaths  []string
	AllowBootstrapRoutes []PublicRoute
	BootstrapAllowed     func(*http.Request) bool
	// RequireAuthenticatedMode is defence-in-depth on top of
	// config.ValidateServerProductionAuth. When true, the AuthModeNone
	// branch of verify() refuses to issue the anonymous Identity even if
	// the resolved mode is "none" or empty. Bootstrap sets this for
	// non-loopback binds so a future code path that skips startup
	// validation cannot accidentally serve unauthenticated traffic to the
	// public internet. Admin-session and smoke-token fallbacks remain
	// available — only the implicit anonymous identity is suppressed.
	RequireAuthenticatedMode bool
	TrustedProxyCIDRs        []string
	// OIDCVerifier validates a request's Bearer JWT and maps its claims to an
	// Identity. Set by bootstrap only when AuthMode is "oidc"; nil otherwise.
	OIDCVerifier func(*http.Request) (Identity, bool)
	// OboSubjectTokenHeader overrides the header name carrying the
	// edge-forwarded OBO subject token (see EdgeOboSubjectTokenHeader).
	// Empty uses the default. The header is only honoured on identities
	// whose Source is "edge_hmac"; its value is never logged.
	OboSubjectTokenHeader string
}

type PublicRoute struct {
	Path       string
	PathPrefix string
	PathSuffix string
	Methods    []string
}
