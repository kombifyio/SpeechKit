//go:build windows && cgo

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	speechkit "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
)

func TestHABridgeClaimsNamedToolCallWhenUnconfigured(t *testing.T) {
	bridge := newHABridge(&Config{})
	call, matched, err := bridge.MatchTool(context.Background(), speechkit.AssistRequest{
		Text:   "turn on the kitchen light",
		Locale: "en",
	})
	if err != nil || !matched {
		t.Fatalf("MatchTool matched=%v err=%v", matched, err)
	}
	result, err := bridge.ExecuteTool(context.Background(), call)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	assertTerminalBoxHAResult(t, result.Text, string(result.Surface))
	if !strings.Contains(result.Text, "not configured") {
		t.Fatalf("result = %q, want safe configuration denial", result.Text)
	}
}

func TestBoxHomeAssistantEmergencyFallbackUsesRegisteredCatalogs(t *testing.T) {
	for _, locale := range localization.SupportedLocales() {
		t.Run(locale, func(t *testing.T) {
			got := boxHomeAssistantUnavailable(locale)
			if got.MessageID != localization.CompanionHomeAssistantUnavailable || got.ReasonCode != "unavailable" {
				t.Fatalf("metadata = %q/%q", got.MessageID, got.ReasonCode)
			}
			want := localization.Text(locale, localization.CompanionHomeAssistantUnavailable)
			if got.Text != want || got.SpeakText != want {
				t.Fatalf("localized fallback = %q/%q, want %q", got.Text, got.SpeakText, want)
			}
		})
	}
	got := boxHomeAssistantUnavailable("es-MX")
	if got.Locale != "es" || got.Text != localization.Text("es", got.MessageID) {
		t.Fatalf("regional fallback = %#v", got)
	}
}

func TestHABridgeNoMatchAndFailureStayTerminal(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		payload string
	}{
		{
			name:   "no intent match",
			status: http.StatusOK,
			payload: `{"response":{"speech":{"plain":{"speech":"Home Assistant could not match that target."}},` +
				`"response_type":"error","error":{"code":"no_intent_match"}}}`,
		},
		{
			name:    "server failure",
			status:  http.StatusInternalServerError,
			payload: `{"secret":"raw-ha-body-must-not-escape"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/conversation/process" {
					t.Errorf("path = %q", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.payload))
			}))
			defer ha.Close()

			cfg := configuredHAConfig(t, ha.URL)
			bridge := newHABridge(cfg)
			// A named function call need not repeat a natural-language trigger.
			call, matched, err := bridge.MatchTool(context.Background(), speechkit.AssistRequest{
				Text:   "kitchen ceiling light",
				Locale: "en",
			})
			if err != nil || !matched {
				t.Fatalf("MatchTool matched=%v err=%v", matched, err)
			}
			result, err := bridge.ExecuteTool(context.Background(), call)
			if err != nil {
				t.Fatalf("ExecuteTool: %v", err)
			}
			assertTerminalBoxHAResult(t, result.Text, string(result.Surface))
			if strings.Contains(result.Text, "raw-ha-body-must-not-escape") {
				t.Fatalf("raw Home Assistant body escaped: %q", result.Text)
			}
		})
	}
}

func TestHABridgePreservesLanguageOverride(t *testing.T) {
	var language string
	ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		language = body["language"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"speech":{"plain":{"speech":"Erledigt."}},"response_type":"action_done"}}`))
	}))
	defer ha.Close()

	cfg := configuredHAConfig(t, ha.URL)
	cfg.HomeAssistant.Language = "de-DE"
	bridge := newHABridge(cfg)
	call, _, _ := bridge.MatchTool(context.Background(), speechkit.AssistRequest{Text: "turn on", Locale: "en-US"})
	if _, err := bridge.ExecuteTool(context.Background(), call); err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if language != "de" {
		t.Fatalf("Home Assistant language = %q, want de", language)
	}
}

