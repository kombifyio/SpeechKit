package voice_companion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
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
		t.Error("URL+token should be Configured=true")
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

func TestHomeAssistantSkill_Probe_E2E(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{"200 OK", http.StatusOK, false},
		{"401 Unauthorized", http.StatusUnauthorized, true},
		{"403 Forbidden", http.StatusForbidden, true},
		{"500 Internal", http.StatusInternalServerError, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := fakeHA(t, c.status, map[string]string{"message": "ok"}, func(t *testing.T, r *http.Request) {
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
			if c.wantErr && err == nil {
				t.Errorf("expected error for status %d, got nil", c.status)
			}
			if !c.wantErr && err != nil {
				t.Errorf("expected nil error for status %d, got %v", c.status, err)
			}
		})
	}
}

func TestHomeAssistantSkill_NoConfig_IsSilent(t *testing.T) {
	skill := NewHomeAssistantSkill("", "")
	got, err := skill.Execute(context.Background(), assist.ToolCall{Transcript: "turn on the kitchen light", Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Action != "silent" {
		t.Errorf("expected silent without config, got %q", got.Action)
	}
}

func TestHomeAssistantSkill_EmptyTranscriptAndPayload_IsSilent(t *testing.T) {
	skill := NewHomeAssistantSkill("https://ha.test", "tok")
	got, err := skill.Execute(context.Background(), assist.ToolCall{Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Action != "silent" {
		t.Errorf("expected silent on empty input, got %q", got.Action)
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

func TestHomeAssistantSkill_E2E_MatchedAction(t *testing.T) {
	srv := fakeHA(t, http.StatusOK, map[string]any{
		"response": map[string]any{
			"speech": map[string]any{
				"plain": map[string]any{
					"speech": "OK, kitchen light is on.",
				},
			},
			"response_type": "action_done",
		},
	}, func(t *testing.T, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/conversation/process") {
			t.Errorf("path = %s, want .../api/conversation/process", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Authorization = %q, want 'Bearer test-token'", auth)
		}
	})
	defer srv.Close()

	skill := NewHomeAssistantSkill(srv.URL, "test-token")
	got, err := skill.Execute(context.Background(), assist.ToolCall{
		Transcript: "turn on the kitchen light",
		Locale:     "en",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Text != "OK, kitchen light is on." {
		t.Errorf("Text = %q", got.Text)
	}
	if got.Action != "execute" {
		t.Errorf("Action = %q, want execute", got.Action)
	}
}

func TestHomeAssistantSkill_NoIntentMatched_IsSilent(t *testing.T) {
	srv := fakeHA(t, http.StatusOK, map[string]any{
		"response": map[string]any{
			"speech": map[string]any{
				"plain": map[string]any{"speech": "Sorry, I couldn't understand that."},
			},
			"response_type": "error",
		},
	}, nil)
	defer srv.Close()

	skill := NewHomeAssistantSkill(srv.URL, "test-token")
	got, err := skill.Execute(context.Background(), assist.ToolCall{
		Transcript: "philosophical question",
		Locale:     "en",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Action != "silent" {
		t.Errorf("expected silent on no-intent, got %q", got.Action)
	}
}

func TestHomeAssistantSkill_Unauthorized_ReturnsError(t *testing.T) {
	srv := fakeHA(t, http.StatusUnauthorized, nil, nil)
	defer srv.Close()

	skill := NewHomeAssistantSkill(srv.URL, "bad-token")
	_, err := skill.Execute(context.Background(), assist.ToolCall{
		Transcript: "turn on the light",
		Locale:     "en",
	})
	if err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestHomeAssistantSkill_500_PropagatesAsError(t *testing.T) {
	srv := fakeHA(t, http.StatusInternalServerError, nil, nil)
	defer srv.Close()
	skill := NewHomeAssistantSkill(srv.URL, "tok")
	_, err := skill.Execute(context.Background(), assist.ToolCall{Transcript: "anything", Locale: "en"})
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestAllSkills_WithoutHA_ExcludesHA(t *testing.T) {
	skills := AllSkills("", "")
	for _, s := range skills {
		if s.Intent() == shortcuts.IntentHomeAssistant {
			t.Errorf("unconfigured HA should not be in AllSkills, got %v", s)
		}
	}
}

func TestAllSkills_WithHA_IncludesHA(t *testing.T) {
	skills := AllSkills("https://ha.test", "tok")
	found := false
	for _, s := range skills {
		if s.Intent() == shortcuts.IntentHomeAssistant {
			found = true
		}
	}
	if !found {
		t.Error("configured HA should be in AllSkills")
	}
}
