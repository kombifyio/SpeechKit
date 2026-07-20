//go:build linux

package voiceagent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/server/wssession"
)

func TestAdapterApplyVoicePrefDefaults(t *testing.T) {
	providers := map[string]ProviderFactory{
		"gemini":     staticProviderFactory{provider: newFakeProvider()},
		"assemblyai": staticProviderFactory{provider: newFakeProvider()},
	}

	tests := []struct {
		name                string
		prefs               wssession.VoicePrefs
		start               StartFrame
		wantProvider        string
		wantPersona         string
		wantPersonaFromPref bool
	}{
		{
			name:                "pref fills omitted provider and persona",
			prefs:               wssession.VoicePrefs{VAProvider: "assemblyai", VAPersona: "concise-de"},
			wantProvider:        "assemblyai",
			wantPersona:         "concise-de",
			wantPersonaFromPref: true,
		},
		{
			name:         "explicit start values always win",
			prefs:        wssession.VoicePrefs{VAProvider: "assemblyai", VAPersona: "concise-de"},
			start:        StartFrame{Provider: "gemini", PersonaID: "sales"},
			wantProvider: "gemini",
			wantPersona:  "sales",
		},
		{
			name:                "pref provider alias is normalized",
			prefs:               wssession.VoicePrefs{VAProvider: "assembly-ai", VAPersona: "concise-de"},
			wantProvider:        "assemblyai",
			wantPersona:         "concise-de",
			wantPersonaFromPref: true,
		},
		{
			name:         "unconfigured pref provider falls back to server default",
			prefs:        wssession.VoicePrefs{VAProvider: "openai"},
			wantProvider: "", // selectProvider("") applies DefaultProvider later
		},
		{
			name: "no prefs leave the frame untouched",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &Adapter{
				Session:         &ManagedSession{ID: "s1", VoicePrefs: tt.prefs},
				Providers:       providers,
				DefaultProvider: "gemini",
			}
			start := tt.start
			personaFromPref := adapter.applyVoicePrefDefaults(&start)
			if start.Provider != tt.wantProvider {
				t.Fatalf("start.Provider = %q, want %q", start.Provider, tt.wantProvider)
			}
			if start.PersonaID != tt.wantPersona {
				t.Fatalf("start.PersonaID = %q, want %q", start.PersonaID, tt.wantPersona)
			}
			if personaFromPref != tt.wantPersonaFromPref {
				t.Fatalf("personaFromPref = %v, want %v", personaFromPref, tt.wantPersonaFromPref)
			}
		})
	}
}

func TestAdapterApplyVoicePrefDefaultsKeepsFrameWhenProviderPreInjected(t *testing.T) {
	adapter := &Adapter{
		Session:  &ManagedSession{ID: "s1", VoicePrefs: wssession.VoicePrefs{VAProvider: "assemblyai"}},
		Provider: newFakeProvider(), // tests pre-inject; production selection skipped
	}
	start := StartFrame{}
	adapter.applyVoicePrefDefaults(&start)
	if start.Provider != "" {
		t.Fatalf("start.Provider = %q, want empty when a provider is pre-injected", start.Provider)
	}
}

// selectivePersonaResolver resolves only the personas it knows, mimicking a
// persona registry rejecting an unknown persona_id.
type selectivePersonaResolver struct {
	known map[string]bool
}

func (r *selectivePersonaResolver) Resolve(start StartFrame) (LiveConfigFrame, error) {
	if start.PersonaID != "" && !r.known[start.PersonaID] {
		return LiveConfigFrame{}, errors.New("unknown persona " + start.PersonaID)
	}
	return LiveConfigFrame{PersonaID: start.PersonaID}, nil
}

func TestAdapterResolvePersonaConfigFallsBackForPrefPersonaOnly(t *testing.T) {
	resolver := &selectivePersonaResolver{known: map[string]bool{"sales": true}}
	adapter := &Adapter{
		Session: &ManagedSession{ID: "s1"},
		Persona: resolver,
	}

	// A preference-sourced persona that does not resolve falls back to the
	// server default persona instead of failing the session.
	start := StartFrame{PersonaID: "ghost-persona"}
	cfg, err := adapter.resolvePersonaConfig(&start, true)
	if err != nil {
		t.Fatalf("pref persona must fall back, got error: %v", err)
	}
	if cfg.PersonaID != "" || start.PersonaID != "" {
		t.Fatalf("expected default persona fallback, got cfg=%q start=%q", cfg.PersonaID, start.PersonaID)
	}

	// The same unknown persona requested explicitly by the client still
	// errors — explicit requests are not silently rewritten.
	start = StartFrame{PersonaID: "ghost-persona"}
	if _, err := adapter.resolvePersonaConfig(&start, false); err == nil {
		t.Fatal("explicit unknown persona must keep erroring")
	}

	// A known persona resolves unchanged regardless of its source.
	start = StartFrame{PersonaID: "sales"}
	cfg, err = adapter.resolvePersonaConfig(&start, true)
	if err != nil {
		t.Fatalf("known persona: %v", err)
	}
	if cfg.PersonaID != "sales" {
		t.Fatalf("cfg.PersonaID = %q, want sales", cfg.PersonaID)
	}
}

func TestCreateSessionCapturesVoicePrefs(t *testing.T) {
	manager := mustManager(t, Options{})
	handler, err := New(HandlerOptions{
		Manager:  manager,
		Provider: staticProviderFactory{provider: newFakeProvider()},
		Persona:  &fakeResolver{},
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	prefs := middleware.VoicePrefs{VAProvider: "assemblyai", VAPersona: "concise-de"}
	inject := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(middleware.InjectVoicePrefsForTest(r.Context(), prefs)))
	})
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(inject)

	req := httptest.NewRequest(http.MethodPost, "https://speechkit.test/v1/voiceagent/sessions", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body createSessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	session, err := manager.Get(body.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	want := wssession.VoicePrefs{VAProvider: "assemblyai", VAPersona: "concise-de"}
	if session.VoicePrefs != want {
		t.Fatalf("session.VoicePrefs = %+v, want %+v", session.VoicePrefs, want)
	}
}
