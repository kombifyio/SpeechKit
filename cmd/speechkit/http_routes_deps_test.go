package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
)

func TestNewControlPlaneHandlerRegistersAPIV1RoutesFromDeps(t *testing.T) {
	cfg := defaultTestConfig()
	state := newInitialAppState(cfg)
	handler := newControlPlaneHandler(controlPlaneDeps{
		Config:       cfg,
		ConfigPath:   filepath.Join(t.TempDir(), "config.toml"),
		State:        state,
		STTRouter:    &router.Router{},
		InstallState: &config.InstallState{Mode: config.InstallModeLocal},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/modes", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"contracts"`) {
		t.Fatalf("api v1 modes payload = %q", rec.Body.String())
	}
}
