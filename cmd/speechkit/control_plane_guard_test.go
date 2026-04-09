package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
)

func newControlPlaneGuardTestHandler(t *testing.T) http.Handler {
	t.Helper()
	cfg := defaultTestConfig()
	return assetHandler(
		cfg,
		filepath.Join(t.TempDir(), "config.toml"),
		&appState{},
		&router.Router{},
		nil,
		&config.InstallState{Mode: config.InstallModeLocal},
	)
}

func TestControlPlaneGuardRejectsCrossSiteMutatingRequests(t *testing.T) {
	handler := newControlPlaneGuardTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestControlPlaneGuardRejectsUnknownOriginOnMutatingRequests(t *testing.T) {
	handler := newControlPlaneGuardTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestControlPlaneGuardAllowsLocalhostOriginOnMutatingRequests(t *testing.T) {
	handler := newControlPlaneGuardTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Origin", "http://localhost")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestControlPlaneGuardDoesNotBlockGetRoutes(t *testing.T) {
	handler := newControlPlaneGuardTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/app/version", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
