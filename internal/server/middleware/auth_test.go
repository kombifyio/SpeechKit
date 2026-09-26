//go:build linux

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuth_PublicPathBypassesCheck(t *testing.T) {
	t.Setenv("TEST_BEARER", "correct-horse-battery-staple")
	handler := Auth(AuthOptions{
		Mode:             "bearer",
		BearerTokenEnv:   "TEST_BEARER",
		AllowPublicPaths: []string{"/healthz"},
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("healthz should bypass auth; got %d", rec.Code)
	}
}

func TestAuth_BootstrapRouteBypassesOnlyWhenAllowed(t *testing.T) {
	bootstrapAllowed := true
	handler := Auth(AuthOptions{
		Mode: "bearer",
		AllowBootstrapRoutes: []PublicRoute{
			{Path: "/v1/server/settings", Methods: []string{http.MethodPatch}},
		},
		BootstrapAllowed: func(*http.Request) bool { return bootstrapAllowed },
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	bootstrapReq := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	bootstrapRec := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapRec, bootstrapReq)
	if bootstrapRec.Code != http.StatusOK {
		t.Fatalf("bootstrap settings write should bypass auth, got %d", bootstrapRec.Code)
	}

	bootstrapAllowed = false
	rejectedReq := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	rejectedRec := httptest.NewRecorder()
	handler.ServeHTTP(rejectedRec, rejectedReq)
	if rejectedRec.Code != http.StatusUnauthorized {
		t.Fatalf("bootstrap settings write should require auth after bootstrap, got %d", rejectedRec.Code)
	}
}

func TestAuth_BootstrapPathBypassesOnlyWhenAllowed(t *testing.T) {
	bootstrapAllowed := true
	handler := Auth(AuthOptions{
		Mode:                "bearer",
		AllowBootstrapPaths: []string{"/setup"},
		BootstrapAllowed:    func(*http.Request) bool { return bootstrapAllowed },
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	bootstrapReq := httptest.NewRequest(http.MethodGet, "/setup", nil)
	bootstrapRec := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapRec, bootstrapReq)
	if bootstrapRec.Code != http.StatusOK {
		t.Fatalf("bootstrap setup path should bypass auth, got %d", bootstrapRec.Code)
	}

	bootstrapAllowed = false
	rejectedReq := httptest.NewRequest(http.MethodGet, "/setup", nil)
	rejectedRec := httptest.NewRecorder()
	handler.ServeHTTP(rejectedRec, rejectedReq)
	if rejectedRec.Code != http.StatusUnauthorized {
		t.Fatalf("bootstrap setup path should require auth after bootstrap, got %d", rejectedRec.Code)
	}
}

func TestAuth_PublicRouteBypassesOnlyConfiguredMethods(t *testing.T) {
	t.Setenv("TEST_BEARER", "correct-horse-battery-staple")
	handler := Auth(AuthOptions{
		Mode:           "bearer",
		BearerTokenEnv: "TEST_BEARER",
		AllowPublicRoutes: []PublicRoute{
			{Path: "/v1/server/settings", Methods: []string{http.MethodGet, http.MethodHead}},
		},
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	readReq := httptest.NewRequest(http.MethodGet, "/v1/server/settings", nil)
	readRec := httptest.NewRecorder()
	handler.ServeHTTP(readRec, readReq)
	if readRec.Code != http.StatusOK {
		t.Fatalf("settings GET should bypass auth; got %d", readRec.Code)
	}

	writeReq := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	writeRec := httptest.NewRecorder()
	handler.ServeHTTP(writeRec, writeReq)
	if writeRec.Code != http.StatusUnauthorized {
		t.Fatalf("settings PATCH without auth should be rejected; got %d", writeRec.Code)
	}

	authorizedWrite := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	authorizedWrite.Header.Set("Authorization", "Bearer correct-horse-battery-staple")
	authorizedRec := httptest.NewRecorder()
	handler.ServeHTTP(authorizedRec, authorizedWrite)
	if authorizedRec.Code != http.StatusOK {
		t.Fatalf("settings PATCH with auth should pass; got %d", authorizedRec.Code)
	}
}

func TestAuth_PublicRouteCanMatchPrefixAndSuffix(t *testing.T) {
	t.Setenv("TEST_BEARER", "correct-horse-battery-staple")
	handler := Auth(AuthOptions{
		Mode:           "bearer",
		BearerTokenEnv: "TEST_BEARER",
		AllowPublicRoutes: []PublicRoute{
			{PathPrefix: "/v1/voiceagent/sessions/", PathSuffix: "/ws", Methods: []string{http.MethodGet}},
		},
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	wsReq := httptest.NewRequest(http.MethodGet, "/v1/voiceagent/sessions/session-1/ws?ticket=t", nil)
	wsRec := httptest.NewRecorder()
	handler.ServeHTTP(wsRec, wsReq)
	if wsRec.Code != http.StatusOK {
		t.Fatalf("ticket websocket route should bypass auth; got %d", wsRec.Code)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/v1/voiceagent/sessions/session-1/ws?ticket=t", nil)
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong method should require auth; got %d", postRec.Code)
	}

	transcriptReq := httptest.NewRequest(http.MethodGet, "/v1/voiceagent/sessions/session-1/transcript", nil)
	transcriptRec := httptest.NewRecorder()
	handler.ServeHTTP(transcriptRec, transcriptReq)
	if transcriptRec.Code != http.StatusUnauthorized {
		t.Fatalf("other session routes should require auth; got %d", transcriptRec.Code)
	}
}
