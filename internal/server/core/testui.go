//go:build linux

package core

import (
	"io"
	"net/http"

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
	if h.app != nil && h.app.bootstrapSealed.Load() && !serverAdminIdentity(r) {
		http.Error(w, "admin required", http.StatusForbidden)
		return
	}

	writeHTML(w, r, onboarding.SetupUIHTML())
}

func serverAdminIdentity(r *http.Request) bool {
	return middleware.IdentityFromContext(r.Context()).Role == "admin"
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
