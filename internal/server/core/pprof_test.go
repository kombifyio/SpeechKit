//go:build linux

package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func pprofTestApp(listenAddr string, enabled, public bool) *App {
	cfg := &config.Config{}
	cfg.Server.ListenAddr = listenAddr
	cfg.Server.Debug.PprofEnabled = enabled
	cfg.Server.Debug.PprofPublic = public
	return &App{Cfg: cfg, Mux: http.NewServeMux()}
}

func pprofRegistered(app *App) bool {
	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil))
	return rec.Code != http.StatusNotFound
}

func TestRegisterPprof_DisabledByDefault(t *testing.T) {
	app := pprofTestApp("127.0.0.1:8080", false, false)
	registerPprof(app)
	if pprofRegistered(app) {
		t.Fatal("pprof must not be registered when pprof_enabled is false")
	}
}

func TestRegisterPprof_EnabledOnLoopback(t *testing.T) {
	app := pprofTestApp("127.0.0.1:8080", true, false)
	registerPprof(app)
	if !pprofRegistered(app) {
		t.Fatal("pprof should be registered when enabled on a loopback listener")
	}
}

func TestRegisterPprof_RefusedOnNonLoopbackWithoutPublic(t *testing.T) {
	app := pprofTestApp("0.0.0.0:8080", true, false)
	registerPprof(app)
	if pprofRegistered(app) {
		t.Fatal("pprof must be refused on a non-loopback listener unless pprof_public is set")
	}
}

func TestRegisterPprof_AllowedOnNonLoopbackWithPublic(t *testing.T) {
	app := pprofTestApp("0.0.0.0:8080", true, true)
	registerPprof(app)
	if !pprofRegistered(app) {
		t.Fatal("pprof should be registered on a non-loopback listener when pprof_public=true")
	}
}
