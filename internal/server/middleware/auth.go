//go:build linux

package middleware

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
)

// Auth validates credentials according to the configured mode and attaches the
// resolved Identity to the request context. Unauthenticated requests receive
// 401 with a JSON error envelope.
func Auth(opts AuthOptions) Middleware {
	runtime := newAuthRuntime(opts)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			runtime.serveHTTP(w, r, next)
		})
	}
}

type authRuntime struct {
	opts                      AuthOptions
	modeProvider              func() string
	bearerTokenProvider       func() string
	edgeSecretProvider        func() string
	bearerRoleProvider        func() string
	adminUsernameProvider     func() string
	adminPasswordHashProvider func() string
	smokeTokenProvider        func() string
	oidcVerifier              func(*http.Request) (Identity, bool)
	publicSet                 map[string]struct{}
	publicRoutes              []PublicRoute
	bootstrapSet              map[string]struct{}
	bootstrapRoutes           []PublicRoute
	htmlUnauthorizedSet       map[string]struct{}
	htmlUnauthorizedRoutes    []PublicRoute
	trustedProxies            httpx.TrustedProxies
}

func newAuthRuntime(opts AuthOptions) authRuntime {
	modeProvider := opts.ModeProvider
	if modeProvider == nil {
		modeProvider = func() string { return opts.Mode }
	}
	bearerTokenProvider := opts.BearerTokenProvider
	if bearerTokenProvider == nil {
		envName := strings.TrimSpace(opts.BearerTokenEnv)
		bearerTokenProvider = func() string { return strings.TrimSpace(os.Getenv(envName)) }
	}
	edgeSecretProvider := opts.EdgeSecretProvider
	if edgeSecretProvider == nil {
		envName := strings.TrimSpace(opts.EdgeSecretEnv)
		edgeSecretProvider = func() string { return strings.TrimSpace(os.Getenv(envName)) }
	}
	bearerRoleProvider := opts.BearerRoleProvider
	if bearerRoleProvider == nil {
		bearerRole := strings.TrimSpace(opts.BearerRole)
		bearerRoleProvider = func() string { return bearerRole }
	}
	adminUsernameProvider := opts.AdminUsernameProvider
	if adminUsernameProvider == nil {
		adminUsernameProvider = func() string { return "" }
	}
	adminPasswordHashProvider := opts.AdminPasswordHashProvider
	if adminPasswordHashProvider == nil {
		adminPasswordHashProvider = func() string { return "" }
	}
	smokeTokenProvider := opts.SmokeTokenProvider
	if smokeTokenProvider == nil {
		smokeTokenProvider = func() string { return "" }
	}
	trustedProxies, _ := httpx.NewTrustedProxies(opts.TrustedProxyCIDRs)
	publicSet := make(map[string]struct{}, len(opts.AllowPublicPaths))
	for _, p := range opts.AllowPublicPaths {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			publicSet[trimmed] = struct{}{}
		}
	}
	bootstrapSet := make(map[string]struct{}, len(opts.AllowBootstrapPaths))
	for _, p := range opts.AllowBootstrapPaths {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			bootstrapSet[trimmed] = struct{}{}
		}
	}
	htmlUnauthorizedSet := make(map[string]struct{}, len(opts.HTMLUnauthorizedPaths))
	for _, p := range opts.HTMLUnauthorizedPaths {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			htmlUnauthorizedSet[trimmed] = struct{}{}
		}
	}

	return authRuntime{
		opts:                      opts,
		modeProvider:              modeProvider,
		bearerTokenProvider:       bearerTokenProvider,
		edgeSecretProvider:        edgeSecretProvider,
		bearerRoleProvider:        bearerRoleProvider,
		adminUsernameProvider:     adminUsernameProvider,
		adminPasswordHashProvider: adminPasswordHashProvider,
		smokeTokenProvider:        smokeTokenProvider,
		oidcVerifier:              opts.OIDCVerifier,
		publicSet:                 publicSet,
		publicRoutes:              opts.AllowPublicRoutes,
		bootstrapSet:              bootstrapSet,
		bootstrapRoutes:           opts.AllowBootstrapRoutes,
		htmlUnauthorizedSet:       htmlUnauthorizedSet,
		htmlUnauthorizedRoutes:    opts.HTMLUnauthorizedRoutes,
		trustedProxies:            trustedProxies,
	}
}

func (a authRuntime) serveHTTP(w http.ResponseWriter, r *http.Request, next http.Handler) {
	if a.publicAllowed(r) || a.bootstrapAllowed(r) {
		next.ServeHTTP(w, r)
		return
	}

	adminUsername, adminPasswordHash := a.adminCredentials()
	if a.handleAdminSessionEndpoint(w, r, adminUsername, adminPasswordHash) {
		return
	}
	if id, ok := a.authenticateAdmin(r, adminUsername, adminPasswordHash); ok {
		a.serveAuthenticated(next, w, r, id)
		return
	}
	if id, ok := a.authenticateConfiguredMode(r); ok {
		a.serveAuthenticated(next, w, r, id)
		return
	}
	a.writeUnauthorized(w, r, adminUsername, adminPasswordHash)
}

func (a authRuntime) publicAllowed(r *http.Request) bool {
	if _, public := a.publicSet[r.URL.Path]; public {
		return true
	}
	return routeAllowed(a.publicRoutes, r.URL.Path, r.Method)
}

