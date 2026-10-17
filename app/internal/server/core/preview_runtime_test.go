//go:build linux

package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/app/internal/config"
	"github.com/kombifyio/SpeechKit/app/internal/server/configapi"
	"github.com/kombifyio/SpeechKit/app/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/app/internal/server/onboarding"
)

func TestPreviewRuntimeRequiresExplicitCustodyAndRevision(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.AuthMode = "oidc"
	cfg.Server.OIDC.JWKSURL = "https://issuer.example/.well-known/jwks.json"
	cfg.Server.OIDC.Issuer = "https://issuer.example/"
	cfg.Server.OIDC.Audience = "preview-api"
	t.Setenv("KOMBIFY_RELEASE_CHANNEL", "preview")
	t.Setenv("KOMBIFY_EDITION", "saas-standalone")
	t.Setenv("RENDER_GIT_COMMIT", strings.Repeat("a", 40))
	t.Setenv("KOMBIFY_PREVIEW_ADMITTED_SUBJECTS", "shared-subject")
	if _, err := resolvePreviewRuntime(cfg); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ key, value string }{
		{"KOMBIFY_RELEASE_CHANNEL", "nightly"},
		{"KOMBIFY_EDITION", "preview"},
		{"RENDER_GIT_COMMIT", "shortsha"},
		{"KOMBIFY_PREVIEW_ADMITTED_SUBJECTS", ""},
		{"KOMBIFY_PREVIEW_ADMITTED_SUBJECTS", "*"},
	} {
		t.Run(entry.key+entry.value, func(t *testing.T) {
			t.Setenv(entry.key, entry.value)
			if _, err := resolvePreviewRuntime(cfg); err == nil {
				t.Fatal("unsafe preview configuration accepted")
			}
		})
	}
	t.Setenv("KOMBIFY_RELEASE_CHANNEL", "stable")
	if preview, err := resolvePreviewRuntime(cfg); err != nil || preview != nil {
		t.Fatalf("stable bootstrap changed: %v %v", preview, err)
	}
}

func TestPreviewAdmissionProtectsActualHandlers(t *testing.T) {
	p := &previewRuntime{revision: strings.Repeat("a", 40), subjects: map[string]bool{"shared-subject": true}}
	cfg := &config.Config{}
	cfg.Server.AuthMode = "oidc"
	app := newServerApp(cfg, RunOptions{Version: "test"})
	app.Preview = p
	registerHealth(app)
	app.Mux.HandleFunc(onboarding.DeviceAddPath, onboarding.DeviceAddHandler)
	configapi.New(cfg, app.Version, func() string { return "ok" }).Mount(app.Mux)
	registerAPIAlias(app.Mux)
	// Authentication remains the existing Auth middleware. This finite verifier
	// fixture models its verified identity result; invalid credentials never reach
	// the subject guard or the real config handler.
	auth := middleware.Auth(middleware.AuthOptions{Mode: "oidc", OIDCVerifier: func(r *http.Request) (middleware.Identity, bool) {
		subject := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subject != "shared-subject" && subject != "other-subject" {
			return middleware.Identity{}, false
		}
		return middleware.Identity{UserID: subject, Source: "oidc"}, true
	}, AllowPublicPaths: []string{"/healthz"}, AllowPublicRoutes: serverPublicRoutes()})
	handler := p.transport(auth(p.principal(app.Mux)))
	for _, entry := range []struct {
		method, route, credential, accept, upgrade string
		status                                     int
	}{
		{method: "GET", route: "/healthz", status: 200},
		{method: "GET", route: onboarding.DeviceAddPath, status: 200},
		{method: "HEAD", route: "/api" + onboarding.DeviceAddPath, status: 200},
		{method: "GET", route: "/v1/config", status: 401},
		{method: "GET", route: "/v1/config", credential: "invalid", status: 401},
		{method: "GET", route: "/v1/config", credential: "other-subject", status: 403},
		{method: "GET", route: "/api/v1/config", credential: "shared-subject", status: 200},
		{method: "POST", route: "/v1/config", credential: "shared-subject", status: 409},
		{method: "GET", route: "/v1/internal/preview-admission-probe", status: 403},
		{method: "POST", route: "/v1/internal/preview-admission-probe", status: 403},
		{method: "GET", route: "/setup", status: 403},
		{method: "GET", route: "/v1/voiceagent/sessions", credential: "shared-subject", status: 403},
		{method: "GET", route: "/api/v1%2Fconfig", credential: "shared-subject", status: 403},
		{method: "GET", route: "/api/../v1/config", credential: "shared-subject", status: 403},
		{method: "GET", route: "/v1/config", credential: "shared-subject", accept: "text/event-stream", status: 403},
		{method: "GET", route: "/v1/config", credential: "shared-subject", upgrade: "websocket", status: 403},
	} {
		t.Run(entry.method+entry.route+entry.credential+entry.accept+entry.upgrade, func(t *testing.T) {
			r := httptest.NewRequest(entry.method, entry.route, nil).WithContext(context.Background())
			if entry.credential != "" {
				r.Header.Set("Authorization", "Bearer "+entry.credential)
			}
			r.Header.Set("Accept", entry.accept)
			r.Header.Set("Upgrade", entry.upgrade)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != entry.status {
				t.Fatalf("got %d, want %d: %s", w.Code, entry.status, w.Body.String())
			}
			if entry.route == "/api/v1/config" && entry.status == 200 {
				var body map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["auth_mode"] != "oidc" || body["version"] != "test" {
					t.Fatalf("real configuration read missing: %s", w.Body.String())
				}
			}
			if entry.route == "/healthz" && !strings.Contains(w.Body.String(), p.revision) {
				t.Fatal("health lacks exact source binding")
			}
		})
	}
}
