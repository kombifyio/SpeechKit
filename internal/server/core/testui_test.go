//go:build linux

package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterTestUI_ServesModeTester(t *testing.T) {
	app := &App{
		Mux:     http.NewServeMux(),
		Version: "test-version",
		Modes: map[Mode]bool{
			ModeDictation:  true,
			ModeAssist:     true,
			ModeVoiceAgent: true,
		},
	}

	registerTestUI(app)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	app.Mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("expected text/html content type, got %q", got)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"SpeechKit Server Smoke",
		`id="runtimeStatus"`,
		`id="sttProvider"`,
		`id="llmProvider"`,
		`id="voiceProvider"`,
		`id="refreshSettings"`,
		`id="runSmoke"`,
		`id="settingsStatus"`,
		"updateRuntimePanel",
		`fetch(path, opts)`,
		"/v1/server/settings",
		"/api/v1/dictation/transcribe",
		"/api/v1/assist/process",
		"/api/v1/voiceagent/sessions",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("test UI body should contain %q", want)
		}
	}

	for _, forbidden := range []string{
		"Base URL",
		"baseUrl",
		"Authorization",
		"Bearer Token",
		"Token",
		"token",
		"bearerToken",
		"X-Edge-Auth-Hmac",
		"edgeHmac",
		"authMode",
		"onboardingPanel",
		"providerMatrix",
		"dictationProfile",
		"assistProfile",
		"voiceAgentProfile",
		"saveModelSettings",
		"applySettingsToForm",
		"saveServerSettings",
		"smokePrompt",
		"Voice Agent prompt template",
		"system_prompt_override",
		"API Key",
		"dictModel",
		"agentModel",
		"Persona ID",
		"Role ID",
		"Sequence ID",
		"TTS Voice",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("test UI body should not contain client setup field %q", forbidden)
		}
	}
	for _, german := range []string{
		"Bereit",
		"wartet",
		"Aktuelle Konfiguration", //nolint:misspell // Intentional German copy checked for absence.
		"Aktualisieren",
		"Modell",
		"Daten",
		"Prueft",
		"Leer lassen",
		"Laeuft",
		"Fehler",
		"Gruen",
		`lang="de"`,
	} {
		if strings.Contains(body, german) {
			t.Fatalf("smoke UI body should use English copy, found %q", german)
		}
	}
}

func TestRegisterTestUI_ServesSetupOnly(t *testing.T) {
	app := &App{Mux: http.NewServeMux()}
	registerTestUI(app)

	rec := httptest.NewRecorder()
	app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /setup should return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("GET /setup should return text/html content type, got %q", got)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"SpeechKit Server Setup",
		`id="setupWizard"`,
		`id="setupHeading"`,
		`id="setupSubtitle"`,
		`id="settingsPanel"`,
		`id="dictationKind"`,
		`id="assistKind"`,
		`id="voiceAgentKind"`,
		`id="dictationDictionary"`,
		`id="assistToolList"`,
		`id="voiceAgentPromptTemplate"`,
		`data-step-panel="welcome"`,
		`data-step-panel="models"`,
		`data-step-panel="credentials"`,
		`data-step-panel="review"`,
		`id="setupBack"`,
		`id="setupNext"`,
		`id="onboardingPanel"`,
		`id="providerMatrix"`,
		`id="dictationProfile"`,
		`id="assistProfile"`,
		`id="voiceAgentProfile"`,
		"ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M",
		`id="saveModelSettings"`,
		`type="button">Save Settings</button>`,
		"SpeechKit Server Settings",
		"applySettingsToForm",
		"renderSettingsPanels",
		"renderSetupMode",
		"applyModeOptionsToForm",
		"selectedAssistToolIDs",
		"saveServerSettings",
		`byId("saveModelSettings").addEventListener("click"`,
		"loadServerSettings({ preserveStatus: true })",
		"/v1/server/settings",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("setup UI body should contain %q", want)
		}
	}
	for _, forbidden := range []string{
		"SpeechKit Server Smoke",
		"Smoke-Test starten",
		`id="runSmoke"`,
		`id="smokePrompt"`,
		`id="healthStatus"`,
		`id="readyStatus"`,
		`id="dictationStatus"`,
		`id="assistStatus"`,
		`id="voiceagentStatus"`,
		"/api/v1/dictation/transcribe",
		"/api/v1/assist/process",
		"/api/v1/voiceagent/sessions",
		"checkDictation",
		"checkAssist",
		"checkVoiceAgent",
		"system_prompt_override",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("setup UI body should not contain smoke-test field %q", forbidden)
		}
	}
	for _, german := range []string{
		"Bereit",
		"wartet",
		"Aktuelle Konfiguration", //nolint:misspell // Intentional German copy checked for absence.
		"Aktualisieren",
		"Modell",
		"Daten",
		"Provider-Typ",
		"leer lassen",
		"unveraendert",
		"Defaults einsetzen",
		"Settings speichern",
		"Speichert",
		"Gespeichert",
		"Fehler",
		"Laedt",
		"Lokale",
		"abgeschlossen",
		"offen",
		`lang="de"`,
	} {
		if strings.Contains(body, german) {
			t.Fatalf("setup UI body should use English copy, found %q", german)
		}
	}
}