func (a authRuntime) bootstrapAllowed(r *http.Request) bool {
	if a.opts.BootstrapAllowed == nil || !a.opts.BootstrapAllowed(r) {
		return false
	}
	if _, bootstrap := a.bootstrapSet[r.URL.Path]; bootstrap {
		return true
	}
	return routeAllowed(a.bootstrapRoutes, r.URL.Path, r.Method)
}

func (a authRuntime) adminCredentials() (string, string) {
	return strings.TrimSpace(a.adminUsernameProvider()), strings.TrimSpace(a.adminPasswordHashProvider())
}

func (a authRuntime) authenticateAdmin(r *http.Request, username, passwordHash string) (Identity, bool) {
	if id, ok := verifyAdminSession(r, username, passwordHash); ok {
		return id, true
	}
	if id, ok := verifyBasicAdmin(r, username, passwordHash); ok {
		return id, true
	}
	return Identity{}, false
}

func (a authRuntime) authenticateConfiguredMode(r *http.Request) (Identity, bool) {
	mode := AuthMode(strings.TrimSpace(strings.ToLower(a.modeProvider())))
	if mode == "" {
		mode = AuthModeNone
	}
	if mode == AuthModeOIDC || mode == AuthModeBearerOrOIDC {
		// OIDC validation lives in a stateful validator (JWKS cache), injected
		// as a verifier rather than threaded through the pure verify() switch.
		// The combined mode tries the static bearer first: constant-time
		// compare, no JWKS round trip for service callers.
		if mode == AuthModeBearerOrOIDC {
			if id, ok := verifyBearer(r, strings.TrimSpace(a.bearerTokenProvider()), strings.TrimSpace(a.bearerRoleProvider())); ok {
				return id, true
			}
		}
		if a.oidcVerifier != nil {
			if id, ok := a.oidcVerifier(r); ok {
				return id, true
			}
		}
		return verifySmoke(r, strings.TrimSpace(a.smokeTokenProvider()))
	}
	if id, ok := verify(
		mode,
		r,
		strings.TrimSpace(a.bearerTokenProvider()),
		strings.TrimSpace(a.edgeSecretProvider()),
		strings.TrimSpace(a.bearerRoleProvider()),
		a.opts.RequireAuthenticatedMode,
	); ok {
		return id, true
	}
	return verifySmoke(r, strings.TrimSpace(a.smokeTokenProvider()))
}

func (a authRuntime) serveAuthenticated(next http.Handler, w http.ResponseWriter, r *http.Request, id Identity) {
	if id.Source == "basic" {
		username, passwordHash := a.adminCredentials()
		a.setAdminSessionCookie(w, r, username, passwordHash)
	}
	ctx := context.WithValue(r.Context(), identityCtxKey{}, id)
	// Attach the edge-forwarded OBO subject token ONLY when this request's
	// identity was proven via edge-HMAC. Any other source (bearer, admin,
	// smoke, none, oidc) cannot inject a credential through this header.
	// The value is a secret: context-only, never logged.
	if id.Source == "edge_hmac" {
		header := strings.TrimSpace(a.opts.OboSubjectTokenHeader)
		if header == "" {
			header = EdgeOboSubjectTokenHeader
		}
		if token := strings.TrimSpace(r.Header.Get(header)); token != "" {
			ctx = context.WithValue(ctx, edgeOboSubjectTokenCtxKey{}, token)
		}
		// Edge-resolved voice preferences carry their own versioned HMAC
		// (same shared secret) bound to this verified identity plus a
		// timestamp; an invalid overlay degrades to "no preference" and
		// never fails the request. Non-secret provider/persona names; see
		// voiceprefs.go for the contract.
		if prefs := verifiedVoicePrefsFromRequest(r, id, strings.TrimSpace(a.edgeSecretProvider()), time.Now()); !prefs.IsZero() {
			ctx = context.WithValue(ctx, voicePrefsCtxKey{}, prefs)
		}
		binding, present, err := verifiedVoiceAgentBindingFromRequest(r, id, strings.TrimSpace(a.edgeSecretProvider()))
		if err != nil {
			writeAuthError(w)
			return
		}
		if present {
			ctx = context.WithValue(ctx, voiceAgentBindingCtxKey{}, binding)
		}
	}
	next.ServeHTTP(w, r.WithContext(ctx))
}

func (a authRuntime) writeUnauthorized(w http.ResponseWriter, r *http.Request, adminUsername, adminPasswordHash string) {
	if browserUnauthorizedResponse(a.htmlUnauthorizedSet, a.htmlUnauthorizedRoutes, r) && adminUsername != "" && adminPasswordHash != "" {
		writeBrowserAuthError(w, r)
		return
	}
	writeAuthError(w)
}

func routeAllowed(routes []PublicRoute, path, method string) bool {
	for _, route := range routes {
		if !routePathAllowed(route, path) {
			continue
		}
		if methodAllowed(route.Methods, method) {
			return true
		}
	}
	return false
}

func routePathAllowed(route PublicRoute, path string) bool {
	if route.Path != "" {
		return path == route.Path
	}
	if route.PathPrefix == "" && route.PathSuffix == "" {
		return false
	}
	if route.PathPrefix != "" && !strings.HasPrefix(path, route.PathPrefix) {
		return false
	}
	if route.PathSuffix != "" && !strings.HasSuffix(path, route.PathSuffix) {
		return false
	}
	return true
}

func methodAllowed(methods []string, method string) bool {
	requestMethod := strings.ToUpper(strings.TrimSpace(method))
	for _, allowed := range methods {
		if strings.ToUpper(strings.TrimSpace(allowed)) == requestMethod {
			return true
		}
	}
	return false
}
