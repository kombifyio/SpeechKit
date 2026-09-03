//go:build linux

package core

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/server/onboarding"
)

func serverPublicPaths() []string {
	paths := []string{"/healthz"}
	if envBoolDefault(config.ServerDetailedReadinessPublicEnv, true) {
		paths = append(paths, "/readyz", "/readyz/strict")
	}
	if envBoolDefault(config.ServerOperatorUIPublicEnv, true) && envBoolDefault(config.ServerOnboardingUIEnv, true) {
		paths = append(paths, "/")
	}
	return paths
}

func serverPublicRoutes() []middleware.PublicRoute {
	routes := []middleware.PublicRoute{
		{
			PathPrefix: "/v1/voiceagent/sessions/",
			PathSuffix: "/ws",
			Methods:    []string{http.MethodGet},
		},
		{
			PathPrefix: "/api/v1/voiceagent/sessions/",
			PathSuffix: "/ws",
			Methods:    []string{http.MethodGet},
		},
		// Streaming Dictation WS upgrade: authenticated by the single-use
		// HMAC ticket in Sec-WebSocket-Protocol (browsers cannot send
		// Authorization on a WS handshake). Session creation stays behind
		// the normal auth chain.
		{
			PathPrefix: "/v1/dictation/stream/sessions/",
			PathSuffix: "/ws",
			Methods:    []string{http.MethodGet},
		},
		{
			PathPrefix: "/api/v1/dictation/stream/sessions/",
			PathSuffix: "/ws",
			Methods:    []string{http.MethodGet},
		},
		// Public wake-word model catalog: ESPHome satellites and the
		// Kombify-Box fetch manifests/models without a bearer token. Read-only,
		// already-public model metadata + redirects. The authenticated
		// activation-collector (/v1/wakeword/activations) stays private.
		{Path: "/v1/wakeword/models", Methods: []string{http.MethodGet, http.MethodHead}},
		{PathPrefix: "/v1/wakeword/models/", Methods: []string{http.MethodGet, http.MethodHead}},
		{PathPrefix: "/v1/wakeword/files/", Methods: []string{http.MethodGet, http.MethodHead}},
	}
	if !envBoolDefault(config.ServerOperatorUIPublicEnv, true) {
		return routes
	}

	onboardingEnabled := envBoolDefault(config.ServerOnboardingUIEnv, true)
	assistantEnabled := envBoolDefault(config.ServerAssistantUIEnv, true)
	if assistantEnabled {
		routes = append(routes,
			middleware.PublicRoute{Path: "/assistant", Methods: []string{http.MethodGet, http.MethodHead}},
			middleware.PublicRoute{PathPrefix: "/assistant/", Methods: []string{http.MethodGet, http.MethodHead}},
		)
	}
	if onboardingEnabled || assistantEnabled {
		routes = append(routes,
			middleware.PublicRoute{Path: "/v1/server/settings", Methods: []string{http.MethodGet, http.MethodHead}},
			middleware.PublicRoute{Path: "/api/v1/server/settings", Methods: []string{http.MethodGet, http.MethodHead}},
		)
	}
	if onboardingEnabled {
		routes = append(routes,
			middleware.PublicRoute{Path: "/v1/server/admin/session", Methods: []string{http.MethodPost}},
			middleware.PublicRoute{Path: "/api/v1/server/admin/session", Methods: []string{http.MethodPost}},
		)
	}
	return routes
}

func serverBootstrapPaths() []string {
	if !envBoolDefault(config.ServerOperatorUIPublicEnv, true) {
		return nil
	}
	return []string{"/setup", "/setup/"}
}

func serverAdminUIPaths() []string {
	return []string{"/setup", "/setup/"}
}

func serverBootstrapAuthRoutes() []middleware.PublicRoute {
	if !envBoolDefault(config.ServerOperatorUIPublicEnv, true) {
		return nil
	}
	return []middleware.PublicRoute{
		{Path: "/v1/server/settings", Methods: []string{http.MethodGet, http.MethodHead, http.MethodPatch}},
		{Path: "/api/v1/server/settings", Methods: []string{http.MethodGet, http.MethodHead, http.MethodPatch}},
	}
}

