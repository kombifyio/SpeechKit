package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientAddsBearerAndDecodesStatus(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/readyz" {
			t.Fatalf("path = %s, want /readyz", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "test"})
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL, Token: "secret"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	status, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Status != "ok" || status.Version != "test" {
		t.Fatalf("status = %+v", status)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestClientReturnsHTTPErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusTeapot)
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Status(context.Background())
	if err == nil {
		t.Fatal("Status succeeded, want error")
	}
	httpErr, ok := err.(HTTPError)
	if !ok {
		t.Fatalf("error type = %T, want HTTPError", err)
	}
	if httpErr.StatusCode != http.StatusTeapot || !strings.Contains(httpErr.Body, "nope") {
		t.Fatalf("HTTPError = %+v", httpErr)
	}
}

func TestClientTypedVocabularyAndTTSMethods(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.Method+" "+r.URL.RequestURI())
		switch r.URL.Path {
		case "/v1/vocabulary/dictionary":
			_ = json.NewEncoder(w).Encode(map[string]any{"entries": []map[string]any{{"spoken": "codex", "canonical": "Codex", "language": "en", "enabled": true}}})
		case "/v1/tts/synthesize":
			_ = json.NewEncoder(w).Encode(map[string]any{"audio_base64": "YXVkaW8=", "format": "mp3", "provider": "openai", "voice": "nova"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	entries, err := c.VocabularyEntries(context.Background(), "en")
	if err != nil {
		t.Fatalf("VocabularyEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Canonical != "Codex" {
		t.Fatalf("entries = %+v", entries)
	}
	tts, err := c.TTSSynthesize(context.Background(), TTSSynthesizeRequest{Text: "hello"})
	if err != nil {
		t.Fatalf("TTSSynthesize: %v", err)
	}
	if tts.Format != "mp3" || tts.Provider != "openai" {
		t.Fatalf("tts = %+v", tts)
	}
	want := []string{"GET /v1/vocabulary/dictionary?language=en", "POST /v1/tts/synthesize"}
	if strings.Join(gotPaths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths = %v, want %v", gotPaths, want)
	}
}

func TestClientEndpointMethodsUseExpectedRoutes(t *testing.T) {
	responses := map[string]any{
		"GET /v1/config":                                   map[string]any{"mode": "server"},
		"GET /v1/catalog/readiness":                        map[string]any{"readiness": []map[string]any{{"profile": "local"}}},
		"GET /v1/catalog/profiles/voice%20agent/readiness": map[string]any{"profile": "voice agent"},
		"GET /v1/catalog/profiles?mode=assist+mode":        map[string]any{"profiles": []map[string]any{{"id": "assist"}}},
		"GET /v1/catalog/contracts":                        map[string]any{"contracts": []map[string]any{{"mode": "dictation"}}},
		"GET /v1/personas":                                 map[string]any{"personas": []map[string]any{{"id": "persona"}}},
		"GET /v1/personas/p%201":                           map[string]any{"id": "p 1"},
		"POST /v1/personas":                                map[string]any{"id": "created-persona"},
		"PATCH /v1/personas/p%201":                         map[string]any{"id": "p 1", "updated": true},
		"DELETE /v1/personas/p%201":                        map[string]any{"deleted": true},
		"GET /v1/roles":                                    map[string]any{"roles": []map[string]any{{"id": "role"}}},
		"GET /v1/roles/r%201":                              map[string]any{"id": "r 1"},
		"POST /v1/roles":                                   map[string]any{"id": "created-role"},
		"PATCH /v1/roles/r%201":                            map[string]any{"id": "r 1", "updated": true},
		"DELETE /v1/roles/r%201":                           map[string]any{"deleted": true},
		"GET /v1/sequences":                                map[string]any{"sequences": []map[string]any{{"id": "sequence"}}},
		"GET /v1/sequences/s%201":                          map[string]any{"id": "s 1"},
		"POST /v1/sequences":                               map[string]any{"id": "created-sequence"},
		"PATCH /v1/sequences/s%201":                        map[string]any{"id": "s 1", "updated": true},
		"DELETE /v1/sequences/s%201":                       map[string]any{"deleted": true},
		"POST /v1/vocabulary/dictionary":                   map[string]any{"entries": []map[string]any{{"spoken": "speech kit", "canonical": "SpeechKit", "language": "en", "enabled": true}}},
		"GET /v1/transcripts?limit=3":                      map[string]any{"transcripts": []map[string]any{{"id": 9, "text": "hello"}}},
		"GET /v1/transcripts/9":                            map[string]any{"id": 9, "text": "hello"},
		"GET /v1/voiceagent/sessions/7/transcript":         map[string]any{"id": 7, "transcript": "hello"},
		"GET /v1/voiceagent/sessions/7/summary":            map[string]any{"id": 7, "summary": map[string]any{"topic": "demo"}},
		"GET /v1/tts/voices":                               map[string]any{"voices": []map[string]any{{"provider": "openai", "id": "nova", "locale": "en-US", "default": true}}},
		"POST /v1/dictation/transcribe":                    map[string]any{"text": "transcribed", "language": "en"},
		"POST /v1/custom":                                  map[string]any{"ok": true},
	}
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.RequestURI()
		got = append(got, key)
		response, ok := responses[key]
		if !ok {
			t.Fatalf("unexpected request %s", key)
		}
		if key == "POST /v1/dictation/transcribe" {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
				t.Fatalf("Content-Type = %q, want multipart/form-data", r.Header.Get("Content-Type"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read multipart body: %v", err)
			}
			for _, want := range []string{"language", "en", "model", "tiny", "prompt", "prompt-text", "audio-bytes"} {
				if !strings.Contains(string(body), want) {
					t.Fatalf("multipart body missing %q: %s", want, body)
				}
			}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	payload := map[string]any{"name": "demo"}
	audioPath := filepath.Join(t.TempDir(), "sample.wav")
	if err := os.WriteFile(audioPath, []byte("audio-bytes"), 0o600); err != nil {
		t.Fatalf("write audio sample: %v", err)
	}

	calls := []struct {
		name string
		run  func() error
	}{
		{"Config", func() error { _, err := c.Config(ctx); return err }},
		{"CatalogReadiness", func() error { _, err := c.CatalogReadiness(ctx); return err }},
		{"ProviderReadiness", func() error { _, err := c.ProviderReadiness(ctx, " voice agent "); return err }},
		{"CatalogProfiles", func() error { _, err := c.CatalogProfiles(ctx, " assist mode "); return err }},
		{"CatalogContracts", func() error { _, err := c.CatalogContracts(ctx); return err }},
		{"PersonasList", func() error { _, err := c.PersonasList(ctx); return err }},
		{"Personas", func() error { _, err := c.Personas(ctx); return err }},
		{"Persona", func() error { _, err := c.Persona(ctx, " p 1 "); return err }},
		{"CreatePersona", func() error { _, err := c.CreatePersona(ctx, payload); return err }},
		{"UpdatePersona", func() error { _, err := c.UpdatePersona(ctx, " p 1 ", payload); return err }},
		{"DeletePersona", func() error { return c.DeletePersona(ctx, " p 1 ") }},
		{"Roles", func() error { _, err := c.Roles(ctx); return err }},
		{"Role", func() error { _, err := c.Role(ctx, " r 1 "); return err }},
		{"CreateRole", func() error { _, err := c.CreateRole(ctx, payload); return err }},
		{"UpdateRole", func() error { _, err := c.UpdateRole(ctx, " r 1 ", payload); return err }},
		{"DeleteRole", func() error { return c.DeleteRole(ctx, " r 1 ") }},
		{"Sequences", func() error { _, err := c.Sequences(ctx); return err }},
		{"Sequence", func() error { _, err := c.Sequence(ctx, " s 1 "); return err }},
		{"CreateSequence", func() error { _, err := c.CreateSequence(ctx, payload); return err }},
		{"UpdateSequence", func() error { _, err := c.UpdateSequence(ctx, " s 1 ", payload); return err }},
		{"DeleteSequence", func() error { return c.DeleteSequence(ctx, " s 1 ") }},
		{"ReplaceVocabularyEntries", func() error {
			_, err := c.ReplaceVocabularyEntries(ctx, " en ", []DictionaryEntry{{Spoken: "speech kit", Canonical: "SpeechKit", Language: "en", Enabled: true}})
			return err
		}},
		{"Transcripts", func() error { _, err := c.Transcripts(ctx, 3); return err }},
		{"Transcript", func() error { _, err := c.Transcript(ctx, 9); return err }},
		{"VoiceAgentSessionTranscript", func() error { _, err := c.VoiceAgentSessionTranscript(ctx, 7); return err }},
		{"VoiceAgentSessionSummary", func() error { _, err := c.VoiceAgentSessionSummary(ctx, 7); return err }},
		{"TTSVoices", func() error { _, err := c.TTSVoices(ctx); return err }},
		{"TranscribeFile", func() error {
			_, err := c.TranscribeFile(ctx, audioPath, TranscribeOptions{Language: "en", Model: "tiny", Prompt: "prompt-text"})
			return err
		}},
		{"RawJSON", func() error { _, err := c.RawJSON(ctx, http.MethodPost, "/v1/custom", payload); return err }},
	}
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			if err := call.run(); err != nil {
				t.Fatalf("%s: %v", call.name, err)
			}
		})
	}

	if len(got) != len(calls) {
		t.Fatalf("request count = %d, want %d: %v", len(got), len(calls), got)
	}
}

func TestFromEnvUsesDocumentedVariables(t *testing.T) {
	t.Setenv("SPEECHKIT_SERVER_URL", "http://example.test")
	t.Setenv("SPEECHKIT_TOKEN", "")
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "server-token")

	c, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if c.baseURL.String() != "http://example.test" {
		t.Fatalf("baseURL = %s", c.baseURL)
	}
	if c.token != "server-token" {
		t.Fatalf("token = %q", c.token)
	}
}

func TestHTTPErrorWithoutBody(t *testing.T) {
	if got := (HTTPError{StatusCode: http.StatusUnauthorized}).Error(); got != "speechkit: HTTP 401" {
		t.Fatalf("Error() = %q", got)
	}
}
