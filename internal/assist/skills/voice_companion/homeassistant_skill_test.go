package voice_companion

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

func TestHomeAssistantSkill_IntentIsHomeAssistant(t *testing.T) {
	if NewHomeAssistantSkill("", "").Intent() != shortcuts.IntentHomeAssistant {
		t.Errorf("expected IntentHomeAssistant")
	}
}

func TestHomeAssistantSkill_ConfiguredFlag(t *testing.T) {
	if NewHomeAssistantSkill("", "").Configured() {
		t.Error("empty URL+token should be Configured=false")
	}
	if NewHomeAssistantSkill("https://ha.test", "").Configured() {
		t.Error("missing token should be Configured=false")
	}
	if NewHomeAssistantSkill("", "tok").Configured() {
		t.Error("missing URL should be Configured=false")
	}
	if !NewHomeAssistantSkill("https://ha.test", "tok").Configured() {
		t.Error("valid local-DNS HTTPS URL+token should be Configured=true")
	}
}

func TestHomeAssistantSkill_LocalOnlyURLPolicy(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr error
	}{
		{name: "loopback HTTP", url: "http://127.0.0.1:8123"},
		{name: "localhost HTTP", url: "http://localhost:8123"},
		{name: "private HTTPS", url: "https://192.168.1.20:8123"},
		{name: "local DNS HTTPS", url: "https://ha.home.arpa:8123"},
		{name: "private HTTP rejected", url: "http://192.168.1.20:8123", wantErr: netsec.ErrInsecureHTTP},
		{name: "DNS HTTP rejected", url: "http://ha.home.arpa:8123", wantErr: netsec.ErrInsecureHTTP},
		{name: "public literal rejected", url: "https://8.8.8.8", wantErr: netsec.ErrPublicBlocked},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			skill := NewHomeAssistantSkill(tc.url, "tok")
			if tc.wantErr == nil {
				if !skill.Configured() {
					t.Fatalf("expected URL to be accepted, got %v", skill.configErr)
				}
				return
			}
			if skill.Configured() {
				t.Fatal("expected URL to be rejected")
			}
			if !errors.Is(skill.configErr, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, skill.configErr)
			}
		})
	}
}

func TestHomeAssistantSkill_UsesNoProxyAndRejectsRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	skill := NewHomeAssistantSkill(source.URL, "tok")
	rt, ok := skill.client.Transport.(*netsec.RedactingRoundTripper)
	if !ok {
		t.Fatalf("transport = %T, want netsec.RedactingRoundTripper", skill.client.Transport)
	}
	transport, ok := rt.Base.(*http.Transport)
	if !ok {
		t.Fatalf("inner transport = %T, want *http.Transport", rt.Base)
	}
	if transport.Proxy != nil {
		t.Fatal("Home Assistant transport must ignore environment proxies")
	}

	got, err := skill.Execute(context.Background(), assist.ToolCall{Transcript: "turn on the light", Locale: "en"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Action == "silent" || got.Text == "" {
		t.Fatalf("redirect failure must be terminal: %#v", got)
	}
	if redirected.Load() != 0 {
		t.Fatalf("redirect target received %d requests", redirected.Load())
	}
}

func TestHomeAssistantSkill_Probe_EmptyConfig(t *testing.T) {
	if err := NewHomeAssistantSkill("", "").Probe(context.Background()); err == nil {
		t.Error("Probe should fail with empty URL")
	}
	if err := NewHomeAssistantSkill("https://ha.test", "").Probe(context.Background()); err == nil {
		t.Error("Probe should fail with empty token")
	}
}

func TestHomeAssistantSkill_Probe(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "200 OK", status: http.StatusOK},
		{name: "401 Unauthorized", status: http.StatusUnauthorized, wantErr: true},
		{name: "403 Forbidden", status: http.StatusForbidden, wantErr: true},
		{name: "500 Internal", status: http.StatusInternalServerError, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakeHA(t, tc.status, map[string]string{"message": "do-not-return-this-body"}, func(t *testing.T, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("Probe should GET, got %s", r.Method)
				}
				if r.URL.Path != "/api/" {
					t.Errorf("Probe path = %q, want /api/", r.URL.Path)
				}
				if got, want := r.Header.Get("Authorization"), "Bearer tok"; got != want {
					t.Errorf("Authorization = %q, want %q", got, want)
				}
			})
			defer srv.Close()
			err := NewHomeAssistantSkill(srv.URL, "tok").Probe(context.Background())
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for status %d", tc.status)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected nil error for status %d, got %v", tc.status, err)
			}
			if err != nil && strings.Contains(err.Error(), "do-not-return-this-body") {
				t.Fatalf("Probe leaked Home Assistant body: %v", err)
			}
		})
	}
}

