package onboarding

import (
	"strings"
	"testing"
)

func TestTestUIHTMLContainsSmokeMarkers(t *testing.T) {
	body := TestUIHTML("")
	for _, want := range []string{
		"SpeechKit Server Smoke",
		`id="runtimeStatus"`,
		`id="sttProvider"`,
		`id="llmProvider"`,
		`id="voiceProvider"`,
		`id="refreshSettings"`,
		`id="runSmoke"`,
		`id="settingsStatus"`,
		`data-ui-surface="speechkit-server-smoke"`,
		`class="topbar-actions"`,
		`class="nav-link" href="/setup"`,
		"updateRuntimePanel",
		`fetch(path, opts)`,
		"/v1/server/settings",
		"/api/v1/dictation/transcribe",
		"/api/v1/assist/process",
		"/api/v1/voiceagent/sessions",
		// Smoke token plumbing: the page must read the token from a
		// server-rendered meta tag and apply it as a Bearer header.
		`meta name="speechkit-smoke-token"`,
		"smokeBearerToken",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("test UI body should contain %q", want)
		}
	}

	for _, forbidden := range []string{
		"Base URL",
		"baseUrl",
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
		// No operator-facing token input fields — token comes from the
		// server-rendered meta tag, never from a form on this page.
		"Bearer Token",
		`id="bearerToken"`,
		`id="serverToken"`,
		`name="bearerToken"`,
		`<link`,
		`rel="stylesheet"`,
		`@import`,
		`src="`,
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

func TestTestUIHTMLSmokeTokenSentinelSurvivesRewrite(t *testing.T) {
	// Regression: a naïve strings.ReplaceAll over the whole HTML rewrites
	// every occurrence of the placeholder — including any JS guard that
	// detects "still the placeholder". If the JS guard is collapsed by
	// the same rewrite, the smoke flow silently disables itself even
	// when a real token is configured.
	//
	// Production observed on v0.35.3: meta tag carried the token, but
	// smokeBearerToken() returned "" because the literal guard had been
	// rewritten to `value === "<real-token>"` (true → return "").
	const realToken = "smoke-regression-token-9aFp42xQ-not-a-real-secret"
	body := TestUIHTML(realToken)

	// The token MUST land in the meta tag (otherwise the page can't auth).
	if !strings.Contains(body, `content="`+realToken+`"`) {
		t.Fatalf("rendered meta tag must carry the real token; want content=%q in body", realToken)
	}

	// The guard inside smokeBearerToken() must NOT compare against the
	// real token. The literal placeholder string `__SPEECHKIT_SMOKE_TOKEN__`
	// either survives by runtime concatenation OR has been removed entirely
	// — but a `=== "<real-token>"` comparison means the rewrite collapsed
	// the guard and the smoke flow is dead.
	forbidden := `=== "` + realToken + `"`
	if strings.Contains(body, forbidden) {
		t.Fatalf("smokeBearerToken() guard was rewritten by ReplaceAll into a self-comparison\n"+
			"this disables smoke auth at runtime; the JS guard must build its sentinel by\n"+
			"runtime concatenation so the Go-side rewrite cannot match it\n"+
			"forbidden substring: %q", forbidden)
	}
}

func TestTestUIHTMLEmptyTokenLeavesPlaceholderInert(t *testing.T) {
	body := TestUIHTML("")
	// Empty input leaves an empty meta content. The page must still render.
	if !strings.Contains(body, `content=""`) {
		t.Fatalf("empty smoke token should render an empty meta content attribute")
	}
	if !strings.Contains(body, "smokeBearerToken") {
		t.Fatalf("smokeBearerToken function must remain present when token is empty")
	}
}

func TestSetupUIHTMLContainsOnboardingMarkers(t *testing.T) {
	body := SetupUIHTML()
	for _, want := range []string{
		"SpeechKit Server Setup",
		`id="setupWizard"`,
		`id="setupHeading"`,
		`id="setupSubtitle"`,
		`data-ui-surface="speechkit-server-setup"`,
		`class="topbar-actions"`,
		`class="nav-link" href="/"`,
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
		`id="serverTokenManaged"`,
		`id="serverTokenEnv"`,
		`id="serverTokenState"`,
		`id="serverTokenOutput"`,
		`id="generatedServerToken"`,
		`id="copyGeneratedServerToken"`,
		`id="reviewServerAuth"`,
		`id="setupBack"`,
		`id="setupNext"`,
		`id="onboardingPanel"`,
		`id="providerMatrix"`,
		`id="dictationProfile"`,
		`id="assistProfile"`,
		`id="voiceAgentProfile"`,
		"ggml-org/gemma-4-E2B-it-GGUF:Q8_0",
		`id="saveModelSettings"`,
		`type="button">Save Settings</button>`,
		"SpeechKit Server Settings",
		"applySettingsToForm",
		"renderSettingsPanels",
		"renderSetupMode",
		"applyModeOptionsToForm",
		"applyServerAuthToForm",
		"serverAuthPayload",
		"renderGeneratedServerToken",
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
		"speechkit.admin.basic",
		"sessionStorage",
		"localStorage",
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
		`<link`,
		`rel="stylesheet"`,
		`@import`,
		`src="`,
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
