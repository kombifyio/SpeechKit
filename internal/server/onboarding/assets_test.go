package onboarding

import (
	"strings"
	"testing"
)

func TestTestUIHTMLContainsSmokeMarkers(t *testing.T) {
	body := TestUIHTML()
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

func TestSetupUIHTMLContainsOnboardingMarkers(t *testing.T) {
	body := SetupUIHTML()
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
		"ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M",
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
