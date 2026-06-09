//go:build linux

package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"golang.org/x/crypto/bcrypt"
)

// Identity is attached to the request context by Auth and consumed by mode
// handlers for rate-limit keying, session ownership, and audit logs.
type Identity struct {
	UserID string `json:"user_id"`
	OrgID  string `json:"org_id"`
	Plan   string `json:"plan"`
	Role   string `json:"role,omitempty"` // "admin" | "" (default)
	Source string `json:"source"`         // "none" | "bearer" | "edge_hmac" | "basic" | "admin_session"
}

type identityCtxKey struct{}

const (
	adminSessionCookieName = "speechkit_admin_session"
	adminSessionTTL        = 30 * time.Minute
	// adminCSRFCookieName is the JS-readable companion to the admin
	// session cookie. The pair implements double-submit CSRF protection
	// (audit S-13): the SPA reads the cookie value with JS and echoes it
	// into the X-CSRF-Token header on state-changing requests. The
	// browser can read the value because HttpOnly is false, but a
	// cross-site attacker cannot — CORS blocks the read, and SameSite
	// blocks the cookie from being attached to forged cross-origin
	// requests. The header is the proof the request originated from
	// our origin.
	adminCSRFCookieName = "speechkit_admin_csrf"
	// adminCSRFHeaderName is the request header the SPA sends. Picked
	// to match the de-facto convention used by Django, Rails, etc.
	adminCSRFHeaderName = "X-CSRF-Token"
)

// AdminSessionCookieName is exported for server integration tests that verify
// setup/auth transitions without duplicating the cookie contract.
const AdminSessionCookieName = adminSessionCookieName

// AdminCSRFCookieName and AdminCSRFHeaderName are exported so server
// integration tests and the SPA build pipeline can reference the
// double-submit contract without re-declaring it.
const (
	AdminCSRFCookieName = adminCSRFCookieName
	AdminCSRFHeaderName = adminCSRFHeaderName
)

var adminSessionSigningKey = mustRandomBytes(32)

type adminSessionClaims struct {
	User      string `json:"user"`
	ExpiresAt int64  `json:"expires_at"`
	Nonce     string `json:"nonce"`
}

// IdentityFromContext returns the Identity attached by Auth, or the zero
// Identity if none is present.
func IdentityFromContext(ctx context.Context) Identity {
	if v, ok := ctx.Value(identityCtxKey{}).(Identity); ok {
		return v
	}
	return Identity{}
}

// InjectIdentityForTest attaches an Identity to the context using the same
// unexported key the Auth middleware uses. Exported solely so handler tests in
// external packages can exercise endpoints that depend on
// IdentityFromContext without spinning up the full auth middleware. Production
// code MUST NOT use this; the function name and the package it lives in are
// intentionally awkward to make accidental use loud at review time.
func InjectIdentityForTest(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityCtxKey{}, id)
}

// AuthMode selects which credential format the server accepts.
type AuthMode string

const (
	// AuthModeNone disables built-in server authentication. The request still
	// receives a stable anonymous identity so mode handlers can apply session
	// ownership and rate-limit logic without requiring an upstream auth layer.
	AuthModeNone AuthMode = "none"
	// AuthModeBearer requires a static bearer token from the configured env
	// var. Minimum viable auth; suitable for same-network service-to-service
	// calls (e.g. kombify-AI → speechkit over Render private network).
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
}

type PublicRoute struct {
	Path       string
	PathPrefix string
	PathSuffix string
	Methods    []string
}

// AuthState is a concurrency-safe view of the mutable auth config used by the
// server setup flow. It stores env var names, not secret values.
type AuthState struct {
	mu                sync.RWMutex
	mode              string
	bearerTokenEnv    string
	edgeSecretEnv     string
	smokeTokenEnv     string
	adminUsername     string
	adminPasswordHash string
}

func NewAuthState(mode, bearerTokenEnv, edgeSecretEnv, adminUsername, adminPasswordHash string) *AuthState {
	return &AuthState{
		mode:              strings.TrimSpace(mode),
		bearerTokenEnv:    strings.TrimSpace(bearerTokenEnv),
		edgeSecretEnv:     strings.TrimSpace(edgeSecretEnv),
		adminUsername:     strings.TrimSpace(adminUsername),
		adminPasswordHash: strings.TrimSpace(adminPasswordHash),
	}
}