func TestHABridgeDoesNotFollowRedirects(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetCalls.Add(1)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	bridge := newHABridge(configuredHAConfig(t, source.URL))
	call, _, _ := bridge.MatchTool(context.Background(), speechkit.AssistRequest{Text: "turn on", Locale: "en"})
	result, err := bridge.ExecuteTool(context.Background(), call)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	assertTerminalBoxHAResult(t, result.Text, string(result.Surface))
	if targetCalls.Load() != 0 {
		t.Fatalf("redirect target received %d calls", targetCalls.Load())
	}
}

func TestHABridgeLocalURLPolicy(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "loopback HTTP", url: "http://127.0.0.1:8123", want: true},
		{name: "localhost HTTP", url: "http://localhost:8123", want: true},
		{name: "private HTTPS", url: "https://192.168.1.20:8123", want: true},
		{name: "private HTTP", url: "http://192.168.1.20:8123"},
		{name: "DNS HTTP", url: "http://homeassistant.home.arpa:8123"},
		{name: "public literal", url: "https://8.8.8.8"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := validLocalHomeAssistantConfig(tc.url, "token"); got != tc.want {
				t.Fatalf("validLocalHomeAssistantConfig(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestVoiceAgentAlwaysRegistersFailClosedHATool(t *testing.T) {
	registry := agentkit.NewRegistry()
	if _, err := registerHomeAssistantTool(registry, &Config{}, "kbx:test"); err != nil {
		t.Fatalf("registerHomeAssistantTool: %v", err)
	}
	tool, ok := registry.Lookup(intentHomeAssistant)
	if !ok {
		t.Fatal("Home Assistant authority tool was omitted while unconfigured")
	}
	out, err := tool.Invoke(context.Background(), map[string]any{
		"query":  "turn on the kitchen light",
		"locale": "en",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out["matched"] != true || strings.TrimSpace(out["text"].(string)) == "" {
		t.Fatalf("tool output = %#v, want terminal matched result", out)
	}
}

func TestHABridgeClassificationUsesDeterministicCatalog(t *testing.T) {
	bridge := newHABridge(&Config{})
	tests := []struct {
		name   string
		text   string
		locale string
		want   bool
	}{
		{name: "English HA command", text: "please turn on the kitchen light", locale: "en-US", want: true},
		{name: "German HA command", text: "bitte schalte das Küchenlicht an", locale: "de-DE", want: true},
		{name: "general conversation", text: "explain how photosynthesis works", locale: "en-US"},
		{name: "unsupported implication", text: "make it cozy", locale: "en-US"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bridge.classifiesTranscript(tc.text, tc.locale)
			if err != nil {
				t.Fatalf("classifiesTranscript: %v", err)
			}
			if got != tc.want {
				t.Fatalf("classifiesTranscript(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestVoiceAgentPromptPinsHomeAssistantAuthority(t *testing.T) {
	got := withHomeAssistantAuthority("Keep replies concise.")
	if !strings.Contains(got, "Keep replies concise.") ||
		!strings.Contains(got, "deterministic smart-home command policy") ||
		!strings.Contains(got, "MUST call the home_assistant tool") {
		t.Fatalf("authority prompt = %q", got)
	}
}

func configuredHAConfig(t *testing.T, baseURL string) *Config {
	t.Helper()
	const tokenEnv = "SPEECHKIT_BOX_HA_TEST_TOKEN"
	t.Setenv(tokenEnv, "test-token")
	cfg := &Config{}
	cfg.HomeAssistant.BaseURL = baseURL
	cfg.HomeAssistant.TokenEnv = tokenEnv
	return cfg
}

func assertTerminalBoxHAResult(t *testing.T, text, surface string) {
	t.Helper()
	if strings.TrimSpace(text) == "" || surface == "silent" {
		t.Fatalf("Home Assistant result is not terminal: text=%q surface=%q", text, surface)
	}
}
