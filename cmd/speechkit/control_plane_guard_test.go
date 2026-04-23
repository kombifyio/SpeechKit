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
	return newControlPlaneGuardTestHandlerWithState(t, &appState{})
}

func newControlPlaneGuardTestHandlerWithState(t *testing.T, state *appState) http.Handler {
	t.Helper()
	cfg := defaultTestConfig()
	return assetHandler(
		cfg,
		filepath.Join(t.TempDir(), "config.toml"),
		state,
		&router.Router{},
		nil,
		&config.InstallState{Mode: config.InstallModeLocal},
	)
}

func TestControlPlaneGuardRejectsCrossSiteMutatingRequests(t *testing.T) {
	handler := newControlPlaneGuardTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", http.NoBody)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestControlPlaneGuardRejectsUnknownOriginOnMutatingRequests(t *testing.T) {
	handler := newControlPlaneGuardTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", http.NoBody)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestControlPlaneGuardAllowsLocalhostOriginOnMutatingRequests(t *testing.T) {
	handler := newControlPlaneGuardTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", http.NoBody)
	req.Header.Set("Origin", "http://localhost")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestControlPlaneGuardRejectsMissingSessionTokenWhenConfigured(t *testing.T) {
	handler := newControlPlaneGuardTestHandlerWithState(t, &appState{controlPlaneToken: "test-token"})
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestControlPlaneGuardRejectsSessionCookieWithoutHeaderWhenConfigured(t *testing.T) {
	handler := newControlPlaneGuardTestHandlerWithState(t, &appState{controlPlaneToken: "test-token"})
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", http.NoBody)
	req.AddCookie(&http.Cookie{Name: controlPlaneTokenCookieName, Value: "test-token"})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestControlPlaneGuardAllowsSessionHeaderWhenConfigured(t *testing.T) {
	handler := newControlPlaneGuardTestHandlerWithState(t, &appState{controlPlaneToken: "test-token"})
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", http.NoBody)
	req.Header.Set(controlPlaneTokenHeaderName, "test-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestControlPlaneGuardSetsSessionCookieOnReadRequest(t *testing.T) {
	handler := newControlPlaneGuardTestHandlerWithState(t, &appState{controlPlaneToken: "test-token"})
	req := httptest.NewRequest(http.MethodGet, "/app/version", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != controlPlaneTokenCookieName || cookie.Value != "test-token" {
		t.Fatalf("cookie = %s=%q, want %s=%q", cookie.Name, cookie.Value, controlPlaneTokenCookieName, "test-token")
	}
	if !cookie.HttpOnly {
		t.Fatal("control-plane token cookie is not HttpOnly")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("SameSite = %v, want %v", cookie.SameSite, http.SameSiteStrictMode)
	}
	if got := rec.Header().Get(controlPlaneTokenHeaderName); got != "test-token" {
		t.Fatalf("%s = %q, want %q", controlPlaneTokenHeaderName, got, "test-token")
	}
}

func TestControlPlaneTokenEndpointBootstrapsHeader(t *testing.T) {
	handler := newControlPlaneGuardTestHandlerWithState(t, &appState{controlPlaneToken: "test-token"})
	req := httptest.NewRequest(http.MethodGet, "/app/control-token", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get(controlPlaneTokenHeaderName); got != "test-token" {
		t.Fatalf("%s = %q, want %q", controlPlaneTokenHeaderName, got, "test-token")
	}
}

func TestControlPlaneGuardDoesNotBlockGetRoutes(t *testing.T) {
	handler := newControlPlaneGuardTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/app/version", http.NoBody)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
