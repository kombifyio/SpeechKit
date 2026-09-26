//go:build linux

package core

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

func TestDeploymentStatusReportsHeadlessEnvWithoutSecretValues(t *testing.T) {
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "super-secret-bearer")
	t.Setenv("GOOGLE_AI_API_KEY", "super-secret-gemini")

	cfg := &config.Config{}
	cfg.Server.AuthMode = "bearer"
	cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"
	cfg.Providers.Google.APIKeyEnv = "GOOGLE_AI_API_KEY"
	app := &App{
		Cfg:     cfg,
		Mux:     http.NewServeMux(),
		Health:  NewHealthRegistry(),
		Version: "test-version",
		Modes: map[Mode]bool{
			ModeDictation:  true,
			ModeAssist:     true,
			ModeVoiceAgent: true,
		},
	}
	app.Health.SetReady("server", StatusOK, "listening")
	registerDeploymentStatus(app)
	registerAPIAlias(app.Mux)

	handler := middleware.Auth(middleware.AuthOptions{
		Mode:           "bearer",
		BearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
	})(app.Mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/deployment/status", nil)
	req.Header.Set("Authorization", "Bearer super-secret-bearer")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/deployment/status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leaked := range []string{"super-secret-bearer", "super-secret-gemini"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("deployment status leaked secret %q in body=%s", leaked, body)
		}
	}
	var payload struct {
		Version string `json:"version"`
		Auth    struct {
			Mode           string `json:"mode"`
			BearerTokenEnv string `json:"bearer_token_env"`
			BearerTokenSet bool   `json:"bearer_token_set"`
		} `json:"auth"`
		Providers struct {
			Google struct {
				APIKey struct {
					Env        string `json:"env"`
					Configured bool   `json:"configured"`
				} `json:"api_key"`
			} `json:"google"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode deployment status: %v", err)
	}
	if payload.Version != "test-version" {
		t.Fatalf("version = %q, want test-version", payload.Version)
	}
	if payload.Auth.Mode != "bearer" || payload.Auth.BearerTokenEnv != "SPEECHKIT_SERVER_TOKEN" || !payload.Auth.BearerTokenSet {
		t.Fatalf("auth status = %+v", payload.Auth)
	}
	if payload.Providers.Google.APIKey.Env != "GOOGLE_AI_API_KEY" || !payload.Providers.Google.APIKey.Configured {
		t.Fatalf("google api key status = %+v", payload.Providers.Google.APIKey)
	}
}

func TestDeploymentStatusRequiresAuth(t *testing.T) {
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "super-secret-bearer")

	cfg := &config.Config{}
	cfg.Server.AuthMode = "bearer"
	cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"
	app := &App{Cfg: cfg, Mux: http.NewServeMux(), Health: NewHealthRegistry()}
	registerDeploymentStatus(app)

	handler := middleware.Auth(middleware.AuthOptions{
		Mode:           "bearer",
		BearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
	})(app.Mux)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/deployment/status", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated deployment status = %d, want 401", rec.Code)
	}
}

func TestDeploymentStatusRejectsNonAdminEdgeIdentity(t *testing.T) {
	t.Setenv("EDGE_AUTH_SECRET", "edge-secret")

	cfg := &config.Config{}
	cfg.Server.AuthMode = "edge_hmac"
	cfg.Server.EdgeAuthSecretEnv = "EDGE_AUTH_SECRET"
	app := &App{Cfg: cfg, Mux: http.NewServeMux(), Health: NewHealthRegistry()}
	registerDeploymentStatus(app)

	handler := middleware.Auth(middleware.AuthOptions{
		Mode:          "edge_hmac",
		EdgeSecretEnv: "EDGE_AUTH_SECRET",
	})(app.Mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/deployment/status", nil)
	setEdgeAuth(req, "user-1", "org-1", "pro", "")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin edge deployment status = %d body=%s, want 403", rec.Code, rec.Body.String())
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/v1/deployment/status", nil)
	setEdgeAuth(adminReq, "admin-1", "org-1", "pro", "admin")
	adminRec := httptest.NewRecorder()
	handler.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("admin edge deployment status = %d body=%s, want 200", adminRec.Code, adminRec.Body.String())
	}
}

func setEdgeAuth(req *http.Request, userID, orgID, plan, role string) {
	req.Header.Set("X-Edge-User-Id", userID)
	req.Header.Set("X-Edge-Org-Id", orgID)
	req.Header.Set("X-Edge-Plan", plan)
	req.Header.Set("X-Edge-Role", role)
	mac := hmac.New(sha256.New, []byte("edge-secret"))
	mac.Write([]byte(userID))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(orgID))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(plan))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(role))
	req.Header.Set("X-Edge-Auth-Hmac", hex.EncodeToString(mac.Sum(nil)))
}