// SetSmokeTokenEnv records the env var name holding the optional public
// demo token. Empty string disables smoke-from-page authentication.
func (s *AuthState) SetSmokeTokenEnv(envName string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.smokeTokenEnv = strings.TrimSpace(envName)
}

// SmokeTokenEnv returns the configured env var name for the demo token.
func (s *AuthState) SmokeTokenEnv() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.smokeTokenEnv
}

// SmokeToken resolves the current value of the demo bearer token, or "" if
// the env var is unset / unconfigured.
func (s *AuthState) SmokeToken() string {
	envName := s.SmokeTokenEnv()
	if envName == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(envName))
}

func (s *AuthState) Mode() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}

func (s *AuthState) BearerTokenEnv() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bearerTokenEnv
}

func (s *AuthState) BearerToken() string {
	return strings.TrimSpace(os.Getenv(strings.TrimSpace(s.BearerTokenEnv())))
}

func (s *AuthState) EdgeSecret() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	envName := s.edgeSecretEnv
	s.mu.RUnlock()
	return strings.TrimSpace(os.Getenv(strings.TrimSpace(envName)))
}

func (s *AuthState) AdminUsername() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.adminUsername
}

func (s *AuthState) AdminPasswordHash() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.adminPasswordHash
}

func (s *AuthState) Set(mode, bearerTokenEnv, edgeSecretEnv string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if value := strings.TrimSpace(mode); value != "" {
		s.mode = value
	}
	if value := strings.TrimSpace(bearerTokenEnv); value != "" {
		s.bearerTokenEnv = value
	}
	if value := strings.TrimSpace(edgeSecretEnv); value != "" {
		s.edgeSecretEnv = value
	}
}

func (s *AuthState) SetAdmin(username, passwordHash string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if value := strings.TrimSpace(username); value != "" {
		s.adminUsername = value
	}
	if value := strings.TrimSpace(passwordHash); value != "" {
		s.adminPasswordHash = value
	}
}

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
	if mode == AuthModeOIDC {
		// OIDC validation lives in a stateful validator (JWKS cache), injected
		// as a verifier rather than threaded through the pure verify() switch.
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

func browserUnauthorizedResponse(paths map[string]struct{}, routes []PublicRoute, r *http.Request) bool {
	if r == nil {
		return false
	}
	if _, ok := paths[r.URL.Path]; !ok && !routeAllowed(routes, r.URL.Path, r.Method) {
		return false
	}
	return requestAcceptsHTML(r)
}

func requestAcceptsHTML(r *http.Request) bool {
	accept := strings.ToLower(strings.TrimSpace(r.Header.Get("Accept")))
	return accept == "" || strings.Contains(accept, "text/html") || strings.Contains(accept, "*/*")
}

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

func verifyBasicAdmin(r *http.Request, username, passwordHash string) (Identity, bool) {
	if username == "" || passwordHash == "" {
		return Identity{}, false
	}
	presentedUser, presentedPassword, ok := r.BasicAuth()
	if !ok || presentedUser == "" || presentedPassword == "" {
		return Identity{}, false
	}
	if !hmacEqual([]byte(presentedUser), []byte(username)) {
		return Identity{}, false
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(presentedPassword)) != nil {
		return Identity{}, false
	}
	return Identity{
		UserID: username,
		OrgID:  "default",
		Plan:   "admin",
		Role:   "admin",
		Source: "basic",
	}, true
}

func AdminPasswordMatches(username, passwordHash, presentedUser, presentedPassword string) bool {
	username = strings.TrimSpace(username)
	passwordHash = strings.TrimSpace(passwordHash)
	presentedUser = strings.TrimSpace(presentedUser)
	if username == "" || passwordHash == "" || presentedUser == "" || presentedPassword == "" {
		return false
	}
	return hmacEqual([]byte(presentedUser), []byte(username)) &&
		bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(presentedPassword)) == nil
}

