package voice_companion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

func TestWikipediaSkill_IntentIsWikipedia(t *testing.T) {
	if NewWikipediaSkill().Intent() != shortcuts.IntentWikipedia {
		t.Errorf("expected IntentWikipedia")
	}
}

func TestWikipediaLanguage_DEEN(t *testing.T) {
	if got := wikipediaLanguage("de"); got != "de" {
		t.Errorf("DE = %q", got)
	}
	if got := wikipediaLanguage("en"); got != "en" {
		t.Errorf("EN = %q", got)
	}
	if got := wikipediaLanguage("fr"); got != "en" {
		t.Errorf("FR fallback = %q, want en", got)
	}
}

func TestCondenseExtract_TrimsToThreeSentences(t *testing.T) {
	in := "A. B. C. D. E."
	out := condenseExtract(in)
	if out != "A. B. C." {
		t.Errorf("got %q, want 'A. B. C.'", out)
	}
}

func TestCondenseExtract_HandlesEmptyInput(t *testing.T) {
	if out := condenseExtract(""); out != "" {
		t.Errorf("expected empty, got %q", out)
	}
}

func TestCondenseExtract_TruncatesLongText(t *testing.T) {
	long := strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit. ", 50)
	out := condenseExtract(long)
	if len(out) > 601 {
		t.Errorf("expected <= 600 chars + ellipsis, got %d", len(out))
	}
}

// fakeWikipedia returns an httptest server that mimics Wikipedia's
// summary endpoint. The endpointFmt string passed into the skill is
// the same format used by the production endpoint, so substituting the
// hostname with the test server URL is enough.
func fakeWikipedia(t *testing.T, status int, payload any) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if payload != nil {
			_ = json.NewEncoder(w).Encode(payload)
		}
	}))
	// endpoint format: %s = lang, %s = topic. Map both to the test
	// server URL, the handler doesn't care about path semantics.
	endpointFmt := srv.URL + "/%s/%s"
	return srv, endpointFmt
}

func TestWikipediaSkill_E2E_EN(t *testing.T) {
	srv, endpointFmt := fakeWikipedia(t, http.StatusOK, map[string]any{
		"title":   "Linux",
		"extract": "Linux is a family of open-source Unix-like operating systems based on the Linux kernel. The kernel was first released in 1991. Linux is widely used on servers and embedded devices.",
		"type":    "standard",
	})
	defer srv.Close()

	skill := NewWikipediaSkill().WithEndpointFormat(endpointFmt)
	got, err := skill.Execute(context.Background(), assist.ToolCall{Payload: "Linux", Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got.Text, "Linux is a family") {
		t.Errorf("Text doesn't start with extract, got %q", got.Text)
	}
	if got.Action != "respond" {
		t.Errorf("Action = %q, want respond", got.Action)
	}
}

func TestWikipediaSkill_DisambiguationIsSilent(t *testing.T) {
	srv, endpointFmt := fakeWikipedia(t, http.StatusOK, map[string]any{
		"title":   "Mercury",
		"extract": "Mercury may refer to: ...",
		"type":    "disambiguation",
	})
	defer srv.Close()

	skill := NewWikipediaSkill().WithEndpointFormat(endpointFmt)
	got, _ := skill.Execute(context.Background(), assist.ToolCall{Payload: "Mercury", Locale: "en"})
	if got.Action != "silent" {
		t.Errorf("disambiguation should be silent, got Action=%q", got.Action)
	}
}

func TestWikipediaSkill_404_GracefulMessage(t *testing.T) {
	srv, endpointFmt := fakeWikipedia(t, http.StatusNotFound, nil)
	defer srv.Close()

	skill := NewWikipediaSkill().WithEndpointFormat(endpointFmt)
	got, err := skill.Execute(context.Background(), assist.ToolCall{Payload: "definitelynotanarticle", Locale: "de"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Action != "respond" {
		t.Errorf("404 should respond gracefully, got %q", got.Action)
	}
	if !strings.Contains(got.Text, "definitelynotanarticle") {
		t.Errorf("expected topic in not-found message, got %q", got.Text)
	}
}

func TestWikipediaSkill_500_PropagatesAsError(t *testing.T) {
	srv, endpointFmt := fakeWikipedia(t, http.StatusInternalServerError, nil)
	defer srv.Close()

	skill := NewWikipediaSkill().WithEndpointFormat(endpointFmt)
	_, err := skill.Execute(context.Background(), assist.ToolCall{Payload: "Linux", Locale: "en"})
	if err == nil {
		t.Fatal("expected 500 to propagate as error")
	}
}

func TestWikipediaSkill_EmptyPayloadIsSilent(t *testing.T) {
	skill := NewWikipediaSkill()
	got, _ := skill.Execute(context.Background(), assist.ToolCall{Locale: "en"})
	if got.Action != "silent" {
		t.Errorf("empty payload should be silent, got %q", got.Action)
	}
}

func TestWikipediaSkill_URLEscapesTopic(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// EscapedPath preserves the wire-form encoding; URL.Path
		// would have already decoded "%20" back to a space.
		captured = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"extract": "ok.", "type": "standard"})
	}))
	defer srv.Close()
	skill := NewWikipediaSkill().WithEndpointFormat(srv.URL + "/%s/%s")
	_, _ = skill.Execute(context.Background(), assist.ToolCall{Payload: "Albert Einstein", Locale: "de"})
	want := "Albert%20Einstein"
	if !strings.Contains(captured, want) {
		t.Errorf("expected URL to include %q, got %q", want, captured)
	}
	_ = fmt.Sprint // ensure fmt is referenced if test trimming removed earlier code
}