func TestRegisterTestUI_RejectsExtraUIPaths(t *testing.T) {
	app := &App{Mux: http.NewServeMux()}
	registerTestUI(app)

	for _, path := range []string{"/test-ui", "/test-ui/", "/admin", "/admin/"} {
		rec := httptest.NewRecorder()
		app.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s should return 404, got %d", path, rec.Code)
		}
	}
}

func TestRegisterTestUI_AllowsHEADAndRejectsOtherMethods(t *testing.T) {
	app := &App{Mux: http.NewServeMux()}
	registerTestUI(app)

	head := httptest.NewRecorder()
	app.Mux.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/", nil))
	if head.Code != http.StatusOK {
		t.Fatalf("HEAD / should return 200, got %d", head.Code)
	}

	setupHead := httptest.NewRecorder()
	app.Mux.ServeHTTP(setupHead, httptest.NewRequest(http.MethodHead, "/setup", nil))
	if setupHead.Code != http.StatusOK {
		t.Fatalf("HEAD /setup should return 200, got %d", setupHead.Code)
	}

	post := httptest.NewRecorder()
	app.Mux.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/setup", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /setup should return 405, got %d", post.Code)
	}
	if got := post.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("unexpected Allow header %q", got)
	}
}

func TestServerPublicPaths_IncludeOnlyAlwaysPublicUIPaths(t *testing.T) {
	paths := serverPublicPaths()
	for _, want := range []string{"/", "/healthz", "/readyz", "/setup", "/setup/"} {
		found := false
		for _, got := range paths {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("serverPublicPaths should include %q, got %#v", want, paths)
		}
	}
	for _, forbidden := range []string{"/test-ui", "/test-ui/", "/admin", "/admin/", "/v1/server/settings", "/api/v1/server/settings"} {
		for _, got := range paths {
			if got == forbidden {
				t.Fatalf("serverPublicPaths should not include extra UI path %q, got %#v", forbidden, paths)
			}
		}
	}
}

func TestServerPublicRoutes_ExposeSettingsReadOnly(t *testing.T) {
	routes := serverPublicRoutes()
	wants := map[string]map[string]bool{
		"/v1/server/settings":     {http.MethodGet: false, http.MethodHead: false},
		"/api/v1/server/settings": {http.MethodGet: false, http.MethodHead: false},
	}

	for _, route := range routes {
		methods, ok := wants[route.Path]
		if !ok {
			t.Fatalf("unexpected public route %q", route.Path)
		}
		for _, method := range route.Methods {
			if _, ok := methods[method]; !ok {
				t.Fatalf("unexpected method %q for public route %q", method, route.Path)
			}
			methods[method] = true
		}
	}
	for path, methods := range wants {
		for method := range methods {
			if !methods[method] {
				t.Fatalf("%s should be public for %s", method, path)
			}
		}
		if methods[http.MethodPatch] {
			t.Fatalf("%s must not be public for %s", http.MethodPatch, path)
		}
	}
}

func TestRegisterAPIAlias_RewritesAPIV1ToLegacyV1(t *testing.T) {
	mux := http.NewServeMux()
	registerAPIAlias(mux)

	var gotPath string
	var gotPrefix string
	mux.HandleFunc("/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotPrefix = r.Header.Get("X-SpeechKit-API-Prefix")
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected alias to route to /v1 handler, got %d", rec.Code)
	}
	if gotPath != "/v1/ping" {
		t.Fatalf("alias target path = %q, want /v1/ping", gotPath)
	}
	if gotPrefix != "/api" {
		t.Fatalf("api prefix header = %q, want /api", gotPrefix)
	}
}