func NewAdminSessionCookie(username, passwordHash string, secure bool, now time.Time) (*http.Cookie, error) {
	username = strings.TrimSpace(username)
	passwordHash = strings.TrimSpace(passwordHash)
	if username == "" || passwordHash == "" {
		return nil, http.ErrNoCookie
	}
	if now.IsZero() {
		now = time.Now()
	}
	expires := now.Add(adminSessionTTL)
	token := signAdminSessionToken(adminSessionClaims{
		User:      username,
		ExpiresAt: expires.Unix(),
		Nonce:     base64.RawURLEncoding.EncodeToString(mustRandomBytes(16)),
	}, passwordHash)
	if token == "" {
		return nil, http.ErrNoCookie
	}
	return &http.Cookie{ // #nosec G124 -- Secure follows the request/proxy scheme so local HTTP admin login keeps working.
		Name:     adminSessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(adminSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}, nil
}

// NewAdminCSRFCookie returns the JS-readable double-submit CSRF cookie
// that pairs with the admin session cookie minted by
// NewAdminSessionCookie. Callers that issue the session cookie MUST
// also issue this cookie in the same response — otherwise the SPA has
// no token to echo in X-CSRF-Token and EnforceAdminCSRF will refuse
// the next state-changing request from the admin_session identity.
//
// sessionCookieValue is the Value of the session cookie returned by
// NewAdminSessionCookie. The returned CSRF cookie carries
// csrfTokenFor(sessionCookieValue) so the binding HMAC over the
// session signature also covers this cookie.
func NewAdminCSRFCookie(sessionCookieValue string, secure bool, now time.Time) *http.Cookie {
	if strings.TrimSpace(sessionCookieValue) == "" {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	expires := now.Add(adminSessionTTL)
	return &http.Cookie{ // #nosec G124 -- CSRF double-submit cookie must be JS-readable; SameSite Strict binds it to same-origin requests.
		Name:     adminCSRFCookieName,
		Value:    csrfTokenFor(sessionCookieValue),
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(adminSessionTTL.Seconds()),
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	}
}

func (a authRuntime) handleAdminSessionEndpoint(w http.ResponseWriter, r *http.Request, username, passwordHash string) bool {
	if r == nil || !isAdminSessionPath(r.URL.Path) {
		return false
	}
	switch r.Method {
	case http.MethodGet:
		if id, ok := verifyAdminSession(r, username, passwordHash); ok {
			writeAdminSessionJSON(w, true, id)
			return true
		}
		writeAuthError(w)
		return true
	case http.MethodPost:
		id, ok := verifyBasicAdmin(r, username, passwordHash)
		if !ok {
			writeAuthError(w)
			return true
		}
		a.setAdminSessionCookie(w, r, username, passwordHash)
		writeAdminSessionJSON(w, true, id)
		return true
	case http.MethodDelete:
		a.clearAdminSessionCookie(w, r)
		w.WriteHeader(http.StatusNoContent)
		return true
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
		return true
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
}

func isAdminSessionPath(path string) bool {
	return path == "/v1/admin/session" || path == "/api/v1/admin/session"
}

func verifyAdminSession(r *http.Request, username, passwordHash string) (Identity, bool) {
	if username == "" || passwordHash == "" {
		return Identity{}, false
	}
	cookie, err := r.Cookie(adminSessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return Identity{}, false
	}
	claims, ok := parseAdminSessionToken(cookie.Value, passwordHash)
	if !ok {
		return Identity{}, false
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return Identity{}, false
	}
	if !hmacEqual([]byte(claims.User), []byte(username)) {
		return Identity{}, false
	}
	return Identity{
		UserID: username,
		OrgID:  "default",
		Plan:   "admin",
		Role:   "admin",
		Source: "admin_session",
	}, true
}

func (a authRuntime) setAdminSessionCookie(w http.ResponseWriter, r *http.Request, username, passwordHash string) {
	expires := time.Now().Add(adminSessionTTL)
	secure := a.trustedProxies.RequestIsHTTPS(r)
	token := signAdminSessionToken(adminSessionClaims{
		User:      username,
		ExpiresAt: expires.Unix(),
		Nonce:     base64.RawURLEncoding.EncodeToString(mustRandomBytes(16)),
	}, passwordHash)
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure follows the request/proxy scheme so local HTTP admin login keeps working.
		Name:     adminSessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(adminSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- CSRF double-submit cookie must be JS-readable; SameSite Strict binds it to same-origin requests.
		Name:  adminCSRFCookieName,
		Value: csrfTokenFor(token),
		Path:  "/",
		// CSRF cookie shares the session lifetime — when the session
		// expires, the cookie disappears with it and a stale token
		// won't validate against a fresh session signature anyway.
		Expires:  expires,
		MaxAge:   int(adminSessionTTL.Seconds()),
		HttpOnly: false, // JS must read this to set the X-CSRF-Token header.
		Secure:   secure,
		// Strict because the cookie is only consumed by our own SPA
		// running on the same origin; tightening from Lax (used by the
		// session cookie for top-level navigation) costs nothing here.
		SameSite: http.SameSiteStrictMode,
	})
}

func (a authRuntime) clearAdminSessionCookie(w http.ResponseWriter, r *http.Request) {
	secure := a.trustedProxies.RequestIsHTTPS(r)
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- deletion cookie mirrors the session cookie attributes for reliable expiry.
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- deletion cookie mirrors the JS-readable CSRF cookie attributes for reliable expiry.
		Name:     adminCSRFCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// csrfTokenFor derives a deterministic CSRF token from the admin
// session cookie value. Using HMAC over the session signature binds the
// CSRF cookie to its session — a stale CSRF cookie cannot validate
// against a freshly issued session, and an attacker who somehow obtains
// only the CSRF cookie (e.g. via XSS on a non-credential surface) still
// cannot forge a session.
//
// The signing key is the process-local adminSessionSigningKey: it
// rotates on every restart, so observability of a token leaks only
// within one process lifetime.
func csrfTokenFor(adminSessionToken string) string {
	mac := hmac.New(sha256.New, adminSessionSigningKey)
	mac.Write([]byte(adminSessionToken))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// ValidateAdminCSRF returns true when the request carries a CSRF
// cookie + matching X-CSRF-Token header AND the cookie value matches
// csrfTokenFor(<session-cookie>). All comparisons are constant-time.
// Returns false when any input is missing or mismatched — callers
// should reject the request with 403.
func ValidateAdminCSRF(r *http.Request) bool {
	header := strings.TrimSpace(r.Header.Get(adminCSRFHeaderName))
	if header == "" {
		return false
	}
	cookie, err := r.Cookie(adminCSRFCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return false
	}
	if !hmacEqual([]byte(cookie.Value), []byte(header)) {
		return false
	}
	sessionCookie, err := r.Cookie(adminSessionCookieName)
	if err != nil || strings.TrimSpace(sessionCookie.Value) == "" {
		return false
	}
	want := csrfTokenFor(sessionCookie.Value)
	return hmacEqual([]byte(cookie.Value), []byte(want))
}

// EnforceAdminCSRF is the handler-level helper for endpoints that
// accept admin-session-cookie-authenticated state changes. Call it at
// the top of the handler when the resolved Identity has
// Source="admin_session"; the helper writes 403 + JSON envelope when
// the double-submit check fails and returns false (caller MUST return
// without further writes).
//
// Bearer / edge-HMAC / smoke callers are not subject to CSRF (their
// credentials are not automatically attached by the browser to
// cross-origin requests), so they should skip this check entirely.
func EnforceAdminCSRF(w http.ResponseWriter, r *http.Request) bool {
	id := IdentityFromContext(r.Context())
	if id.Source != "admin_session" {
		return true
	}
	if ValidateAdminCSRF(r) {
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":{"code":"csrf_required","message":"X-CSRF-Token header missing or invalid"}}`))
	return false
}

func writeAdminSessionJSON(w http.ResponseWriter, authenticated bool, id Identity) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"authenticated": authenticated,
		"identity":      id,
		"expires_in":    int(adminSessionTTL.Seconds()),
	})
}

func signAdminSessionToken(claims adminSessionClaims, passwordHash string) string {
	payload, err := json.Marshal(claims)
	if err != nil {
		return ""
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, adminSessionSigningSecret(passwordHash))
	mac.Write([]byte(encodedPayload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encodedPayload + "." + signature
}

func parseAdminSessionToken(token, passwordHash string) (adminSessionClaims, bool) {
	payload, signature, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || payload == "" || signature == "" {
		return adminSessionClaims{}, false
	}
	mac := hmac.New(sha256.New, adminSessionSigningSecret(passwordHash))
	mac.Write([]byte(payload))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmacEqual([]byte(signature), []byte(want)) {
		return adminSessionClaims{}, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return adminSessionClaims{}, false
	}
	var claims adminSessionClaims
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return adminSessionClaims{}, false
	}
	return claims, true
}

func adminSessionSigningSecret(passwordHash string) []byte {
	mac := hmac.New(sha256.New, adminSessionSigningKey)
	mac.Write([]byte(strings.TrimSpace(passwordHash)))
	return mac.Sum(nil)
}

func mustRandomBytes(size int) []byte {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		panic("speechkit admin session entropy unavailable: " + err.Error())
	}
	return buf
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

// edgeHMACMaxSkew bounds how far the optional X-Edge-Auth-Ts may deviate from
// server time. It is the replay window: a captured edge-signed header is only
// accepted within this interval once the edge starts sending a timestamp.
const edgeHMACMaxSkew = 5 * time.Minute

func verifyEdgeHMAC(r *http.Request, secret string) (Identity, bool) {
	if secret == "" {
		return Identity{}, false
	}
	presented := strings.TrimSpace(r.Header.Get("X-Edge-Auth-Hmac"))
	userID := strings.TrimSpace(r.Header.Get("X-Edge-User-Id"))
	orgID := strings.TrimSpace(r.Header.Get("X-Edge-Org-Id"))
	plan := strings.TrimSpace(r.Header.Get("X-Edge-Plan"))
	role := strings.TrimSpace(r.Header.Get("X-Edge-Role"))
	ts := strings.TrimSpace(r.Header.Get("X-Edge-Auth-Ts"))
	if presented == "" || userID == "" || orgID == "" {
		return Identity{}, false
	}
	// Signature base: userID + "\n" + orgID + "\n" + plan + "\n" + role,
	// with the timestamp appended as a fifth field ("\n" + ts) when the edge
	// supplies one. Binding the timestamp into the MAC bounds replayability;
	// a request without X-Edge-Auth-Ts uses the legacy unbound base so an
	// older edge signer keeps working (backward compatible). A downgrade —
	// stripping the timestamp from a captured ts-bound request — fails
	// because the presented HMAC was computed over the ts-bound base and
	// will not match the legacy base recomputed here.
	if ts != "" && !edgeTimestampFresh(ts, time.Now()) {
		return Identity{}, false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(userID))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(orgID))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(plan))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(role))
	if ts != "" {
		mac.Write([]byte{'\n'})
		mac.Write([]byte(ts))
	}
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmacEqual([]byte(presented), []byte(want)) {
		return Identity{}, false
	}
	return Identity{
		UserID: userID,
		OrgID:  orgID,
		Plan:   plan,
		Role:   role,
		Source: "edge_hmac",
	}, true
}