func TestHomeAssistantSkill_NoConfig_IsTerminal(t *testing.T) {
	got, err := NewHomeAssistantSkill("", "").Execute(context.Background(), assist.ToolCall{
		Transcript: "turn on the kitchen light",
		Locale:     "en",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertTerminalHAResult(t, got)
	if !strings.Contains(got.Text, "not configured") {
		t.Fatalf("Text = %q, want safe configuration guidance", got.Text)
	}
}

func TestHomeAssistantSkill_EmptyTranscriptAndPayload_IsTerminal(t *testing.T) {
	got, err := NewHomeAssistantSkill("http://127.0.0.1:1", "tok").Execute(context.Background(), assist.ToolCall{Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertTerminalHAResult(t, got)
}

func TestHomeAssistantFailureMessagesUseRegisteredCatalogs(t *testing.T) {
	reasons := []struct {
		reason string
		id     localization.MessageID
	}{
		{reason: "not_configured", id: localization.CompanionHomeAssistantNotConfigured},
		{reason: "empty_command", id: localization.CompanionHomeAssistantCommandEmpty},
		{reason: "not_matched", id: localization.CompanionHomeAssistantNotMatched},
		{reason: "rejected", id: localization.CompanionHomeAssistantRejected},
		{reason: "invalid_response", id: localization.CompanionHomeAssistantUnavailable},
	}
	for _, locale := range localization.SupportedLocales() {
		for _, tc := range reasons {
			t.Run(locale+"/"+tc.reason, func(t *testing.T) {
				got := homeAssistantFailureResult(locale, tc.reason)
				if got.MessageID != tc.id || got.ReasonCode != tc.reason {
					t.Fatalf("metadata = %q/%q, want %q/%q", got.MessageID, got.ReasonCode, tc.id, tc.reason)
				}
				want := localization.Text(locale, tc.id)
				if got.Text != want || got.SpeakText != want {
					t.Fatalf("localized result = %q/%q, want %q", got.Text, got.SpeakText, want)
				}
				assertTerminalHAResult(t, got)
			})
		}
	}
}

func TestHomeAssistantFailureUsesNegotiatedLocale(t *testing.T) {
	got := homeAssistantFailureResult("es-MX", "not_configured")
	if got.Locale != "es" || got.Text != localization.Text("es", got.MessageID) {
		t.Fatalf("regional failure result = %#v", got)
	}
	got = homeAssistantFailureResult("zh-Latn", "not_configured")
	if got.Locale != "en" || got.Text != localization.Text("en", got.MessageID) {
		t.Fatalf("unsupported-script failure result = %#v", got)
	}
}

func fakeHA(t *testing.T, status int, payload any, headerCheck func(*testing.T, *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if headerCheck != nil {
			headerCheck(t, r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if payload != nil {
			_ = json.NewEncoder(w).Encode(payload)
		}
	}))
}

func TestHomeAssistantSkill_MatchedAction(t *testing.T) {
	srv := fakeHA(t, http.StatusOK, map[string]any{
		"response": map[string]any{
			"speech":        map[string]any{"plain": map[string]any{"speech": "OK, kitchen light is on."}},
			"response_type": "action_done",
		},
	}, func(t *testing.T, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/conversation/process" {
			t.Errorf("path = %s, want /api/conversation/process", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", auth)
		}
	})
	defer srv.Close()

	got, err := NewHomeAssistantSkill(srv.URL, "test-token").Execute(context.Background(), assist.ToolCall{
		Transcript: "turn on the kitchen light",
		Locale:     "en",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Text != "OK, kitchen light is on." || got.Action != "execute" {
		t.Fatalf("result = %#v", got)
	}
	if got.MessageID != "" || got.ReasonCode != "" {
		t.Fatalf("imported Home Assistant speech acquired local failure metadata: %#v", got)
	}
}

func TestHomeAssistantSkillForwardsSupportedLanguageAndPreservesRawResultLocale(t *testing.T) {
	tests := []struct {
		locale       string
		wantLanguage string
	}{
		{locale: "en-US", wantLanguage: "en"},
		{locale: "de-DE", wantLanguage: "de"},
		{locale: "es-MX", wantLanguage: "es"},
		{locale: "zh-Hans-CN", wantLanguage: "zh-Hans"},
		{locale: "hi-IN", wantLanguage: "hi"},
		{locale: "ar-EG", wantLanguage: "ar"},
	}
	for _, tc := range tests {
		t.Run(tc.locale, func(t *testing.T) {
			var language string
			srv := fakeHA(t, http.StatusOK, map[string]any{
				"response": map[string]any{
					"speech":        map[string]any{"plain": map[string]any{"speech": "provider-owned speech"}},
					"response_type": "action_done",
				},
			}, func(t *testing.T, r *http.Request) {
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				language = body["language"]
			})
			defer srv.Close()

			got, err := NewHomeAssistantSkill(srv.URL, "test-token").Execute(context.Background(), assist.ToolCall{
				Transcript: "host-authorized command",
				Locale:     tc.locale,
			})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if language != tc.wantLanguage {
				t.Fatalf("HA language = %q, want %q", language, tc.wantLanguage)
			}
			if got.Text != "provider-owned speech" || got.Locale != tc.locale || got.MessageID != "" || got.ReasonCode != "" {
				t.Fatalf("imported HA result = %#v", got)
			}
		})
	}
}

func TestHomeAssistantSkill_NoIntentMatched_IsTerminal(t *testing.T) {
	srv := fakeHA(t, http.StatusOK, map[string]any{
		"response": map[string]any{
			"speech":        map[string]any{"plain": map[string]any{"speech": "Sorry, I couldn't understand that."}},
			"response_type": "error",
			"data":          map[string]any{"code": "no_intent_match"},
		},
	}, nil)
	defer srv.Close()

	got, err := NewHomeAssistantSkill(srv.URL, "test-token").Execute(context.Background(), assist.ToolCall{
		Transcript: "turn on something unknown",
		Locale:     "en",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertTerminalHAResult(t, got)
	if got.Text != "Sorry, I couldn't understand that." {
		t.Fatalf("Text = %q, want Home Assistant's terminal response", got.Text)
	}
}

func TestHomeAssistantSkill_FailuresAreTerminalAndDoNotLeakBodies(t *testing.T) {
	for _, tc := range []struct {
		status int
		reason string
	}{
		{status: http.StatusUnauthorized, reason: "authentication_failed"},
		{status: http.StatusInternalServerError, reason: "unexpected_status"},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := fakeHA(t, tc.status, map[string]string{"secret": "raw-ha-body-secret"}, nil)
			defer srv.Close()
			got, err := NewHomeAssistantSkill(srv.URL, "tok").Execute(context.Background(), assist.ToolCall{
				Transcript: "turn on the light",
				Locale:     "en",
			})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			assertTerminalHAResult(t, got)
			if strings.Contains(got.Text, "raw-ha-body-secret") {
				t.Fatalf("result leaked Home Assistant body: %q", got.Text)
			}
			if got.MessageID != localization.CompanionHomeAssistantUnavailable || got.ReasonCode != tc.reason {
				t.Fatalf("failure metadata = %q/%q, want unavailable/%q", got.MessageID, got.ReasonCode, tc.reason)
			}
		})
	}
}

func TestHomeAssistantSkill_OversizedOrMalformedResponseIsTerminal(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{not-json`},
		{name: "oversized", body: strings.Repeat("x", maxHomeAssistantResponseBytes+1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			got, err := NewHomeAssistantSkill(srv.URL, "tok").Execute(context.Background(), assist.ToolCall{
				Transcript: "turn on the light",
				Locale:     "en",
			})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			assertTerminalHAResult(t, got)
		})
	}
}

func TestAllSkills_AlwaysIncludesHABoundary(t *testing.T) {
	for _, tc := range []struct {
		name, url, token string
	}{
		{name: "unconfigured"},
		{name: "configured", url: "https://ha.test", token: "tok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found := false
			for _, skill := range AllSkills(tc.url, tc.token) {
				if skill.Intent() == shortcuts.IntentHomeAssistant {
					found = true
				}
			}
			if !found {
				t.Fatal("AllSkills must include the fail-closed Home Assistant boundary")
			}
		})
	}
}

func assertTerminalHAResult(t *testing.T, got assist.ToolResult) {
	t.Helper()
	if got.Action == "silent" || strings.TrimSpace(got.Text) == "" || strings.TrimSpace(got.SpeakText) == "" {
		t.Fatalf("Home Assistant result is not terminal: %#v", got)
	}
	if got.Surface != assist.ResultSurfaceActionAck || got.Kind == "" {
		t.Fatalf("Home Assistant result lacks structured classification: %#v", got)
	}
}
