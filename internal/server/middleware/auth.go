//go:build linux

package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
)

// Identity is attached to the request context by Auth and consumed by mode
// handlers for rate-limit keying, session ownership, and audit logs.
type Identity struct {
	UserID string `json:"user_id"`
	OrgID  string `json:"org_id"`
	Plan   string `json:"plan"`
	Role   string `json:"role,omitempty"` // "admin" | "" (default)
	Source string `json:"source"`         // "none" | "bearer" | "edge_hmac"
}

type identityCtxKey struct{}

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
	AllowPublicPaths  []string // exact path matches that skip auth entirely (e.g. /healthz)
	AllowPublicRoutes []PublicRoute
	// Dynamic providers are evaluated for every request. They let first-run
	// setup generate a token without rebuilding the middleware chain.
	ModeProvider        func() string
	BearerTokenProvider func() string
	EdgeSecretProvider  func() string
	// Bootstrap routes are public only while BootstrapAllowed returns true.
	// The server uses this for the first settings write when bearer auth is
	// configured but no bearer token exists yet.
	AllowBootstrapPaths  []string
	AllowBootstrapRoutes []PublicRoute
	BootstrapAllowed     func(*http.Request) bool
}

type PublicRoute struct {
	Path    string
	Methods []string
}

// AuthState is a concurrency-safe view of the mutable auth config used by the
// server setup flow. It stores env var names, not secret values.
type AuthState struct {
	mu             sync.RWMutex
	mode           string
	bearerTokenEnv string
	edgeSecretEnv  string
}

func NewAuthState(mode, bearerTokenEnv, edgeSecretEnv string) *AuthState {
	return &AuthState{
		mode:           strings.TrimSpace(mode),
		bearerTokenEnv: strings.TrimSpace(bearerTokenEnv),
		edgeSecretEnv:  strings.TrimSpace(edgeSecretEnv),
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
	publicSet := make(map[string]struct{}, len(opts.AllowPublicPaths))
	for _, p := range opts.AllowPublicPaths {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			publicSet[trimmed] = struct{}{}
		}
	}
	publicRoutes := routeSet(opts.AllowPublicRoutes)
	bootstrapSet := make(map[string]struct{}, len(opts.AllowBootstrapPaths))
	for _, p := range opts.AllowBootstrapPaths {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			bootstrapSet[trimmed] = struct{}{}
		}
	}
	bootstrapRoutes := routeSet(opts.AllowBootstrapRoutes)

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
			id, ok := verify(mode, r, strings.TrimSpace(bearerTokenProvider()), strings.TrimSpace(edgeSecretProvider()))
			if !ok {
				writeAuthError(w)
				return
			}
			ctx := context.WithValue(r.Context(), identityCtxKey{}, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func routeSet(routes []PublicRoute) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{}, len(routes))
	for _, route := range routes {
		path := strings.TrimSpace(route.Path)
		if path == "" {
			continue
		}
		methods := out[path]
		if methods == nil {
			methods = map[string]struct{}{}
			out[path] = methods
		}
		for _, method := range route.Methods {
			if normalized := strings.ToUpper(strings.TrimSpace(method)); normalized != "" {
				methods[normalized] = struct{}{}
			}
		}
	}
	return out
}

func routeAllowed(routes map[string]map[string]struct{}, path, method string) bool {
	methods, ok := routes[path]
	if !ok {
		return false
	}
	_, ok = methods[method]
	return ok
}

func verify(mode AuthMode, r *http.Request, bearerToken, edgeSecret string) (Identity, bool) {
	switch mode {
	case AuthModeNone:
		return Identity{
			UserID: "anonymous",
			OrgID:  "public",
			Plan:   "public",
			Source: "none",
		}, true
	case AuthModeBearer:
		return verifyBearer(r, bearerToken)
	case AuthModeEdgeHMAC:
		return verifyEdgeHMAC(r, edgeSecret)
	case AuthModeBearerOrEdge:
		if id, ok := verifyBearer(r, bearerToken); ok {
			return id, true
		}
		return verifyEdgeHMAC(r, edgeSecret)
	default:
		return Identity{}, false
	}
}

func verifyBearer(r *http.Request, expected string) (Identity, bool) {
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
