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
	"strings"
	"sync"
	"time"

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
	// Bootstrap routes are public only while BootstrapAllowed returns true.
	// The server uses this for the first settings write when bearer auth is
	// configured but no bearer token exists yet.
	AllowBootstrapPaths  []string
	AllowBootstrapRoutes []PublicRoute
	BootstrapAllowed     func(*http.Request) bool
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
	publicSet := make(map[string]struct{}, len(opts.AllowPublicPaths))
	for _, p := range opts.AllowPublicPaths {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			publicSet[trimmed] = struct{}{}
		}
	}
	publicRoutes := opts.AllowPublicRoutes
	bootstrapSet := make(map[string]struct{}, len(opts.AllowBootstrapPaths))
	for _, p := range opts.AllowBootstrapPaths {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			bootstrapSet[trimmed] = struct{}{}
		}
	}
	bootstrapRoutes := opts.AllowBootstrapRoutes
	htmlUnauthorizedSet := make(map[string]struct{}, len(opts.HTMLUnauthorizedPaths))
	for _, p := range opts.HTMLUnauthorizedPaths {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			htmlUnauthorizedSet[trimmed] = struct{}{}
		}
	}
	htmlUnauthorizedRoutes := opts.HTMLUnauthorizedRoutes

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, public := publicSet[r.URL.Path]; public {
				next.ServeHTTP(w, r)
				return
			}
			if routeAllowed(publicRoutes, r.URL.Path, r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			if _, bootstrap := bootstrapSet[r.URL.Path]; bootstrap && opts.BootstrapAllowed != nil && opts.BootstrapAllowed(r) {
				next.ServeHTTP(w, r)
				return
			}
			if routeAllowed(bootstrapRoutes, r.URL.Path, r.Method) && opts.BootstrapAllowed != nil && opts.BootstrapAllowed(r) {
				next.ServeHTTP(w, r)
				return
			}

			mode := AuthMode(strings.TrimSpace(strings.ToLower(modeProvider())))
			if mode == "" {
				mode = AuthModeNone
			}
			adminUsername := strings.TrimSpace(adminUsernameProvider())
			adminPasswordHash := strings.TrimSpace(adminPasswordHashProvider())
			if handleAdminSessionEndpoint(w, r, adminUsername, adminPasswordHash) {
				return
			}
			if id, ok := verifyAdminSession(r, adminUsername, adminPasswordHash); ok {
				ctx := context.WithValue(r.Context(), identityCtxKey{}, id)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if id, ok := verifyBasicAdmin(r, adminUsername, adminPasswordHash); ok {
				setAdminSessionCookie(w, r, adminUsername, adminPasswordHash)
				ctx := context.WithValue(r.Context(), identityCtxKey{}, id)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			id, ok := verify(mode, r, strings.TrimSpace(bearerTokenProvider()), strings.TrimSpace(edgeSecretProvider()), strings.TrimSpace(bearerRoleProvider()))
			if !ok {
				if browserUnauthorizedResponse(htmlUnauthorizedSet, htmlUnauthorizedRoutes, r) {
					writeBrowserAuthError(w, r)
				} else {
					writeAuthError(w)
				}
				return
			}
			ctx := context.WithValue(r.Context(), identityCtxKey{}, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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

func verify(mode AuthMode, r *http.Request, bearerToken, edgeSecret, bearerRole string) (Identity, bool) {
	switch mode {
	case AuthModeNone:
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

func handleAdminSessionEndpoint(w http.ResponseWriter, r *http.Request, username, passwordHash string) bool {
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
		setAdminSessionCookie(w, r, username, passwordHash)
		writeAdminSessionJSON(w, true, id)
		return true
	case http.MethodDelete:
		clearAdminSessionCookie(w, r)
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

func setAdminSessionCookie(w http.ResponseWriter, r *http.Request, username, passwordHash string) {
	expires := time.Now().Add(adminSessionTTL)
	token := signAdminSessionToken(adminSessionClaims{
		User:      username,
		ExpiresAt: expires.Unix(),
		Nonce:     base64.RawURLEncoding.EncodeToString(mustRandomBytes(16)),
	}, passwordHash)
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(adminSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearAdminSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
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

func requestIsHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func mustRandomBytes(size int) []byte {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		panic("speechkit admin session entropy unavailable: " + err.Error())
	}
	return buf
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

func verifyEdgeHMAC(r *http.Request, secret string) (Identity, bool) {
	if secret == "" {
		return Identity{}, false
	}
	presented := strings.TrimSpace(r.Header.Get("X-Edge-Auth-Hmac"))
	userID := strings.TrimSpace(r.Header.Get("X-Edge-User-Id"))
	orgID := strings.TrimSpace(r.Header.Get("X-Edge-Org-Id"))
	plan := strings.TrimSpace(r.Header.Get("X-Edge-Plan"))
	role := strings.TrimSpace(r.Header.Get("X-Edge-Role"))
	if presented == "" || userID == "" || orgID == "" {
		return Identity{}, false
	}
	// Signature base: userID + "\n" + orgID + "\n" + plan + "\n" + role.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(userID))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(orgID))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(plan))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(role))
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

func writeBrowserAuthError(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Add("WWW-Authenticate", `Basic realm="speechkit-admin", charset="UTF-8"`)
	w.Header().Add("WWW-Authenticate", `Bearer realm="speechkit"`)
	w.WriteHeader(http.StatusUnauthorized)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>SpeechKit Admin Sign-In Required</title>
  <style>
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
  </style>
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
  <script>
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
  </script>
</body>
</html>`))
}
