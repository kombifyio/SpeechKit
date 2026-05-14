//go:build linux

package core

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/server/onboarding"
)

func serverPublicPaths() []string {
	return []string{"/", "/healthz", "/readyz", "/readyz/strict"}
}

func serverPublicRoutes() []middleware.PublicRoute {
	return []middleware.PublicRoute{
		{Path: "/v1/server/settings", Methods: []string{http.MethodGet, http.MethodHead}},
		{Path: "/api/v1/server/settings", Methods: []string{http.MethodGet, http.MethodHead}},
		{Path: "/v1/server/admin/session", Methods: []string{http.MethodPost}},
		{Path: "/api/v1/server/admin/session", Methods: []string{http.MethodPost}},
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
	}
}

func serverBootstrapPaths() []string {
	return []string{"/setup", "/setup/"}
}

func serverAdminUIPaths() []string {
	return []string{"/setup", "/setup/"}
}

func serverBootstrapAuthRoutes() []middleware.PublicRoute {
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
	app.Mux.Handle("/", testUIHandler{})
	setup := setupUIHandler{app: app}
	app.Mux.Handle("/setup", setup)
	app.Mux.Handle("/setup/", setup)
	session := adminSessionHandler{app: app}
	app.Mux.Handle("/v1/server/admin/session", session)
	app.Mux.Handle("/api/v1/server/admin/session", session)
}

type testUIHandler struct{}

func (testUIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeHTML(w, r, onboarding.TestUIHTML())
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
	cookie, err := middleware.NewAdminSessionCookie(username, passwordHash, requestIsSecure(r), time.Now())
	if err != nil || cookie == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "admin_session_failed"})
		return
	}
	http.SetCookie(w, cookie)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func requestIsSecure(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func writeHTML(w http.ResponseWriter, r *http.Request, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, html)
}