func registerTestUI(app *App) {
	if app == nil || app.Mux == nil {
		return
	}
	// Operators can disable the onboarding/smoke UI entirely with
	// SPEECHKIT_SERVER_ONBOARDING_UI=false. The default is on.
	if !envBoolDefault(config.ServerOnboardingUIEnv, true) {
		return
	}
	app.Mux.Handle("/", testUIHandler{app: app})
	setup := setupUIHandler{app: app}
	app.Mux.Handle("/setup", setup)
	app.Mux.Handle("/setup/", setup)
	session := adminSessionHandler{app: app}
	app.Mux.Handle("/v1/server/admin/session", session)
	app.Mux.Handle("/api/v1/server/admin/session", session)
}

type testUIHandler struct {
	app *App
}

func (h testUIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	smokeToken := ""
	if h.app != nil && h.app.AuthState != nil {
		smokeToken = h.app.AuthState.SmokeToken()
	}
	writeHTML(w, r, onboarding.TestUIHTML(smokeToken))
}

type setupUIHandler struct {
	app *App
}

func (h setupUIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/setup" && r.URL.Path != "/setup/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// After bootstrap, setup remains available as an authenticated admin UI.
	// The Auth middleware blocks anonymous browser traffic before this handler.
	if h.app != nil && serverAdminConfigured(h.app) && h.app.bootstrapSealed.Load() && !serverAdminIdentity(r) {
		http.Error(w, "admin required", http.StatusForbidden)
		return
	}

	writeHTML(w, r, onboarding.SetupUIHTML())
}

func serverAdminIdentity(r *http.Request) bool {
	return middleware.IdentityFromContext(r.Context()).Role == "admin"
}

type adminSessionHandler struct {
	app *App
}

func (h adminSessionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/server/admin/session" && r.URL.Path != "/api/v1/server/admin/session" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_login_payload"})
		return
	}
	username := ""
	passwordHash := ""
	if h.app == nil || h.app.Cfg == nil || !h.app.Cfg.Server.AdminAuthEnabled {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "admin_auth_disabled"})
		return
	}
	if h.app.AuthState != nil {
		username = h.app.AuthState.AdminUsername()
		passwordHash = h.app.AuthState.AdminPasswordHash()
	} else {
		username = h.app.Cfg.Server.AdminUsername
		passwordHash = h.app.Cfg.Server.AdminPasswordHash
	}
	if !middleware.AdminPasswordMatches(username, passwordHash, strings.TrimSpace(payload.Username), payload.Password) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_admin_credentials"})
		return
	}
	cookie, err := middleware.NewAdminSessionCookie(username, passwordHash, serverRequestIsSecure(h.app, r), time.Now())
	if err != nil || cookie == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "admin_session_failed"})
		return
	}
	http.SetCookie(w, cookie)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func serverRequestIsSecure(app *App, r *http.Request) bool {
	if r == nil {
		return false
	}
	var cidrs []string
	if app != nil && app.Cfg != nil {
		cidrs = app.Cfg.Server.TrustedProxyCIDRs
	}
	trustedProxies, _ := httpx.NewTrustedProxies(cidrs)
	return trustedProxies.RequestIsHTTPS(r)
}

func writeHTML(w http.ResponseWriter, r *http.Request, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", inlineHTMLCSP(html))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, html)
}

func inlineHTMLCSP(html string) string {
	return strings.Join([]string{
		"default-src 'none'",
		"style-src " + cspSources(cspBlockHashes(html, "<style>", "</style>")),
		"script-src " + cspSources(cspBlockHashes(html, "<script>", "</script>")),
		"img-src 'self'",
		"connect-src 'self' ws: wss:",
		"object-src 'none'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"form-action 'self'",
	}, "; ")
}

func cspSources(hashes []string) string {
	if len(hashes) == 0 {
		return "'none'"
	}
	return strings.Join(hashes, " ")
}

func cspBlockHashes(html, openTag, closeTag string) []string {
	var hashes []string
	for {
		start := strings.Index(html, openTag)
		if start < 0 {
			return hashes
		}
		html = html[start+len(openTag):]
		end := strings.Index(html, closeTag)
		if end < 0 {
			return hashes
		}
		hashes = append(hashes, cspSourceHash(html[:end]))
		html = html[end+len(closeTag):]
	}
}

func cspSourceHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}