// edgeTimestampFresh reports whether ts (Unix seconds as a decimal string) is
// within edgeHMACMaxSkew of now. A malformed timestamp is rejected so a
// garbage value cannot bypass the freshness check.
func edgeTimestampFresh(ts string, now time.Time) bool {
	secs, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	delta := now.Sub(time.Unix(secs, 0))
	if delta < 0 {
		delta = -delta
	}
	return delta <= edgeHMACMaxSkew
}

func hmacEqual(a, b []byte) bool {
	return hmac.Equal(a, b)
}

func writeAuthError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="speechkit"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    "unauthenticated",
			"message": "missing or invalid credentials",
		},
	})
}

// adminAuthErrorStyleCSS and adminAuthErrorScriptJS hold the exact inline
// <style>/<script> bodies of the admin sign-in page. They are kept as named
// constants so their sha256 hashes can be pinned in the page's
// Content-Security-Policy — letting the page keep its inline assets without
// 'unsafe-inline'. The hash is computed over these constants and the page is
// assembled from the same constants, so the two can never drift.
const adminAuthErrorStyleCSS = `
    :root { color-scheme: light dark; font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #f7f8fa; color: #1f2933; }
    main { max-width: 38rem; padding: 2rem; }
    h1 { font-size: 1.75rem; margin: 0 0 0.75rem; }
    p { line-height: 1.55; margin: 0.5rem 0; }
    form { display: grid; gap: .85rem; margin-top: 1.25rem; }
    label { display: grid; gap: .35rem; font-size: .94rem; }
    input { min-height: 2.5rem; border: 1px solid #cbd5e1; border-radius: .4rem; padding: .55rem .7rem; font: inherit; }
    button { min-height: 2.6rem; border: 0; border-radius: .4rem; background: #111827; color: #fff; font: inherit; cursor: pointer; }
    .error { color: #b91c1c; min-height: 1.4rem; }
    code { font-family: ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace; }
    @media (prefers-color-scheme: dark) { body { background: #111827; color: #f3f4f6; } input { background: #0b1220; color: #f3f4f6; border-color: #374151; } button { background: #2563eb; } .error { color: #fca5a5; } }
  `

