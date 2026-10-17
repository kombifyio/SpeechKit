//go:build linux

package core

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/kombifyio/SpeechKit/app/internal/config"
	"github.com/kombifyio/SpeechKit/app/internal/server/configapi"
	"github.com/kombifyio/SpeechKit/app/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/app/internal/server/onboarding"
)

// The hosted preview exercises customer handoff and the real configuration
// read. It branches before telemetry, providers, storage, jobs and mode wiring;
// admitting an HTTP read alone would not prevent those startup effects.
type previewRuntime struct {
	revision string
	subjects map[string]bool
}

func resolvePreviewRuntime(cfg *config.Config) (*previewRuntime, error) {
	channel := strings.TrimSpace(os.Getenv("KOMBIFY_RELEASE_CHANNEL"))
	if channel == "" || channel == "stable" {
		return nil, nil
	}
	if channel != "preview" {
		return nil, fmt.Errorf("core.Run: unknown release channel")
	}
	if os.Getenv("KOMBIFY_EDITION") != "saas-standalone" || cfg.Server.AuthMode != "oidc" {
		return nil, fmt.Errorf("core.Run: preview requires SaaS edition and OIDC authentication")
	}
	revision := strings.TrimSpace(os.Getenv("RENDER_GIT_COMMIT"))
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(revision) {
		return nil, fmt.Errorf("core.Run: preview requires its native source revision")
	}
	subjects := map[string]bool{}
	for _, subject := range strings.Split(os.Getenv("KOMBIFY_PREVIEW_ADMITTED_SUBJECTS"), ",") {
		if subject = strings.TrimSpace(subject); subject != "" {
			subjects[subject] = true
		}
	}
	if len(subjects) == 0 || subjects["*"] {
		return nil, fmt.Errorf("core.Run: preview requires explicit shared test subjects")
	}
	if cfg.Server.AdminAuthEnabled || cfg.Server.Discovery.Enabled {
		return nil, fmt.Errorf("core.Run: preview prohibits operator authentication and discovery")
	}
	if _, err := serverOIDCVerifier(cfg); err != nil {
		return nil, fmt.Errorf("core.Run: preview OIDC: %w", err)
	}
	return &previewRuntime{revision: revision, subjects: subjects}, nil
}

func previewExecutionScope() map[string]string {
	return map[string]string{"http": "shared-test-identities", "startup": "verify-only", "scheduled": "disabled", "queues": "disabled"}
}

func runPreview(ctx context.Context, cfg *config.Config, opts RunOptions, preview *previewRuntime) error {
	app := newServerApp(cfg, opts)
	app.Preview = preview
	app.Modes = map[Mode]bool{}
	registerHealth(app)
	app.Mux.HandleFunc(onboarding.DeviceAddPath, onboarding.DeviceAddHandler)
	configapi.New(cfg, app.Version, func() string { return string(StatusOK) }).Mount(app.Mux)
	registerAPIAlias(app.Mux)
	app.Health.SetReady("server", StatusOK, "customer handoff and authenticated configuration read only")
	return serveServer(ctx, cfg, app)
}

func previewPublicPath(route string) bool {
	return route == "/healthz" || route == onboarding.DeviceAddPath || route == "/api"+onboarding.DeviceAddPath
}

func (p *previewRuntime) transport(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := r.URL.Path
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.RawPath != "" || r.URL.EscapedPath() != route || path.Clean(route) != route || strings.Contains(route, `\`) ||
			r.Header.Get("Upgrade") != "" || strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream") {
			http.Error(w, "preview_transport_not_admitted", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(route, "/v1/internal/") || strings.HasPrefix(route, "/api/v1/internal/") {
			http.Error(w, "preview_route_not_admitted", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "preview_mutation_not_admitted", http.StatusConflict)
			return
		}
		if !previewPublicPath(route) && route != "/v1/config" && route != "/api/v1/config" {
			http.Error(w, "preview_route_not_admitted", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (p *previewRuntime) principal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !previewPublicPath(r.URL.Path) {
			id := middleware.IdentityFromContext(r.Context())
			if id.Source != "oidc" || !p.subjects[id.UserID] {
				http.Error(w, "preview_subject_not_admitted", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
