//go:build linux

package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func newAssistantUITestApp(t *testing.T) *App {
	t.Helper()
	app := &App{
		Cfg: &config.Config{},
		Mux: http.NewServeMux(),
	}
	registerAssistantUI(app)
	return app
}

func TestAssistantUIServesPage(t *testing.T) {
	app := newAssistantUITestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/assistant", nil)
	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assistant = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "speechkit-voice-assistant") {
		t.Errorf("page does not mount the kit element")
	}
	if strings.Contains(body, "__SPEECHKIT_SMOKE_TOKEN__") {
		t.Errorf("smoke-token placeholder leaked into the served page")
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'sha256-") {
		t.Errorf("CSP lacks a script hash: %s", csp)
	}
	if !strings.Contains(csp, "img-src 'self'") {
		t.Errorf("CSP lacks img-src for the mark assets: %s", csp)
	}
}

func TestAssistantUIServesMarkAssets(t *testing.T) {
	app := newAssistantUITestApp(t)

	for _, path := range []string{"/assistant/marks/rosette.png", "/assistant/marks/k.png"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		app.Mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "image/png" {
			t.Errorf("%s Content-Type = %q, want image/png", path, got)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s served an empty body", path)
		}
	}
}

func TestAssistantUIMethodGate(t *testing.T) {
	app := newAssistantUITestApp(t)

	req := httptest.NewRequest(http.MethodPost, "/assistant", nil)
	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /assistant = %d, want 405", rec.Code)
	}
}

func TestAssistantUIDisabledByEnv(t *testing.T) {
	t.Setenv(config.ServerAssistantUIEnv, "false")
	app := newAssistantUITestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/assistant", nil)
	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /assistant with %s=false = %d, want 404", config.ServerAssistantUIEnv, rec.Code)
	}
}