const adminAuthErrorScriptJS = `
    (function () {
      const form = document.getElementById("adminLogin");
      const error = document.getElementById("loginError");
      function adminSessionPath() {
        return window.location.pathname.indexOf("/api/") === 0 ? "/api/v1/admin/session" : "/v1/admin/session";
      }
      async function tryLogin(username, password) {
        const response = await fetch(adminSessionPath(), {
          method: "POST",
          headers: { "Authorization": "Basic " + btoa(username + ":" + password), "Accept": "application/json" },
          credentials: "same-origin"
        });
        if (!response.ok) throw new Error("Invalid admin username or password.");
        window.location.reload();
      }
      form.addEventListener("submit", function (event) {
        event.preventDefault();
        error.textContent = "";
        const username = document.getElementById("adminUsername").value.trim();
        const password = document.getElementById("adminPassword").value;
        tryLogin(username, password).catch(function (err) {
          error.textContent = err && err.message ? err.message : String(err);
        });
      });
    })();
  `

// cspSourceHash returns the CSP source-expression (e.g. 'sha256-<base64>') for
// an inline element body, so it can be allow-listed without 'unsafe-inline'.
func cspSourceHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// adminAuthErrorCSP locks the admin sign-in page to its own hashed inline
// style+script; connect-src 'self' lets the form POST the same-origin admin
// session endpoint. Computed once at init from the constants above.
var adminAuthErrorCSP = "default-src 'none'; style-src " + cspSourceHash(adminAuthErrorStyleCSS) +
	"; script-src " + cspSourceHash(adminAuthErrorScriptJS) +
	"; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

var adminAuthErrorHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>SpeechKit Admin Sign-In Required</title>
  <style>` + adminAuthErrorStyleCSS + `</style>
</head>
<body>
  <main>
    <h1>SpeechKit Admin Sign-In Required</h1>
    <p>The server setup UI is protected after bootstrap. Sign in with the admin account created during setup.</p>
    <form id="adminLogin">
      <label for="adminUsername">Admin username
        <input id="adminUsername" autocomplete="username" required>
      </label>
      <label for="adminPassword">Admin password
        <input id="adminPassword" type="password" autocomplete="current-password" required>
      </label>
      <button type="submit">Sign In</button>
      <div class="error" id="loginError" role="status"></div>
    </form>
    <p>API clients can continue to call <code>/api/v1/*</code> or <code>/v1/*</code> with their configured bearer token.</p>
  </main>
  <script>` + adminAuthErrorScriptJS + `</script>
</body>
</html>`

func writeBrowserAuthError(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Override the strict default CSP from SecurityHeaders with a per-page
	// policy that permits only this page's hashed inline assets.
	w.Header().Set("Content-Security-Policy", adminAuthErrorCSP)
	w.WriteHeader(http.StatusUnauthorized)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(adminAuthErrorHTML))
}
